// Package adviceapi serves the advice module's API operation: the best window for large loads today.
package adviceapi

import (
	"context"
	"math"
	"time"

	"github.com/chrishrb/noonside/internal/modules/advice"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

// Systems loads systems (implemented by the systems module).
type Systems interface {
	GetSystem(ctx context.Context, id uuid.UUID) (*systems.System, error)
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

func dateOf(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

type Handler struct {
	advice  *advice.Service
	systems Systems
	authz   httpapi.Authz
	now     func() time.Time
}

// New returns the handler; a nil service answers without advice.
func New(a *advice.Service, sys Systems, roles auth.RoleLookup) *Handler {
	return &Handler{advice: a, systems: sys, authz: httpapi.Authz{Roles: roles}, now: time.Now}
}

func (h *Handler) GetAdvice(ctx context.Context, params generated.GetAdviceParams) (*generated.Advice, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataRead); err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	hours := params.Hours.Or(advice.DefaultHours)
	if h.advice == nil {
		return &generated.Advice{Date: dateOf(h.now(), sys.Loc()), Hours: hours, NegativePrices: []generated.PricePoint{}, Slots: []generated.AdviceSlot{}}, nil
	}
	a, err := h.advice.Today(ctx, sys, hours)
	if err != nil {
		return nil, err
	}
	return toAdvice(a), nil
}

func toAdvice(a *advice.Advice) *generated.Advice {
	win := func(w *advice.Window) generated.OptAdviceWindow {
		if w == nil {
			return generated.OptAdviceWindow{}
		}
		return generated.NewOptAdviceWindow(generated.AdviceWindow{
			Start: w.Start, End: w.End, ExpectedKwh: generated.NewOptFloat64(w.ExpectedKWh),
			MeanW: generated.NewOptFloat64(w.MeanW), MeanEurMwh: httpapi.OptFloat(w.MeanEURMWh),
		})
	}
	out := &generated.Advice{
		Available: a.Available, Date: a.Date, Hours: a.Hours,
		ExpectedKwh: httpapi.OptFloat(a.ExpectedKWh), RemainingKwh: httpapi.OptFloat(a.RemainingKWh),
		SolarWindow: win(a.Solar), CheapWindow: win(a.Cheap),
		NegativePrices: []generated.PricePoint{}, Slots: []generated.AdviceSlot{},
	}
	for _, p := range a.Negative {
		out.NegativePrices = append(out.NegativePrices, generated.PricePoint{Start: p.Start, End: p.End, EurMwh: round(p.EURMWh, 2)})
	}
	for _, s := range a.Slots {
		out.Slots = append(out.Slots, generated.AdviceSlot{Start: s.Start, End: s.End, ExpectedW: httpapi.OptFloat(s.ExpectedW), EurMwh: httpapi.OptFloat(s.EURMWh)})
	}
	return out
}
