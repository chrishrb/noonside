package weather

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
	"github.com/google/uuid"
)

// TTL is how long a cached forecast is used before it is fetched again.
const TTL = 15 * time.Minute

// Service returns cached forecasts and refreshes stale ones on request, so
// nothing runs in the background and systems nobody opens cost no calls.
type Service struct {
	client *Client
	store  Store
	sys    Systems
	tel    Telemetry
	jobs   db.JobLocker
	now    func() time.Time
}

// NewService returns the weather module; a nil client disables weather.
func NewService(client *Client, d *db.DB, sys Systems, tel Telemetry) *Service {
	return &Service{client: client, store: &pgStore{db: d}, sys: sys, tel: tel, jobs: d, now: time.Now}
}

// newService returns a service on st (tests).
func newService(client *Client, st Store) *Service {
	return &Service{client: client, store: st, now: time.Now}
}

func (s *Service) Enabled() bool { return s != nil && s.client != nil }

// RequestFor returns the request for sys, or false if it has no coordinates.
// Planes are weighted by their peak power (equally if none is given).
func RequestFor(sys *systems.System) (Request, bool) {
	if sys.Latitude == nil || sys.Longitude == nil {
		return Request{}, false
	}
	r := Request{Lat: *sys.Latitude, Lon: *sys.Longitude, Loc: sys.Loc()}
	var total float64
	for _, p := range sys.Planes {
		total += p.KWp
	}
	for _, p := range sys.Planes {
		w := 1 / float64(len(sys.Planes))
		if total > 0 {
			w = p.KWp / total
		}
		r.Planes = append(r.Planes, Plane{Tilt: p.TiltDeg, Azimuth: p.AzimuthDeg, Weight: w})
	}
	return r, true
}

