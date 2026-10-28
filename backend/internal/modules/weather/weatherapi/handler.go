// Package weatherapi serves the weather module's API operation: a system's
// forecast with the expected power.
package weatherapi

import (
	"context"
	"math"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/weather"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

// Systems loads systems (implemented by the systems module).
type Systems interface {
	GetSystem(ctx context.Context, id uuid.UUID) (*systems.System, error)
}

type Handler struct {
	weather *weather.Service
	systems Systems
	authz   httpapi.Authz
	now     func() time.Time
}

func New(w *weather.Service, sys Systems, roles auth.RoleLookup) *Handler {
	return &Handler{weather: w, systems: sys, authz: httpapi.Authz{Roles: roles}, now: time.Now}
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

func dateOf(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// hourlyHours is how far ahead the hourly forecast goes.
const hourlyHours = 24

func (h *Handler) GetWeather(ctx context.Context, params generated.GetWeatherParams) (*generated.Weather, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeDataRead); err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	out := &generated.Weather{Hourly: []generated.WeatherHour{}, Days: []generated.WeatherDay{}}
	f, err := h.weather.Get(ctx, sys)
	if err != nil || f == nil {
		return out, err
	}
	loc, now, kwp := sys.Loc(), h.now(), sys.PeakPowerKWp
	today := dateOf(now, loc)
	out.Available = true
	out.Oriented = len(sys.Planes) > 0
	out.Source = generated.NewOptString("Open-Meteo")

	cur := generated.WeatherCurrent{
		Time: f.Current.Time, TemperatureC: f.Current.TemperatureC, WeatherCode: f.Current.WeatherCode,
		CloudCoverPct: f.Current.CloudCoverPct, IsDay: f.Current.IsDay,
	}
	if w, ok := f.ExpectedAt(now, kwp); ok {
		cur.ExpectedW = generated.NewOptFloat64(round(w, 0))
	}
	out.Current = generated.NewOptWeatherCurrent(cur)

	days := map[string]weather.Day{}
	for _, d := range f.Days {
		days[d.Date] = d
		start, err := time.ParseInLocation(time.DateOnly, d.Date, loc)
		if err != nil || start.Before(today) {
			continue
		}
		wd := generated.WeatherDay{
			Date: start, WeatherCode: d.WeatherCode, TempMinC: d.TempMinC, TempMaxC: d.TempMaxC,
			SunshineH: round(d.SunshineH, 1), ExpectedKwh: round(f.ExpectedKWh(start, start.AddDate(0, 0, 1), kwp), 1),
		}
		if !d.Sunrise.IsZero() {
			wd.Sunrise = generated.NewOptDateTime(d.Sunrise)
			wd.Sunset = generated.NewOptDateTime(d.Sunset)
		}
		out.Days = append(out.Days, wd)
	}
	for _, hr := range f.Hours {
		if hr.Time.Add(time.Hour).Before(now) || len(out.Hourly) == hourlyHours {
			continue
		}
		mid := hr.Time.Add(30 * time.Minute)
		d := days[hr.Time.In(loc).Format(time.DateOnly)]
		out.Hourly = append(out.Hourly, generated.WeatherHour{
			Time: hr.Time, TemperatureC: hr.TemperatureC, WeatherCode: hr.WeatherCode, CloudCoverPct: hr.CloudCoverPct,
			PrecipitationProbabilityPct: hr.PrecipProbPct, IsDay: mid.After(d.Sunrise) && mid.Before(d.Sunset),
		})
	}
	return out, nil
}
