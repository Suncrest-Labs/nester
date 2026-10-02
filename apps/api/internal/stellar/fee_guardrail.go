package stellar

import (
	"context"
	"errors"
	"fmt"
)

var ErrFeeSpikeExceeded = errors.New("fee spike exceeded baseline multiplier")

type BaselineProvider interface {
	GetHistoricalBaselineFee(ctx context.Context) (int64, error)
}

type FeeGuardrail struct {
	isMainnet         bool
	maxMultiple       float64
	baselineProvider  BaselineProvider
	alertFunc         func(msg string)
}

func NewFeeGuardrail(isMainnet bool, maxMultiple float64, baselineProvider BaselineProvider, alertFunc func(msg string)) *FeeGuardrail {
	return &FeeGuardrail{
		isMainnet:        isMainnet,
		maxMultiple:      maxMultiple,
		baselineProvider: baselineProvider,
		alertFunc:        alertFunc,
	}
}

func (g *FeeGuardrail) CheckFee(ctx context.Context, estimatedFee int64) error {
	if !g.isMainnet || g.baselineProvider == nil {
		return nil
	}

	baseFee, err := g.baselineProvider.GetHistoricalBaselineFee(ctx)
	if err != nil {
		return nil
	}

	if baseFee > 0 && float64(estimatedFee) > float64(baseFee)*g.maxMultiple {
		msg := fmt.Sprintf("Fee spike detected: estimated fee %d exceeds baseline %d by factor greater than %.2f", estimatedFee, baseFee, g.maxMultiple)
		if g.alertFunc != nil {
			g.alertFunc(msg)
		}
		return fmt.Errorf("%w: %s", ErrFeeSpikeExceeded, msg)
	}

	return nil
}
