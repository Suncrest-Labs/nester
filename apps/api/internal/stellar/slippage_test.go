package stellar

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSlippageBps_RejectsZero(t *testing.T) {
	err := ValidateSlippageBps(0)
	require.ErrorIs(t, err, ErrInvalidSlippageBps)
}

func TestValidateSlippageBps_RejectsAboveMax(t *testing.T) {
	err := ValidateSlippageBps(301)
	require.ErrorIs(t, err, ErrInvalidSlippageBps)
}

func TestValidateSlippageBps_AcceptsSafeRange(t *testing.T) {
	require.NoError(t, ValidateSlippageBps(1))
	require.NoError(t, ValidateSlippageBps(300))
}

func TestResolveSlippageBps_UsesDefaultWhenUnset(t *testing.T) {
	bps, err := ResolveSlippageBps(0, 50)
	require.NoError(t, err)
	assert.Equal(t, 50, bps)
}

func TestResolveSlippageBps_FallsBackToPackageDefault(t *testing.T) {
	bps, err := ResolveSlippageBps(0, 0)
	require.NoError(t, err)
	assert.Equal(t, DefaultWithdrawalSlippageBps, bps)
}

func TestResolveSlippageBps_ValidatesCallerOverride(t *testing.T) {
	_, err := ResolveSlippageBps(400, 50)
	require.ErrorIs(t, err, ErrInvalidSlippageBps)

	bps, err := ResolveSlippageBps(50, 100)
	require.NoError(t, err)
	assert.Equal(t, 50, bps)
}

func TestComputeMinAssetsOut_FiftyBps(t *testing.T) {
	const preview = int64(10_000_000)
	minOut := ComputeMinAssetsOut(preview, 50)
	assert.Equal(t, int64(9_950_000), minOut)
}

func TestComputeMinAssetsOut_ZeroPreview(t *testing.T) {
	assert.Equal(t, int64(0), ComputeMinAssetsOut(0, 50))
}
