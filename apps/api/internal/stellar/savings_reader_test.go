package stellar

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stellar/go/strkey"
	"github.com/stellar/go/xdr"

	"github.com/suncrestlabs/nester/apps/api/internal/chaintest"
)

func scI128(v int64) xdr.ScVal {
	b := new(big.Int).SetInt64(v)
	parts := xdr.Int128Parts{Hi: xdr.Int64(b.Rsh(b, 64).Int64()), Lo: xdr.Uint64(uint64(v))}
	return xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &parts}
}

func scU32(v uint32) xdr.ScVal {
	x := xdr.Uint32(v)
	return xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &x}
}
func scU64(v uint64) xdr.ScVal {
	x := xdr.Uint64(v)
	return xdr.ScVal{Type: xdr.ScValTypeScvU64, U64: &x}
}
func scSym(s string) xdr.ScVal {
	x := xdr.ScSymbol(s)
	return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &x}
}
func scVec(v ...xdr.ScVal) xdr.ScVal {
	x := xdr.ScVec(v)
	p := &x
	return xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &p}
}

func scContract(t *testing.T, id byte) (xdr.ScVal, string) {
	raw := make([]byte, 32)
	raw[0] = id
	str, err := strkey.Encode(strkey.VersionByteContract, raw)
	if err != nil {
		t.Fatal(err)
	}
	var cid xdr.ContractId
	copy(cid[:], raw)
	addr := xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract, ContractId: &cid}
	return xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &addr}, str
}

func scMap(t *testing.T, kv ...xdr.ScVal) xdr.ScVal {
	m := xdr.ScMap{}
	for i := 0; i < len(kv); i += 2 {
		m = append(m, xdr.ScMapEntry{Key: kv[i], Val: kv[i+1]})
	}
	p := &m
	return xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &p}
}

func b64(t *testing.T, v xdr.ScVal) string {
	s, err := xdr.MarshalBase64(v)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSavingsReader_ParsesState(t *testing.T) {
	chain := chaintest.New()
	t.Cleanup(chain.Close)

	adapterVal, adapterStr := scContract(t, 7)
	_, coreStr := scContract(t, 1)
	eta := uint64(1_800_000_000)

	ret := map[string]xdr.ScVal{
		"share_price":     scI128(10_010_000),
		"total_assets":    scI128(5_000_000_000),
		"total_supply":    scI128(4_995_000_000),
		"idle":            scI128(1_000_000_000),
		"reserve_shares":  scI128(12_345),
		"fee_bps":         scU32(1000),
		"timelock":        scU64(259_200),
		"allocation":      scI128(4_000_000_000),
		"adapters":        scVec(scMap(t, scSym("abs_cap"), scI128(9_000_000_000), scSym("adapter"), adapterVal, scSym("rel_cap_bps"), scU32(5000))),
		"pending_changes": scVec(scVec(scU32(3), scVec(scSym("SetFee"), scU32(1500)), scU64(eta))),
	}
	for fn, v := range ret {
		chain.SetSimulateReturn(fn, b64(t, v))
	}

	reader := NewContractReader(chain.SorobanURL, "Test SDF Network ; September 2015", "")
	sr := NewSavingsReader(reader, coreStr, map[string]string{adapterStr: "Blend"})

	st, err := sr.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	eq := func(name string, got decimal.Decimal, want int64) {
		if !got.Equal(decimal.NewFromInt(want)) {
			t.Errorf("%s = %s, want %d", name, got, want)
		}
	}
	eq("SharePrice", st.SharePrice, 10_010_000)
	eq("TotalAssets", st.TotalAssets, 5_000_000_000)
	eq("TotalSupply", st.TotalSupply, 4_995_000_000)
	eq("Idle", st.Idle, 1_000_000_000)
	eq("ReserveShares", st.ReserveShares, 12_345)
	if st.FeeBps != 1000 || st.Timelock != 72*time.Hour {
		t.Errorf("fee/timelock = %d/%s", st.FeeBps, st.Timelock)
	}
	if len(st.Adapters) != 1 {
		t.Fatalf("adapters = %d", len(st.Adapters))
	}
	a := st.Adapters[0]
	if a.Address != adapterStr || a.Name != "Blend" || a.RelCapBps != 5000 {
		t.Errorf("adapter = %+v", a)
	}
	eq("Adapter.Value", a.Value, 4_000_000_000)
	eq("Adapter.AbsCap", a.AbsCap, 9_000_000_000)
	if len(st.Pending) != 1 || st.Pending[0].ID != 3 || st.Pending[0].Kind != "SetFee" || !st.Pending[0].ETA.Equal(time.Unix(int64(eta), 0)) {
		t.Errorf("pending = %+v", st.Pending)
	}
}

func TestSavingsReader_BalanceAndConvertToAssets(t *testing.T) {
	chain := chaintest.New()
	t.Cleanup(chain.Close)
	_, coreStr := scContract(t, 1)
	chain.SetSimulateReturn("balance", b64(t, scI128(25_000_000)))
	chain.SetSimulateReturn("convert_to_assets", b64(t, scI128(25_025_000)))

	sr := NewSavingsReader(NewContractReader(chain.SorobanURL, "Test SDF Network ; September 2015", ""), coreStr, nil)
	wallet := "GAAZI4TCR3TY5OJHCTJC2A4QSY6CJWJH5IAJTGKIN2ER7LBNVKOCCWN7"

	bal, err := sr.Balance(context.Background(), wallet)
	if err != nil || !bal.Equal(decimal.NewFromInt(25_000_000)) {
		t.Fatalf("Balance = %s, %v", bal, err)
	}
	assets, err := sr.ConvertToAssets(context.Background(), bal)
	if err != nil || !assets.Equal(decimal.NewFromInt(25_025_000)) {
		t.Fatalf("ConvertToAssets = %s, %v", assets, err)
	}
	if _, err := sr.Balance(context.Background(), "not-an-address"); err == nil {
		t.Fatal("want error for invalid wallet")
	}
	if _, err := sr.ConvertToAssets(context.Background(), decimal.RequireFromString("1.5")); err == nil {
		t.Fatal("want error for non-integer shares")
	}
}
