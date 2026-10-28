package prices

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/google/uuid"
)

// EnergyStore reads prices and minute energy.
type EnergyStore interface {
	NegativeMarketPrices(ctx context.Context, zone string, from, to time.Time) ([]MarketPrice, bool, error)
	EnergyDuring(ctx context.Context, systemID uuid.UUID, intervals []telemetry.Interval) (map[uuid.UUID]float64, error)
}

// NegativeEnergy is the production while day-ahead prices were negative.
type NegativeEnergy struct {
	KWh   float64
	Hours float64
	// UnpaidRevenue is the feed-in revenue not paid for it (only with
	// System.UnpaidAtNegativePrices).
	UnpaidRevenue float64
}

// NegativeDuring returns the energy the inverters in only (all if nil) produced
// in [from, to) while the price in the system's zone was negative. It is nil
// without a price zone or without any prices in the range. Without meters
// all production counts as fed in.
func NegativeDuring(ctx context.Context, st EnergyStore, sys *systems.System, from, to time.Time, only map[uuid.UUID]bool) (*NegativeEnergy, error) {
	if sys.PriceZone == nil {
		return nil, nil
	}
	ps, any, err := st.NegativeMarketPrices(ctx, *sys.PriceZone, from, to)
	if err != nil || !any {
		return nil, err
	}
	neg := Negative(Clip(ps, from, to))
	out := &NegativeEnergy{Hours: Hours(neg)}
	if len(neg) == 0 {
		return out, nil
	}
	ivs := make([]telemetry.Interval, len(neg))
	for i, p := range neg {
		ivs[i] = telemetry.Interval{Start: p.Start, End: p.End}
	}
	per, err := st.EnergyDuring(ctx, sys.ID, ivs)
	if err != nil {
		return nil, err
	}
	for id, wh := range per {
		if only == nil || only[id] {
			out.KWh += wh / 1000
		}
	}
	if sys.UnpaidAtNegativePrices {
		out.UnpaidRevenue = out.KWh * sys.TariffPerKWh
	}
	return out, nil
}
