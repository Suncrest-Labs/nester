package stellar

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stellar/go/xdr"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savings"
)

// SavingsReader reads the live state of the savings core contract.
type SavingsReader interface {
	State(ctx context.Context) (savings.ChainState, error)
}

// ChainSavingsReader implements SavingsReader by simulating the core's view
// functions. It never submits a transaction.
type ChainSavingsReader struct {
	reader       *ContractReader
	contract     string
	adapterNames map[string]string
}

// NewSavingsReader builds a reader for the savings core at contract.
// adapterNames maps adapter contract addresses to display names (the contracts
// carry no names); unknown adapters get an empty name.
func NewSavingsReader(reader *ContractReader, contract string, adapterNames map[string]string) *ChainSavingsReader {
	return &ChainSavingsReader{reader: reader, contract: contract, adapterNames: adapterNames}
}

func (s *ChainSavingsReader) call(ctx context.Context, fn string, args ...xdr.ScVal) (xdr.ScVal, error) {
	addr, err := contractAddressToXDR(s.contract)
	if err != nil {
		return xdr.ScVal{}, err
	}
	v, err := s.reader.simulate(ctx, addr, fn, args)
	if err != nil {
		return xdr.ScVal{}, fmt.Errorf("savings %s: %w", fn, err)
	}
	return v, nil
}

func (s *ChainSavingsReader) i128(ctx context.Context, fn string) (decimal.Decimal, error) {
	v, err := s.call(ctx, fn)
	if err != nil {
		return decimal.Zero, err
	}
	return scValI128(v, fn)
}

// State simulates every view and assembles a ChainState. Any failing view
// fails the whole read: a partial state would misreport the vault.
func (s *ChainSavingsReader) State(ctx context.Context) (savings.ChainState, error) {
	var st savings.ChainState
	var err error

	for _, f := range []struct {
		fn  string
		dst *decimal.Decimal
	}{
		{"share_price", &st.SharePrice},
		{"total_assets", &st.TotalAssets},
		{"total_supply", &st.TotalSupply},
		{"idle", &st.Idle},
		{"reserve_shares", &st.ReserveShares},
	} {
		if *f.dst, err = s.i128(ctx, f.fn); err != nil {
			return savings.ChainState{}, err
		}
	}

	v, err := s.call(ctx, "fee_bps")
	if err != nil {
		return savings.ChainState{}, err
	}
	if st.FeeBps, err = scValU32(v, "fee_bps"); err != nil {
		return savings.ChainState{}, err
	}

	if v, err = s.call(ctx, "timelock"); err != nil {
		return savings.ChainState{}, err
	}
	secs, err := scValU64(v, "timelock")
	if err != nil {
		return savings.ChainState{}, err
	}
	st.Timelock = time.Duration(secs) * time.Second

	if v, err = s.call(ctx, "adapters"); err != nil {
		return savings.ChainState{}, err
	}
	if st.Adapters, err = s.decodeAdapters(ctx, v); err != nil {
		return savings.ChainState{}, err
	}

	if v, err = s.call(ctx, "pending_changes"); err != nil {
		return savings.ChainState{}, err
	}
	if st.Pending, err = decodePending(v); err != nil {
		return savings.ChainState{}, err
	}
	return st, nil
}

