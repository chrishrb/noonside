// Package advice combines the PV forecast and the day-ahead prices into
// "when to run large loads today": the window with the most expected PV
// power and the one with the lowest price. It is advisory; nothing here
// switches devices. Without meters the surplus is the expected production.
package advice

import (
	"context"
	"math"
	"time"

	"github.com/chrishrb/noonside/internal/modules/prices"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/weather"
)

// DefaultHours is the window length unless the caller asks for another.
const DefaultHours = 3

// step is the grid the windows are searched on (the forecast's slots and
// the prices' shortest interval).
const step = 15 * time.Minute

// Window is a time range of the advice.
type Window struct {
	Start, End  time.Time
	ExpectedKWh float64
	MeanW       float64
	// MeanEURMWh is the mean price, nil without prices.
	MeanEURMWh *float64
}

// Slot is one hour of the rest of the day.
type Slot struct {
	Start, End time.Time
	ExpectedW  *float64
	EURMWh     *float64
}

// Advice is the rest of a local day.
type Advice struct {
	// Available is false without a forecast and without prices.
	Available bool
	Date      time.Time
	Hours     int
	// ExpectedKWh (whole day) and RemainingKWh (from now) are nil without
	// a forecast.
	ExpectedKWh  *float64
	RemainingKWh *float64
	// Solar is the window with the most expected PV energy; Cheap the one
	// with the lowest mean price. Either is nil when there is none.
	Solar, Cheap *Window
	// Negative are the remaining intervals with a negative price.
	Negative []prices.MarketPrice
	Slots    []Slot
}

// Compute returns the advice for the rest of the local day at now: slots is
// the irradiance forecast, kwp the peak power, ps the day-ahead prices.
func Compute(now time.Time, loc *time.Location, slots []weather.Slot, kwp float64, ps []prices.MarketPrice, hours int) *Advice {
	if hours <= 0 {
		hours = DefaultHours
	}
	l := now.In(loc)
	day := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
	end := day.AddDate(0, 0, 1)
	from := now.Truncate(step)
	a := &Advice{Date: day, Hours: hours}

	// Expected power and price on the 15-minute grid from now on.
	var grid []time.Time
	for t := from; t.Before(end); t = t.Add(step) {
		grid = append(grid, t)
	}
	haveForecast := kwp > 0 && weather.ExpectedKWh(slots, day, end, kwp) > 0
	power := make([]float64, len(grid))
	if haveForecast {
		total := weather.ExpectedKWh(slots, day, end, kwp)
		remaining := energyFrom(slots, from, end, kwp)
		a.ExpectedKWh, a.RemainingKWh = ptr(round(total, 1)), ptr(round(remaining, 1))
		for i, t := range grid {
			power[i] = meanPower(slots, t, t.Add(step), kwp)
		}
	}
	price := make([]*float64, len(grid))
	ps = prices.Clip(ps, from, end)
	for i, t := range grid {
		price[i] = meanPrice(ps, t, t.Add(step))
	}
	havePrices := len(ps) > 0
	a.Available = haveForecast || havePrices
	if !a.Available {
		return a
	}
	a.Negative = prices.Negative(ps)

	n := min(hours*int(time.Hour/step), len(grid))
	if haveForecast && n > 0 {
		best, bestWh := -1, 0.0
		for i := 0; i+n <= len(grid); i++ {
			wh := 0.0
			for _, p := range power[i : i+n] {
				wh += p * step.Hours()
			}
			// Ties go to the earlier window.
			if wh > bestWh+1e-9 {
				best, bestWh = i, wh
			}
		}
		// A window must expect something worth shifting a load to.
		if best >= 0 && bestWh >= 10 {
			a.Solar = window(grid, power, price, best, n, bestWh)
		}
	}
	if havePrices && n > 0 {
		best, bestMean := -1, math.Inf(1)
		for i := 0; i+n <= len(grid); i++ {
			m, ok := mean(price[i : i+n])
			if ok && m < bestMean-1e-9 {
				best, bestMean = i, m
			}
		}
		if best >= 0 {
			wh := 0.0
			for _, p := range power[best : best+n] {
				wh += p * step.Hours()
			}
			a.Cheap = window(grid, power, price, best, n, wh)
		}
	}

	// Hourly slots from the current hour.
	for t := now.In(loc).Truncate(time.Hour); t.Before(end); t = t.Add(time.Hour) {
		s := Slot{Start: t, End: t.Add(time.Hour)}
		if haveForecast {
			s.ExpectedW = ptr(round(meanPower(slots, t, t.Add(time.Hour), kwp), 0))
		}
		if p := meanPrice(ps, t, t.Add(time.Hour)); p != nil {
			s.EURMWh = ptr(round(*p, 2))
		}
		a.Slots = append(a.Slots, s)
	}
	return a
}

