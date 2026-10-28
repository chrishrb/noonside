package analyticsapi

import (
	"context"
	"errors"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
)

// sharedSystem resolves an active share link to its system. Unknown,
// revoked and expired links are all 404.
func (h *Handler) sharedSystem(ctx context.Context, token string) (*systems.ShareLink, *systems.System, error) {
	l, sys, err := h.systems.ResolveShareLink(ctx, token, h.now())
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil, httpapi.NotFound("link not found")
	}
	return l, sys, err
}

func (h *Handler) GetPublicSystem(ctx context.Context, params generated.GetPublicSystemParams) (*generated.PublicSystem, error) {
	l, sys, err := h.sharedSystem(ctx, params.Token)
	if err != nil {
		return nil, err
	}
	invs, err := h.systems.ListInverters(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	out := &generated.PublicSystem{
		Name: sys.Name, Label: l.Label, Timezone: sys.Timezone, PeakPowerKwp: sys.PeakPowerKWp,
		ImageURL:       httpapi.OptMediaURL(sys.ImageKey),
		CommissionedAt: httpapi.OptDate(sys.CommissionedAt), Currency: sys.Currency,
		Co2KgPerKwh: generated.NewOptFloat64(sys.CO2KgPerKWh), ShowFinancials: l.ShowFinancials,
		Inverters: make([]generated.PublicInverter, len(invs)),
	}
	if l.ShowFinancials {
		out.TariffPerKwh = generated.NewOptFloat64(sys.TariffPerKWh)
	}
	for i, inv := range invs {
		out.Inverters[i] = generated.PublicInverter{
			ID: inv.ID, Name: inv.Name, Color: inv.Color, PeakPowerKwp: inv.PeakPowerKWp, SortOrder: inv.SortOrder,
		}
	}
	return out, nil
}

func (h *Handler) GetPublicLive(ctx context.Context, params generated.GetPublicLiveParams) (*generated.Live, error) {
	_, sys, err := h.sharedSystem(ctx, params.Token)
	if err != nil {
		return nil, err
	}
	out, err := h.live(ctx, sys)
	if err != nil {
		return nil, err
	}
	for i := range out.Inverters {
		li := &out.Inverters[i]
		li.StatusCode, li.StatusText, li.ErrorCode, li.ErrorText = generated.OptInt{}, "", generated.OptInt{}, ""
	}
	return out, nil
}

func (h *Handler) GetPublicOverview(ctx context.Context, params generated.GetPublicOverviewParams) (*generated.Overview, error) {
	l, sys, err := h.sharedSystem(ctx, params.Token)
	if err != nil {
		return nil, err
	}
	out, err := h.overview(ctx, sys, overviewQuery{period: params.Period, date: params.Date, compare: params.Compare})
	if err != nil {
		return nil, err
	}
	if !l.ShowFinancials {
		out.Summary.Revenue = 0
		out.Summary.UnpaidRevenue.Reset()
	}
	for i := range out.Inverters {
		out.Inverters[i].StatusText = ""
		out.Inverters[i].ErrorText = generated.OptString{}
	}
	return out, nil
}