func (s *ChainSavingsReader) decodeAdapters(ctx context.Context, v xdr.ScVal) ([]savings.AdapterState, error) {
	items, err := scValVec(v, "adapters")
	if err != nil {
		return nil, err
	}
	out := make([]savings.AdapterState, 0, len(items))
	for _, item := range items {
		if item.Type != xdr.ScValTypeScvMap || item.Map == nil || *item.Map == nil {
			return nil, fmt.Errorf("adapters: expected map entry, got %s", item.Type)
		}
		var a savings.AdapterState
		var addrVal xdr.ScVal
		var haveAddr bool
		for _, e := range **item.Map {
			if e.Key.Type != xdr.ScValTypeScvSymbol {
				continue
			}
			switch string(e.Key.MustSym()) {
			case "adapter":
				addrVal, haveAddr = e.Val, true
				if e.Val.Type != xdr.ScValTypeScvAddress || e.Val.Address == nil {
					return nil, fmt.Errorf("adapters: adapter is not an address")
				}
				if a.Address, err = e.Val.Address.String(); err != nil {
					return nil, fmt.Errorf("adapters: encode address: %w", err)
				}
			case "abs_cap":
				if a.AbsCap, err = scValI128(e.Val, "abs_cap"); err != nil {
					return nil, err
				}
			case "rel_cap_bps":
				if a.RelCapBps, err = scValU32(e.Val, "rel_cap_bps"); err != nil {
					return nil, err
				}
			}
		}
		if !haveAddr {
			return nil, fmt.Errorf("adapters: entry missing adapter")
		}
		a.Name = s.adapterNames[a.Address]
		val, err := s.call(ctx, "allocation", addrVal)
		if err != nil {
			return nil, err
		}
		if a.Value, err = scValI128(val, "allocation"); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// decodePending decodes Vec<(u32, Change, u64)>: each tuple is an ScVec of
// [id, Change, eta_unix_seconds] and Change is ScVec[Symbol(variant), ...].
func decodePending(v xdr.ScVal) ([]savings.PendingChange, error) {
	items, err := scValVec(v, "pending_changes")
	if err != nil {
		return nil, err
	}
	out := make([]savings.PendingChange, 0, len(items))
	for _, item := range items {
		tuple, err := scValVec(item, "pending_changes entry")
		if err != nil {
			return nil, err
		}
		if len(tuple) != 3 {
			return nil, fmt.Errorf("pending_changes: tuple has %d elements, want 3", len(tuple))
		}
		id, err := scValU32(tuple[0], "pending id")
		if err != nil {
			return nil, err
		}
		change, err := scValVec(tuple[1], "pending change")
		if err != nil {
			return nil, err
		}
		if len(change) == 0 || change[0].Type != xdr.ScValTypeScvSymbol {
			return nil, fmt.Errorf("pending_changes: change has no variant symbol")
		}
		eta, err := scValU64(tuple[2], "pending eta")
		if err != nil {
			return nil, err
		}
		out = append(out, savings.PendingChange{
			ID:   id,
			Kind: string(change[0].MustSym()),
			ETA:  time.Unix(int64(eta), 0).UTC(), // #nosec G115 -- ledger timestamps fit int64
		})
	}
	return out, nil
}

func scValVec(v xdr.ScVal, what string) ([]xdr.ScVal, error) {
	if v.Type != xdr.ScValTypeScvVec || v.Vec == nil || *v.Vec == nil {
		return nil, fmt.Errorf("%s: expected vec, got %s", what, v.Type)
	}
	return []xdr.ScVal(**v.Vec), nil
}

func scValU32(v xdr.ScVal, what string) (uint32, error) {
	if v.Type != xdr.ScValTypeScvU32 || v.U32 == nil {
		return 0, fmt.Errorf("%s: expected u32, got %s", what, v.Type)
	}
	return uint32(*v.U32), nil
}

func scValU64(v xdr.ScVal, what string) (uint64, error) {
	if v.Type != xdr.ScValTypeScvU64 || v.U64 == nil {
		return 0, fmt.Errorf("%s: expected u64, got %s", what, v.Type)
	}
	return uint64(*v.U64), nil
}

// scValI128 converts an i128 to a decimal without the int64 bound
// simulateI128 imposes: hi*2^64 + lo, with hi signed.
func scValI128(v xdr.ScVal, what string) (decimal.Decimal, error) {
	if v.Type != xdr.ScValTypeScvI128 || v.I128 == nil {
		return decimal.Zero, fmt.Errorf("%s: expected i128, got %s", what, v.Type)
	}
	n := new(big.Int).SetInt64(int64(v.I128.Hi))
	n.Lsh(n, 64)
	n.Add(n, new(big.Int).SetUint64(uint64(v.I128.Lo)))
	return decimal.NewFromBigInt(n, 0), nil
}