func window(grid []time.Time, power []float64, price []*float64, i, n int, wh float64) *Window {
	w := &Window{Start: grid[i], End: grid[i+n-1].Add(step), ExpectedKWh: round(wh/1000, 1)}
	w.MeanW = round(wh/w.End.Sub(w.Start).Hours(), 0)
	if m, ok := mean(price[i : i+n]); ok {
		w.MeanEURMWh = ptr(round(m, 2))
	}
	return w
}

// meanPower is the mean expected power in W over [from, to), weighting
// each slot by its overlap.
func meanPower(slots []weather.Slot, from, to time.Time, kwp float64) float64 {
	var wh float64
	for _, s := range slots {
		if o := overlap(s.Start(), s.End, from, to); o > 0 {
			wh += s.Power(kwp) * o.Hours()
		}
	}
	return wh / to.Sub(from).Hours()
}

func energyFrom(slots []weather.Slot, from, to time.Time, kwp float64) float64 {
	return meanPower(slots, from, to, kwp) * to.Sub(from).Hours() / 1000
}

// meanPrice is the time-weighted mean price over [from, to), nil without
// prices for all of it.
func meanPrice(ps []prices.MarketPrice, from, to time.Time) *float64 {
	var sum float64
	var covered time.Duration
	for _, p := range ps {
		if o := overlap(p.Start, p.End, from, to); o > 0 {
			sum += p.EURMWh * o.Hours()
			covered += o
		}
	}
	if covered < to.Sub(from) {
		return nil
	}
	v := sum / covered.Hours()
	return &v
}

func mean(vs []*float64) (float64, bool) {
	var sum float64
	for _, v := range vs {
		if v == nil {
			return 0, false
		}
		sum += *v
	}
	return sum / float64(len(vs)), len(vs) > 0
}

func overlap(a0, a1, b0, b1 time.Time) time.Duration {
	s, e := a0, a1
	if b0.After(s) {
		s = b0
	}
	if b1.Before(e) {
		e = b1
	}
	return max(e.Sub(s), 0)
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

func ptr[T any](v T) *T { return &v }

// Prices reads day-ahead prices (implemented by the prices module).
type Prices interface {
	MarketPrices(ctx context.Context, zone string, from, to time.Time) ([]prices.MarketPrice, error)
}

// Forecaster returns a system's cached forecast (implemented by
// weather.Service).
type Forecaster interface {
	Get(ctx context.Context, sys *systems.System) (*weather.Forecast, error)
}

// Service computes the advice from the stored prices and the forecast.
type Service struct {
	weather Forecaster
	prices  Prices
	now     func() time.Time
}

// NewService returns the service; wx may be nil (prices only).
func NewService(wx Forecaster, ps Prices) *Service {
	return &Service{weather: wx, prices: ps, now: time.Now}
}

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Today returns the advice for the rest of the system's local day.
func (s *Service) Today(ctx context.Context, sys *systems.System, hours int) (*Advice, error) {
	now := s.now()
	var slots []weather.Slot
	if s.weather != nil {
		f, err := s.weather.Get(ctx, sys)
		if err != nil {
			return nil, err
		}
		if f != nil {
			slots = f.Slots
		}
	}
	var ps []prices.MarketPrice
	if sys.PriceZone != nil {
		l := now.In(sys.Loc())
		day := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, sys.Loc())
		var err error
		if ps, err = s.prices.MarketPrices(ctx, *sys.PriceZone, day, day.AddDate(0, 0, 1)); err != nil {
			return nil, err
		}
	}
	return Compute(now, sys.Loc(), slots, sys.PeakPowerKWp, ps, hours), nil
}
