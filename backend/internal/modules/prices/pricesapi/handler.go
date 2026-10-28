// Package pricesapi serves the prices module's API operation: a system's day-ahead prices.
package pricesapi

import (
	"context"
	"math"
	"time"

	"github.com/chrishrb/noonside/internal/modules/prices"
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
	prices  *prices.Module
	systems Systems
	authz   httpapi.Authz
	now     func() time.Time
}

func New(p *prices.Module, sys Systems, roles auth.RoleLookup) *Handler {
	return &Handler{prices: p, systems: sys, authz: httpapi.Authz{Roles: roles}, now: time.Now}
}

// maxPriceDays bounds a prices request.
const maxPriceDays = 31

// --- day-ahead prices --------------------------------------------------------

func (h *Handler) GetPrices(ctx context.Context, params generated.GetPricesParams) (*generated.MarketPrices, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataRead); err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	out := &generated.MarketPrices{Prices: []generated.PricePoint{}}
	if sys.PriceZone == nil {
		return out, nil
	}
	loc := sys.Loc()
	from := dateOf(h.now(), loc)
	if d, ok := params.From.Get(); ok {
		from = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	}
	to := from.AddDate(0, 0, 2)
	if d, ok := params.To.Get(); ok {
		to = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	}
	if !to.After(from) {
		return nil, httpapi.BadRequest("to must be after from")
	}
	if to.After(from.AddDate(0, 0, maxPriceDays)) {
		return nil, httpapi.BadRequest("at most 31 days of prices per request")
	}
	ps, err := h.prices.MarketPrices(ctx, *sys.PriceZone, from, to)
	if err != nil {
		return nil, err
	}
	out.Zone = generated.NewOptString(*sys.PriceZone)
	if h.prices.SourceName() != "" {
		out.Source = generated.NewOptString(h.prices.SourceName())
	}
	for _, p := range prices.Clip(ps, from, to) {
		out.Prices = append(out.Prices, generated.PricePoint{Start: p.Start, End: p.End, EurMwh: round(p.EURMWh, 2)})
	}
	return out, nil
}
