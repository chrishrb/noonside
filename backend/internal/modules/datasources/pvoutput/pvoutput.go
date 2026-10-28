// Package pvoutput uploads a system's data to PVOutput.org (data source kind
// pvoutput_export): 5-minute status (power and the day's energy) and the
// daily output of finished days. It runs as a data source, so the scheduler
// drives it on its interval; the progress is kept in data_sources.state.
package pvoutput

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/google/uuid"
)

// Config is the JSON config of a pvoutput_export data source.
type Config struct {
	SystemID string `json:"pvoutput_system_id,omitempty"`
	// APIKeyEnc is the PVOutput API key sealed with the server's secret key.
	APIKeyEnc string `json:"api_key_enc,omitempty"`
}

// State is what was uploaded (data_sources.state).
type State struct {
	// StatusUntil is the end of the last status uploaded.
	StatusUntil time.Time `json:"status_until"`
	// OutputUntil is the last local date whose output was uploaded.
	OutputUntil string `json:"output_until,omitempty"`
}

// ParseState reads a data source's state; an empty or invalid one is zero.
func ParseState(raw []byte) State {
	var s State
	_ = json.Unmarshal(raw, &s)
	return s
}

const (
	// bucket is the status interval; the PVOutput system should use 5 minutes.
	bucket = 5 * time.Minute
	// maxAge is how old a status or output may be (without PVOutput's
	// donation mode).
	maxAge = 14 * 24 * time.Hour
	// batchSize is the most statuses per request (30 without donation mode).
	batchSize = 30
	// maxStatusRequests per run keep within the 60 requests an hour at a
	// 5-minute interval (plus one output request).
	maxStatusRequests = 3
	// settleAfter past midnight a day's output is uploaded.
	settleAfter = 3 * time.Hour
	// Timeout bounds each request.
	Timeout = 30 * time.Second
)

// Store is what the exporter reads and writes.
type Store interface {
	GetSystem(ctx context.Context, id uuid.UUID) (*systems.System, error)
	LastMeasurementTime(ctx context.Context, systemID uuid.UUID) (*time.Time, error)
	PowerCurve(ctx context.Context, systemID uuid.UUID, from, to time.Time, bucket time.Duration) ([]telemetry.PowerPoint, error)
	Energy(ctx context.Context, systemID uuid.UUID, granularity string, from, to time.Time) ([]telemetry.EnergyBucket, error)
	SetDataSourceState(ctx context.Context, id uuid.UUID, state []byte) error
}

// Exporter uploads one system to one PVOutput system.
type Exporter struct {
	store        Store
	baseURL      string
	dataSourceID uuid.UUID
	systemID     uuid.UUID
	pvoSystemID  string
	apiKey       string
	state        State
	http         *http.Client
	now          func() time.Time
	logger       *slog.Logger
}

// Source is the data source the exporter belongs to.
type Source struct {
	ID, SystemID uuid.UUID
	// State is the stored progress (JSON).
	State []byte
}

// New returns the exporter of data source ds; apiKey is the opened key.
func New(st Store, baseURL string, ds Source, cfg Config, apiKey string) *Exporter {
	return &Exporter{
		store: st, baseURL: strings.TrimRight(baseURL, "/"), dataSourceID: ds.ID, systemID: ds.SystemID,
		pvoSystemID: cfg.SystemID, apiKey: apiKey, state: ParseState(ds.State),
		http: errtrack.HTTPClient(Timeout), now: time.Now,
		logger: slog.Default().With("component", "adapter.pvoutput", "system_id", ds.SystemID),
	}
}

// SetClock replaces the clock (tests).
func (e *Exporter) SetClock(now func() time.Time) { e.now = now }

// Poll uploads the daily output of finished days, then new statuses.
func (e *Exporter) Poll(ctx context.Context) error {
	if e.pvoSystemID == "" || e.apiKey == "" {
		return fmt.Errorf("PVOutput system ID and API key are required")
	}
	sys, err := e.store.GetSystem(ctx, e.systemID)
	if err != nil {
		return err
	}
	if err := e.uploadOutputs(ctx, sys); err != nil {
		return err
	}
	return e.uploadStatuses(ctx, sys)
}

func (e *Exporter) save(ctx context.Context) error {
	b, err := json.Marshal(e.state)
	if err != nil {
		return err
	}
	return e.store.SetDataSourceState(context.WithoutCancel(ctx), e.dataSourceID, b)
}

