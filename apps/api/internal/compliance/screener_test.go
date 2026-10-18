package compliance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sanctionedAddr1 = "GAKX7VYAJTQOZ4ZKQ7GCTAWOAKCXY6BFCHOEQ4RRWJBTAY3EKQXHTKZL"
	sanctionedAddr2 = "GBRPYHIL2CI3FNQ4BXLFMNDLFJUNPU2HY3ZMFSHONUCEOASW7QC7OX2H"
	cleanAddr       = "GCZJM35NKGVK5EQP2NL2QMPMHFUWFYSNMWNQPOVEDCF5S5CYPQFVQZQK"
)

type stubProvider struct {
	blocked map[string]string
}

func (p *stubProvider) CheckAddress(_ context.Context, walletAddress string) (bool, string, error) {
	if reason, ok := p.blocked[strings.ToUpper(strings.TrimSpace(walletAddress))]; ok {
		return true, reason, nil
	}
	return false, "", nil
}

func TestStandardScreener_StaticHitAndMiss(t *testing.T) {
	var auditWallet, auditSource string
	screener := NewStandardScreener(Config{
		Enabled:         true,
		StaticAddresses: []string{sanctionedAddr1},
		OnAuditHit: func(_ context.Context, walletAddress, _, source string) {
			auditWallet = walletAddress
			auditSource = source
		},
	})

	// Miss
	missDecision, err := screener.ScreenAddress(context.Background(), cleanAddr)
	require.NoError(t, err)
	assert.True(t, missDecision.Allowed)
	assert.False(t, missDecision.Blocked)
	assert.Equal(t, uint64(0), screener.HitsCount())

	// Hit
	hitDecision, err := screener.ScreenAddress(context.Background(), strings.ToLower(sanctionedAddr1))
	require.ErrorIs(t, err, ErrAddressRestricted)
	assert.False(t, hitDecision.Allowed)
	assert.True(t, hitDecision.Blocked)
	assert.Equal(t, GenericBlockedMessage, hitDecision.Reason)
	assert.Equal(t, sanctionedAddr1, auditWallet)
	assert.Equal(t, "static_sanctions_list", auditSource)
	assert.Equal(t, uint64(1), screener.HitsCount())
}

func TestStandardScreener_OFACRefreshHitAndMiss(t *testing.T) {
	sampleOFACXML := `<?xml version="1.0" standalone="yes"?>
<sdnList>
  <sdnEntry>
    <uid>40001</uid>
    <lastName>SANCTIONED ENTITY</lastName>
    <idList>
      <id>
        <uid>9001</uid>
        <idType>Digital Currency Address - XLM</idType>
        <idNumber>` + sanctionedAddr2 + `</idNumber>
      </id>
    </idList>
  </sdnEntry>
</sdnList>`

	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	screener := NewStandardScreener(Config{
		Enabled:      true,
		MaxStaleness: 36 * time.Hour,
		Now:          func() time.Time { return now },
	})

	count, err := screener.RefreshFromOFACReader(strings.NewReader(sampleOFACXML))
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Hit on OFAC-refreshed address
	err = screener.ScreenWalletAddress(context.Background(), sanctionedAddr2)
	require.ErrorIs(t, err, ErrAddressRestricted)

	// Miss on clean address
	err = screener.ScreenWalletAddress(context.Background(), cleanAddr)
	require.NoError(t, err)
}

func TestStandardScreener_ExternalProviderHitAndMiss(t *testing.T) {
	screener := NewStandardScreener(Config{
		Enabled: true,
		Provider: &stubProvider{
			blocked: map[string]string{
				sanctionedAddr1: "risk-score-high",
			},
		},
	})

	require.ErrorIs(t, screener.ScreenWalletAddress(context.Background(), sanctionedAddr1), ErrAddressRestricted)
	require.NoError(t, screener.ScreenWalletAddress(context.Background(), cleanAddr))
}

func TestStandardScreener_StalenessAlerting(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	screener := NewStandardScreener(Config{
		Enabled:      true,
		MaxStaleness: 24 * time.Hour,
		Now:          func() time.Time { return now },
	})

	// Before first refresh -> stale
	assert.True(t, screener.CheckStaleness())
	assert.Equal(t, uint64(1), screener.StaleAlertsCount())

	// Refresh now -> fresh
	_, err := screener.RefreshFromOFACReader(strings.NewReader(`<sdnList></sdnList>`))
	require.NoError(t, err)
	assert.False(t, screener.CheckStaleness())
	assert.Equal(t, uint64(1), screener.StaleAlertsCount())

	// Advance clock past MaxStaleness -> stale again
	now = now.Add(25 * time.Hour)
	assert.True(t, screener.CheckStaleness())
	assert.Equal(t, uint64(2), screener.StaleAlertsCount())
}

func TestStandardScreener_DisabledAllowsAll(t *testing.T) {
	screener := NewStandardScreener(Config{
		Enabled:         false,
		StaticAddresses: []string{sanctionedAddr1},
	})

	require.NoError(t, screener.ScreenWalletAddress(context.Background(), sanctionedAddr1))
}
