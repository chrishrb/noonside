package systemsapi

import (
	"context"
	"strings"

	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
)

// geocodeLimit is how many suggestions the address search returns.
const geocodeLimit = 5

func (h *Handler) Geocode(ctx context.Context, params generated.GeocodeParams) (*generated.GeocodeResult, error) {
	if _, err := httpapi.CurrentUser(ctx); err != nil {
		return nil, err
	}
	out := &generated.GeocodeResult{Places: []generated.Place{}}
	q := strings.TrimSpace(params.Q)
	if h.cfg.Geocoder == nil || len(q) < 3 {
		return out, nil
	}
	places, err := h.cfg.Geocoder.Search(ctx, q, params.Lang.Or(""), geocodeLimit)
	if err != nil {
		h.logger.WarnContext(ctx, "geocode", "error", err)
		return out, nil
	}
	out.Available = true
	for _, p := range places {
		out.Places = append(out.Places, generated.Place{
			Label: p.Label, Latitude: p.Lat, Longitude: p.Lon, Kind: generated.PlaceKind(p.Kind),
			CountryCode: generated.NewOptString(p.CountryCode),
		})
	}
	return out, nil
}

// ReverseGeocode returns the address nearest to a position (a pin moved on
// the map). Like Geocode, failures return an empty, unavailable result.
func (h *Handler) ReverseGeocode(ctx context.Context, params generated.ReverseGeocodeParams) (*generated.GeocodeResult, error) {
	if _, err := httpapi.CurrentUser(ctx); err != nil {
		return nil, err
	}
	out := &generated.GeocodeResult{Places: []generated.Place{}}
	if h.cfg.Geocoder == nil {
		return out, nil
	}
	p, err := h.cfg.Geocoder.Reverse(ctx, params.Lat, params.Lon, params.Lang.Or(""))
	if err != nil {
		h.logger.WarnContext(ctx, "reverse geocode", "error", err)
		return out, nil
	}
	out.Available = true
	if p != nil {
		out.Places = append(out.Places, generated.Place{
			Label: p.Label, Latitude: p.Lat, Longitude: p.Lon, Kind: generated.PlaceKind(p.Kind),
			CountryCode: generated.NewOptString(p.CountryCode),
		})
	}
	return out, nil
}
