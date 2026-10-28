// Package weather fetches forecasts from Open-Meteo, caches them per system
// and estimates the expected PV power from the irradiance on the panel plane.
//
// Open-Meteo's free API is for non-commercial use only; for commercial use
// set an API key and the customer base URL (or a self-hosted instance).
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"
)

// Client calls the Open-Meteo forecast, historical forecast and archive APIs.
type Client struct {
	cfg  Config
	http *http.Client
}

// Config holds the API base URLs; APIKey is optional (commercial use).
type Config struct {
	// ForecastURL, e.g. https://api.open-meteo.com
	ForecastURL string
	// HistoricalURL (archived forecasts, 15 minutes, from 2022), e.g.
	// https://historical-forecast-api.open-meteo.com
	HistoricalURL string
	// ArchiveURL (ERA5 reanalysis, hourly, before 2022), e.g.
	// https://archive-api.open-meteo.com
	ArchiveURL string
	APIKey     string
}

func NewClient(cfg Config) *Client {
	trim := func(s string) string { return strings.TrimRight(s, "/") }
	cfg.ForecastURL, cfg.HistoricalURL, cfg.ArchiveURL = trim(cfg.ForecastURL), trim(cfg.HistoricalURL), trim(cfg.ArchiveURL)
	return &Client{cfg: cfg, http: errtrack.HTTPClient(10 * time.Second)}
}

// Request is what a forecast is fetched for.
type Request struct {
	Lat, Lon float64
	// Planes are the module fields, weighted by their share of the power;
	// one horizontal plane if empty.
	Planes []Plane
	Loc    *time.Location
}

// Plane is one orientation: Tilt 0 is horizontal; Azimuth 0 = south,
// -90 = east, 90 = west. Weight is its share of the peak power.
type Plane struct {
	Tilt, Azimuth, Weight float64
}

// planes returns the request's planes, one horizontal plane if none.
func (r Request) planes() []Plane {
	if len(r.Planes) == 0 {
		return []Plane{{Weight: 1}}
	}
	return r.Planes
}

// query holds the location and the parameters of one plane.
func (r Request) query(p Plane) url.Values {
	q := url.Values{}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	q.Set("latitude", f(r.Lat))
	q.Set("longitude", f(r.Lon))
	q.Set("tilt", f(p.Tilt))
	q.Set("azimuth", f(p.Azimuth))
	q.Set("timezone", r.Loc.String())
	return q
}

// Key identifies the request for caching (and the model version, so stored
// values are recomputed when it changes).
func (r Request) Key() string {
	var b strings.Builder
	fmt.Fprintf(&b, "v2|%.3f,%.3f|%s", r.Lat, r.Lon, r.Loc)
	for _, p := range r.planes() {
		fmt.Fprintf(&b, "|%g/%g/%.3f", p.Tilt, p.Azimuth, p.Weight)
	}
	return b.String()
}

// combine replaces the irradiance of base (plane 0) by the weighted sum over
// all planes; slots missing in another plane are dropped.
func combine(base []Slot, planes []Plane, others [][]Slot) []Slot {
	if len(others) == 0 {
		if w := planes[0].Weight; w != 1 {
			for i := range base {
				base[i].GTI *= w
			}
		}
		return base
	}
	byEnd := make([]map[int64]float64, len(others))
	for k, o := range others {
		byEnd[k] = make(map[int64]float64, len(o))
		for _, s := range o {
			byEnd[k][s.End.Unix()] = s.GTI
		}
	}
	out := base[:0]
	for _, s := range base {
		gti, ok := s.GTI*planes[0].Weight, true
		for k := range others {
			v, found := byEnd[k][s.End.Unix()]
			if !found {
				ok = false
				break
			}
			gti += v * planes[k+1].Weight
		}
		if ok {
			s.GTI = gti
			out = append(out, s)
		}
	}
	return out
}

// Days is how many local days (from today) a forecast covers.
const Days = 3

// Fetch returns the forecast for today and the next days. The first plane
// brings the weather; further planes only their irradiance.
func (c *Client) Fetch(ctx context.Context, r Request) (*Forecast, error) {
	planes := r.planes()
	q := r.query(planes[0])
	q.Set("current", "temperature_2m,weather_code,cloud_cover,is_day")
	q.Set("minutely_15", "global_tilted_irradiance,temperature_2m")
	q.Set("hourly", "weather_code,temperature_2m,cloud_cover,precipitation_probability")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,sunrise,sunset,sunshine_duration")
	q.Set("forecast_days", strconv.Itoa(Days))
	body, err := c.get(ctx, c.cfg.ForecastURL+"/v1/forecast", q)
	if err != nil {
		return nil, err
	}
	f, err := parse(body, r.Loc)
	if err != nil {
		return nil, err
	}
	var others [][]Slot
	for _, p := range planes[1:] {
		q := r.query(p)
		q.Set("minutely_15", "global_tilted_irradiance")
		q.Set("forecast_days", strconv.Itoa(Days))
		body, err := c.get(ctx, c.cfg.ForecastURL+"/v1/forecast", q)
		if err != nil {
			return nil, err
		}
		o, err := parse15(body)
		if err != nil {
			return nil, err
		}
		others = append(others, o)
	}
	f.Slots = combine(f.Slots, planes, others)
	return f, nil
}

