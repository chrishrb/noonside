package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// HistoricalFrom is the first day of the historical forecast API; earlier
// days come from the ERA5 archive, which is hourly only.
var HistoricalFrom = time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)

// History returns the irradiance for the local days from..to (inclusive,
// dates in r.Loc). fine asks for 15-minute values where available (from
// HistoricalFrom), otherwise values are hourly. Missing values (not yet
// available) are left out.
func (c *Client) History(ctx context.Context, r Request, from, to time.Time, fine bool) ([]Slot, error) {
	planes := r.planes()
	var base []Slot
	var others [][]Slot
	for i, p := range planes {
		s, err := c.historyPlane(ctx, r, p, from, to, fine)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			base = s
		} else {
			others = append(others, s)
		}
	}
	return combine(base, planes, others), nil
}

func (c *Client) historyPlane(ctx context.Context, r Request, p Plane, from, to time.Time, fine bool) ([]Slot, error) {
	split := time.Date(HistoricalFrom.Year(), HistoricalFrom.Month(), HistoricalFrom.Day(), 0, 0, 0, 0, from.Location())
	var out []Slot
	if from.Before(split) {
		end := to
		if !end.Before(split) {
			end = split.AddDate(0, 0, -1)
		}
		s, err := c.history(ctx, c.cfg.ArchiveURL+"/v1/archive", r, p, from, end, false)
		if err != nil {
			return nil, err
		}
		out = append(out, s...)
		from = split
	}
	if !to.Before(from) {
		s, err := c.history(ctx, c.cfg.HistoricalURL+"/v1/forecast", r, p, from, to, fine)
		if err != nil {
			return nil, err
		}
		out = append(out, s...)
	}
	return out, nil
}

func (c *Client) history(ctx context.Context, url string, r Request, p Plane, from, to time.Time, fine bool) ([]Slot, error) {
	q := r.query(p)
	q.Set("start_date", from.Format(time.DateOnly))
	// Values are labelled with the end of their interval and a range ends at
	// 23:00 (hourly) or 23:45, so the last interval of `to` needs the next day.
	end := to.AddDate(0, 0, 1)
	q.Set("end_date", end.Format(time.DateOnly))
	res, minutes := "hourly", 60
	if fine {
		res, minutes = "minutely_15", 0
	}
	q.Set(res, "global_tilted_irradiance,temperature_2m")
	body, err := c.get(ctx, url, q)
	if err != nil {
		return nil, err
	}
	type series struct {
		Time        []int64    `json:"time"`
		GTI         []*float64 `json:"global_tilted_irradiance"`
		Temperature []*float64 `json:"temperature_2m"`
	}
	var a struct {
		Hourly     series `json:"hourly"`
		Minutely15 series `json:"minutely_15"`
	}
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("open-meteo: decode: %w", err)
	}
	d := a.Hourly
	if fine {
		d = a.Minutely15
	}
	out := make([]Slot, 0, len(d.Time))
	for i, t := range d.Time {
		if i >= len(d.GTI) || d.GTI[i] == nil {
			continue
		}
		s := Slot{End: time.Unix(t, 0).UTC(), GTI: *d.GTI[i], Minutes: minutes}
		if s.Start().Before(from) || s.End.After(end) {
			continue
		}
		if i < len(d.Temperature) && d.Temperature[i] != nil {
			s.TemperatureC = *d.Temperature[i]
		}
		out = append(out, s)
	}
	return out, nil
}

// Daily sums the expected energy per kWp and the irradiation for each local
// day that the slots cover completely (a DST day has 23 or 25 hours).
func Daily(slots []Slot, loc *time.Location) map[string]ExpectedDay {
	wh := map[string]float64{}
	irr := map[string]float64{}
	covered := map[string]time.Duration{}
	for _, s := range slots {
		d := s.Start().In(loc).Format(time.DateOnly)
		wh[d] += ExpectedPower(s.GTI, s.TemperatureC, 1) * s.Len().Hours()
		irr[d] += s.GTI * s.Len().Hours()
		covered[d] += s.Len()
	}
	out := map[string]ExpectedDay{}
	for d, v := range wh {
		start, err := time.ParseInLocation(time.DateOnly, d, loc)
		if err != nil {
			continue
		}
		if covered[d] >= start.AddDate(0, 0, 1).Sub(start) {
			out[d] = ExpectedDay{KWhPerKWp: v / 1000, IrradiationKWhM2: irr[d] / 1000}
		}
	}
	return out
}

// IrradiationKWhM2 sums the irradiation of the slots that end in (from, to]
// (partial slots count in proportion), in kWh/m².
func IrradiationKWhM2(slots []Slot, from, to time.Time) float64 {
	var wh float64
	for _, s := range slots {
		a, b := s.Start(), s.End
		if a.Before(from) {
			a = from
		}
		if b.After(to) {
			b = to
		}
		if b.After(a) {
			wh += s.GTI * b.Sub(a).Hours()
		}
	}
	return wh / 1000
}
