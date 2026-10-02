package stellar

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockBaseline struct {
	fee int64
	err error
}

func (m *mockBaseline) GetHistoricalBaselineFee(ctx context.Context) (int64, error) {
	return m.fee, m.err
}

func TestFeeGuardrail_MainnetExceeded(t *testing.T) {
	provider := &mockBaseline{fee: 100}
	guardrail := NewFeeGuardrail(true, 3.0, provider, nil)

	err := guardrail.CheckFee(context.Background(), 500)
	assert.ErrorIs(t, err, ErrFeeSpikeExceeded)
}

func TestFeeGuardrail_MainnetWithinLimit(t *testing.T) {
	provider := &mockBaseline{fee: 100}
	guardrail := NewFeeGuardrail(true, 3.0, provider, nil)

	err := guardrail.CheckFee(context.Background(), 250)
	assert.NoError(t, err)
}

func TestFeeGuardrail_TestnetIgnored(t *testing.T) {
	provider := &mockBaseline{fee: 100}
	guardrail := NewFeeGuardrail(false, 3.0, provider, nil)

	err := guardrail.CheckFee(context.Background(), 10000)
	assert.NoError(t, err)
}