// parse15 returns the 15-minute irradiance of a forecast response.
func parse15(body []byte) ([]Slot, error) {
	var a struct {
		Minutely15 struct {
			Time []int64    `json:"time"`
			GTI  []*float64 `json:"global_tilted_irradiance"`
		} `json:"minutely_15"`
	}
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("open-meteo: decode: %w", err)
	}
	out := make([]Slot, 0, len(a.Minutely15.Time))
	for i, t := range a.Minutely15.Time {
		var gti float64
		if i < len(a.Minutely15.GTI) && a.Minutely15.GTI[i] != nil {
			gti = *a.Minutely15.GTI[i]
		}
		out = append(out, Slot{End: time.Unix(t, 0).UTC(), GTI: gti})
	}
	return out, nil
}

// get calls url with q (plus timeformat and the API key) and returns the body.
func (c *Client) get(ctx context.Context, url string, q url.Values) ([]byte, error) {
	q.Set("timeformat", "unixtime")
	if c.cfg.APIKey != "" {
		q.Set("apikey", c.cfg.APIKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open-meteo: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("open-meteo: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(body, &e)
		return nil, fmt.Errorf("open-meteo: status %d: %s", resp.StatusCode, e.Reason)
	}
	return body, nil
}

type apiResponse struct {
	Current struct {
		Time        int64   `json:"time"`
		Temperature float64 `json:"temperature_2m"`
		WeatherCode int     `json:"weather_code"`
		CloudCover  float64 `json:"cloud_cover"`
		IsDay       int     `json:"is_day"`
	} `json:"current"`
	Minutely15 struct {
		Time        []int64    `json:"time"`
		GTI         []*float64 `json:"global_tilted_irradiance"`
		Temperature []*float64 `json:"temperature_2m"`
	} `json:"minutely_15"`
	Hourly struct {
		Time        []int64    `json:"time"`
		WeatherCode []*int     `json:"weather_code"`
		Temperature []*float64 `json:"temperature_2m"`
		CloudCover  []*float64 `json:"cloud_cover"`
		PrecipProb  []*float64 `json:"precipitation_probability"`
	} `json:"hourly"`
	Daily struct {
		Time        []int64    `json:"time"`
		WeatherCode []*int     `json:"weather_code"`
		TempMax     []*float64 `json:"temperature_2m_max"`
		TempMin     []*float64 `json:"temperature_2m_min"`
		Sunrise     []int64    `json:"sunrise"`
		Sunset      []int64    `json:"sunset"`
		Sunshine    []*float64 `json:"sunshine_duration"`
	} `json:"daily"`
}

func parse(body []byte, loc *time.Location) (*Forecast, error) {
	var a apiResponse
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("open-meteo: decode: %w", err)
	}
	unix := func(s int64) time.Time { return time.Unix(s, 0).UTC() }
	at := func(v []*float64, i int) float64 {
		if i < len(v) && v[i] != nil {
			return *v[i]
		}
		return 0
	}
	atInt := func(v []*int, i int) int {
		if i < len(v) && v[i] != nil {
			return *v[i]
		}
		return 0
	}
	f := &Forecast{Current: Current{
		Time: unix(a.Current.Time), TemperatureC: a.Current.Temperature, WeatherCode: a.Current.WeatherCode,
		CloudCoverPct: a.Current.CloudCover, IsDay: a.Current.IsDay == 1,
	}}
	m := a.Minutely15
	for i, t := range m.Time {
		f.Slots = append(f.Slots, Slot{End: unix(t), GTI: at(m.GTI, i), TemperatureC: at(m.Temperature, i)})
	}
	h := a.Hourly
	for i, t := range h.Time {
		f.Hours = append(f.Hours, Hour{
			Time: unix(t), WeatherCode: atInt(h.WeatherCode, i), TemperatureC: at(h.Temperature, i),
			CloudCoverPct: at(h.CloudCover, i), PrecipProbPct: at(h.PrecipProb, i),
		})
	}
	d := a.Daily
	for i, t := range d.Time {
		day := Day{
			Date: unix(t).In(loc).Format(time.DateOnly), WeatherCode: atInt(d.WeatherCode, i),
			TempMinC: at(d.TempMin, i), TempMaxC: at(d.TempMax, i), SunshineH: at(d.Sunshine, i) / 3600,
		}
		if i < len(d.Sunrise) {
			day.Sunrise = unix(d.Sunrise[i])
		}
		if i < len(d.Sunset) {
			day.Sunset = unix(d.Sunset[i])
		}
		f.Days = append(f.Days, day)
	}
	if len(f.Slots) == 0 || len(f.Days) == 0 {
		return nil, fmt.Errorf("open-meteo: empty forecast")
	}
	return f, nil
}