func localDay(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// uploadOutputs sends the energy of each finished day since the last upload
// (at most 14 days back) in one batch. A day counts as finished settleAfter
// past midnight, when late exports have arrived.
func (e *Exporter) uploadOutputs(ctx context.Context, sys *systems.System) error {
	loc := sys.Loc()
	now := e.now()
	today := localDay(now.Add(-settleAfter), loc)
	from := localDay(now.Add(-maxAge), loc).AddDate(0, 0, 1)
	if d, err := time.ParseInLocation(time.DateOnly, e.state.OutputUntil, loc); err == nil && !d.Before(from) {
		from = d.AddDate(0, 0, 1)
	}
	if !from.Before(today) {
		return nil
	}
	buckets, err := e.store.Energy(ctx, sys.ID, "day", from, today)
	if err != nil {
		return err
	}
	perDay := map[string]float64{}
	for _, b := range buckets {
		perDay[b.Start.Format(time.DateOnly)] += b.EnergyWh
	}
	var rows []string
	for d := from; d.Before(today) && len(rows) < batchSize; d = d.AddDate(0, 0, 1) {
		wh := perDay[d.Format(time.DateOnly)]
		if wh <= 0 {
			continue
		}
		row := fmt.Sprintf("%s,%d,", d.Format("20060102"), int64(wh+0.5))
		peak, at, err := e.peak(ctx, sys.ID, d, d.AddDate(0, 0, 1))
		if err != nil {
			return err
		}
		if peak > 0 {
			row += fmt.Sprintf(",%d,%s", int64(peak+0.5), at.In(loc).Format("15:04"))
		}
		rows = append(rows, row)
	}
	if len(rows) > 0 {
		if err := e.post(ctx, "/service/r2/addbatchoutput.jsp", strings.Join(rows, ";")); err != nil {
			return fmt.Errorf("upload daily output: %w", err)
		}
	}
	e.state.OutputUntil = today.AddDate(0, 0, -1).Format(time.DateOnly)
	return e.save(ctx)
}

// peak returns the highest 5-minute system power in [from, to) and when.
func (e *Exporter) peak(ctx context.Context, systemID uuid.UUID, from, to time.Time) (float64, time.Time, error) {
	pts, err := e.store.PowerCurve(ctx, systemID, from, to, bucket)
	if err != nil {
		return 0, time.Time{}, err
	}
	var peak float64
	var at time.Time
	for _, p := range sumByTime(pts) {
		if p.w > peak {
			peak, at = p.w, p.t
		}
	}
	return peak, at, nil
}

type total struct {
	t time.Time
	w float64
}

// sumByTime adds up the inverters per bucket, ordered by time.
func sumByTime(pts []telemetry.PowerPoint) []total {
	by := map[time.Time]float64{}
	for _, p := range pts {
		by[p.Time] += p.PacW
	}
	out := make([]total, 0, len(by))
	for t, w := range by {
		out = append(out, total{t, w})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].t.Before(out[j].t) })
	return out
}

// uploadStatuses sends the complete 5-minute buckets since the last upload
// (from today's start on the first run, at most 14 days back): power and
// the energy of the day so far, integrated from the power.
func (e *Exporter) uploadStatuses(ctx context.Context, sys *systems.System) error {
	loc := sys.Loc()
	now := e.now()
	last, err := e.store.LastMeasurementTime(ctx, sys.ID)
	if err != nil || last == nil {
		return err
	}
	// The bucket with the newest sample may still fill up.
	end := last.Truncate(bucket)
	from := e.state.StatusUntil
	if from.IsZero() {
		from = localDay(now, loc)
	}
	if oldest := now.Add(-maxAge).Add(time.Hour).Truncate(bucket); from.Before(oldest) {
		from = oldest
	}
	for posts := 0; posts < maxStatusRequests && from.Before(end); {
		day := localDay(from, loc)
		dayEnd := day.AddDate(0, 0, 1)
		// The day's curve from midnight, for the energy so far.
		pts, err := e.store.PowerCurve(ctx, sys.ID, day, minTime(dayEnd, end), bucket)
		if err != nil {
			return err
		}
		var rows []string
		var energy float64
		next := minTime(dayEnd, end)
		for _, p := range sumByTime(pts) {
			energy += p.w * bucket.Hours()
			if p.t.Before(from) {
				continue
			}
			if len(rows) == batchSize {
				next = p.t
				break
			}
			rows = append(rows, fmt.Sprintf("%s,%s,%d,%d", p.t.In(loc).Format("20060102"), p.t.In(loc).Format("15:04"),
				int64(energy+0.5), int64(p.w+0.5)))
		}
		if len(rows) > 0 {
			if err := e.post(ctx, "/service/r2/addbatchstatus.jsp", strings.Join(rows, ";")); err != nil {
				return fmt.Errorf("upload status: %w", err)
			}
			posts++
		}
		e.state.StatusUntil, from = next, next
		if err := e.save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// post sends data to a PVOutput service; any other status than 200 is an
// error with PVOutput's message (e.g. the rate limit).
func (e *Exporter) post(ctx context.Context, path, data string) error {
	form := url.Values{"data": {data}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Pvoutput-Apikey", e.apiKey)
	req.Header.Set("X-Pvoutput-SystemId", e.pvoSystemID)
	res, err := e.http.Do(req)
	if err != nil {
		if ue, ok := err.(*url.Error); ok {
			return fmt.Errorf("request failed: %w", ue.Err)
		}
		return err
	}
	defer res.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("PVOutput: %s", strings.TrimSpace(string(body)))
	}
	e.logger.Debug("uploaded to PVOutput", "service", path, "response", strings.TrimSpace(string(body)))
	return nil
}