// Get returns the forecast for sys, or nil if weather is disabled, the
// system has no coordinates or nothing could be fetched. A failed refresh
// falls back to the stale forecast.
func (s *Service) Get(ctx context.Context, sys *systems.System) (*Forecast, error) {
	if !s.Enabled() {
		return nil, nil
	}
	req, ok := RequestFor(sys)
	if !ok {
		return nil, nil
	}
	key := req.Key()
	cached, err := s.store.GetWeather(ctx, sys.ID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	var stale *Forecast
	if cached != nil && cached.Key == key {
		var f Forecast
		if err := json.Unmarshal(cached.Data, &f); err == nil {
			if s.now().Sub(cached.FetchedAt) < TTL {
				return &f, nil
			}
			stale = &f
		}
	}
	f, err := s.client.Fetch(ctx, req)
	if err != nil {
		slog.WarnContext(ctx, "weather fetch failed", "system", sys.ID, "error", err)
		return stale, nil
	}
	data, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	if err := s.store.PutWeather(ctx, &WeatherCache{SystemID: sys.ID, Key: key, FetchedAt: s.now(), Data: data}); err != nil {
		slog.WarnContext(ctx, "weather cache write failed", "system", sys.ID, "error", err)
	}
	return f, nil
}

// Curve returns the irradiance slots of the local day starting at day: from
// the cached forecast for today and later, otherwise from the historical
// APIs (15-minute from 2022, hourly before). nil if unavailable.
func (s *Service) Curve(ctx context.Context, sys *systems.System, day time.Time) ([]Slot, error) {
	if !s.Enabled() {
		return nil, nil
	}
	req, ok := RequestFor(sys)
	if !ok {
		return nil, nil
	}
	end := day.AddDate(0, 0, 1)
	today := localDay(s.now(), req.Loc)
	var slots []Slot
	if !day.Before(today) {
		f, err := s.Get(ctx, sys)
		if err != nil || f == nil {
			return nil, err
		}
		slots = f.Slots
	} else {
		var err error
		if slots, err = s.client.History(ctx, req, day, day, true); err != nil {
			slog.WarnContext(ctx, "weather history fetch failed", "system", sys.ID, "error", err)
			return nil, nil
		}
	}
	var out []Slot
	for _, sl := range slots {
		if !sl.Start().Before(day) && !sl.End.After(end) {
			out = append(out, sl)
		}
	}
	return out, nil
}

// ExpectedDays returns the expectation by local date in [from, to): stored
// days from the backfill plus today and later from the forecast. Days
// without data are missing.
func (s *Service) ExpectedDays(ctx context.Context, sys *systems.System, from, to time.Time) (map[string]ExpectedDay, error) {
	if !s.Enabled() {
		return nil, nil
	}
	req, ok := RequestFor(sys)
	if !ok {
		return nil, nil
	}
	out, err := s.store.ExpectedDaily(ctx, sys.ID, req.Key(), from, to)
	if err != nil {
		return nil, err
	}
	today := localDay(s.now(), req.Loc)
	if to.After(today) {
		f, err := s.Get(ctx, sys)
		if err != nil {
			return nil, err
		}
		if f != nil {
			for d, v := range Daily(f.Slots, req.Loc) {
				if day, err := time.ParseInLocation(time.DateOnly, d, req.Loc); err == nil && !day.Before(maxTime(from, today)) && day.Before(to) {
					out[d] = v
				}
			}
		}
	}
	return out, nil
}

// Systems lists all systems for the backfill (implemented by the systems
// module).
type Systems interface {
	ListSystems(ctx context.Context, userID *uuid.UUID) ([]systems.SystemWithRole, error)
}

// Telemetry tells where a system's data starts (implemented by the telemetry
// module).
type Telemetry interface {
	FirstDataDay(ctx context.Context, systemID uuid.UUID) (*time.Time, error)
}

const (
	// backfillEvery is how often the backfill runs; yesterday is usually in
	// the historical API early the next morning.
	backfillEvery = time.Hour
	// backfillChunk is the most days fetched per request.
	backfillChunk = 366
)

// Backfill is a scheduler job that stores the expected energy of every past
// day of systems with coordinates, from their first data (or commissioning)
// day to yesterday. It recomputes everything when the coordinates or the
// orientation change.
func (s *Service) Backfill() scheduler.Job {
	return scheduler.Job{Name: "expected_daily", Run: func(ctx context.Context, deadline time.Time) error {
		if !s.Enabled() {
			return nil
		}
		unlock, ok, err := s.jobs.TryLockJob(ctx, "expected_daily", 5*time.Minute, backfillEvery)
		if err != nil || !ok {
			return err
		}
		defer unlock()
		all, err := s.sys.ListSystems(ctx, nil)
		if err != nil {
			return err
		}
		for i := range all {
			if s.now().After(deadline) {
				return nil
			}
			if err := s.backfillSystem(ctx, &all[i].System, deadline); err != nil {
				slog.WarnContext(ctx, "expected energy backfill failed", "system", all[i].ID, "error", err)
			}
		}
		return nil
	}}
}

func (s *Service) backfillSystem(ctx context.Context, sys *systems.System, deadline time.Time) error {
	req, ok := RequestFor(sys)
	if !ok {
		return nil
	}
	loc := req.Loc
	key, first, last, err := s.store.ExpectedDailyRange(ctx, sys.ID)
	if err != nil {
		return err
	}
	if key != req.Key() {
		if err := s.store.ResetExpectedDaily(ctx, sys.ID, req.Key()); err != nil {
			return err
		}
		first, last = nil, nil
	}
	start, err := s.firstDay(ctx, sys, loc)
	if err != nil || start == nil {
		return err
	}
	yesterday := localDay(s.now(), loc).AddDate(0, 0, -1)
	// Missing ranges: before the stored days and after them.
	var ranges [][2]time.Time
	if first == nil {
		ranges = append(ranges, [2]time.Time{*start, yesterday})
	} else {
		f := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc)
		l := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, loc)
		ranges = append(ranges, [2]time.Time{*start, f.AddDate(0, 0, -1)}, [2]time.Time{l.AddDate(0, 0, 1), yesterday})
	}
	for _, r := range ranges {
		for from := r[0]; !from.After(r[1]); from = from.AddDate(0, 0, backfillChunk) {
			if s.now().After(deadline) {
				return nil
			}
			to := from.AddDate(0, 0, backfillChunk-1)
			if to.After(r[1]) {
				to = r[1]
			}
			slots, err := s.client.History(ctx, req, from, to, false)
			if err != nil {
				return err
			}
			if err := s.store.PutExpectedDaily(ctx, sys.ID, Daily(slots, loc)); err != nil {
				return err
			}
		}
	}
	return nil
}

// firstDay is the earlier of the first data day and the commissioning date.
func (s *Service) firstDay(ctx context.Context, sys *systems.System, loc *time.Location) (*time.Time, error) {
	var out *time.Time
	f, err := s.tel.FirstDataDay(ctx, sys.ID)
	if err != nil {
		return nil, err
	}
	for _, d := range []*time.Time{f, sys.CommissionedAt} {
		if d == nil {
			continue
		}
		t := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
		if out == nil || t.Before(*out) {
			out = &t
		}
	}
	return out, nil
}

func localDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// ExpectedDailyRange returns the key the system's expected daily energy was
// computed for and its first and last day (nil if there is none).
func (s *Service) ExpectedDailyRange(ctx context.Context, systemID uuid.UUID) (string, *time.Time, *time.Time, error) {
	return s.store.ExpectedDailyRange(ctx, systemID)
}
