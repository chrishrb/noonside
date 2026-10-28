// Package prices fetches day-ahead electricity prices (EPEX spot) per
// bidding zone into market_prices: from aWATTar (free, DE-LU and AT) or,
// with a token, from the ENTSO-E Transparency Platform. Fetching runs in the
// scheduler only; requests read the stored prices.
package prices

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/google/uuid"
)

// Source fetches the prices of a zone for [from, to).
type Source interface {
	Fetch(ctx context.Context, zone string, from, to time.Time) ([]MarketPrice, error)
	// Name is shown as attribution.
	Name() string
}

// Timeout bounds each request.
const Timeout = 30 * time.Second

// zoneLoc is the market's time zone (all supported zones are CET).
var zoneLoc = mustLoc("Europe/Berlin")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func get(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "noonside")
	res, err := c.Do(req)
	if err != nil {
		return nil, redact(err)
	}
	defer res.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return body, fmt.Errorf("%s: %s", res.Status, msg)
	}
	return body, nil
}

// redact drops the URL from request errors (ENTSO-E's carries the token).
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("price request failed: %w", ue.Err)
	}
	return err
}

// normalize sorts the prices and, where intervals of different lengths
// overlap (a market moving from hourly to 15 minutes), keeps the finer ones.
func normalize(ps []MarketPrice) []MarketPrice {
	sort.Slice(ps, func(i, j int) bool {
		if !ps[i].Start.Equal(ps[j].Start) {
			return ps[i].Start.Before(ps[j].Start)
		}
		return ps[i].End.Before(ps[j].End)
	})
	out := ps[:0]
	for _, p := range ps {
		if !p.End.After(p.Start) {
			continue
		}
		if n := len(out); n > 0 && p.Start.Before(out[n-1].End) {
			// Overlap: keep the shorter interval.
			if p.End.Sub(p.Start) < out[n-1].End.Sub(out[n-1].Start) {
				out[n-1] = p
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

// Negative returns the intervals with a negative price, adjacent ones merged.
func Negative(ps []MarketPrice) []MarketPrice {
	var out []MarketPrice
	for _, p := range ps {
		if p.EURMWh >= 0 {
			continue
		}
		if n := len(out); n > 0 && out[n-1].End.Equal(p.Start) {
			// Merged intervals carry the lowest price.
			out[n-1].End = p.End
			out[n-1].EURMWh = min(out[n-1].EURMWh, p.EURMWh)
			continue
		}
		out = append(out, p)
	}
	return out
}

// Hours sums the length of the intervals in hours.
func Hours(ps []MarketPrice) float64 {
	var d time.Duration
	for _, p := range ps {
		d += p.End.Sub(p.Start)
	}
	return d.Hours()
}

// Clip limits the prices to [from, to).
func Clip(ps []MarketPrice, from, to time.Time) []MarketPrice {
	var out []MarketPrice
	for _, p := range ps {
		if !p.End.After(from) || !p.Start.Before(to) {
			continue
		}
		if p.Start.Before(from) {
			p.Start = from
		}
		if p.End.After(to) {
			p.End = to
		}
		out = append(out, p)
	}
	return out
}

// --- scheduler job ---------------------------------------------------------

// Store is what the job needs.
type Store interface {
	ListSystems(ctx context.Context, userID *uuid.UUID) ([]systems.SystemWithRole, error)
	TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (unlock func(), ok bool, err error)
	PriceStore
}

const (
	// jobEvery is how often prices are fetched; the next day's prices are
	// published around 13:00 CET.
	jobEvery = time.Hour
	// backfillChunk is the most days fetched per backfill request; one
	// chunk per zone and run keeps within the providers' fair use.
	backfillChunk = 31
)

// BackfillFrom is the oldest day fetched: history for the negative-price
// hours of the last years.
var BackfillFrom = time.Date(2024, time.January, 1, 0, 0, 0, 0, zoneLoc)

// Job fetches the prices of every zone a system uses: today and tomorrow
// until they are complete, and one chunk of history per run.
type Job struct {
	store  Store
	source Source
	now    func() time.Time
	logger *slog.Logger
}

// NewJob returns the job; a nil source disables it.
func NewJob(st Store, src Source) *Job {
	return &Job{store: st, source: src, now: time.Now, logger: slog.Default().With("component", "prices")}
}

// SetClock replaces the clock (tests).
func (j *Job) SetClock(now func() time.Time) { j.now = now }

// Enabled reports whether prices are fetched.
func (j *Job) Enabled() bool { return j != nil && j.source != nil }

// SourceName is the provider's name for attribution ("" if disabled).
func (j *Job) SourceName() string {
	if !j.Enabled() {
		return ""
	}
	return j.source.Name()
}

func (j *Job) Run(ctx context.Context, deadline time.Time) error {
	if !j.Enabled() {
		return nil
	}
	unlock, ok, err := j.store.TryLockJob(ctx, "market_prices", 5*time.Minute, jobEvery-time.Minute)
	if err != nil || !ok {
		return err
	}
	defer unlock()
	systems, err := j.store.ListSystems(ctx, nil)
	if err != nil {
		return err
	}
	var zones []string
	for _, s := range systems {
		if z := s.PriceZone; z != nil && !slices.Contains(zones, *z) {
			zones = append(zones, *z)
		}
	}
	slices.Sort(zones)
	for _, z := range zones {
		if j.now().After(deadline) {
			return nil
		}
		if err := j.RunZone(ctx, z); err != nil {
			j.logger.Warn("fetch day-ahead prices", "zone", z, "error", err)
		}
	}
	return nil
}

// RunZone fetches what is missing of the zone.
func (j *Job) RunZone(ctx context.Context, zone string) error {
	now := j.now().In(zoneLoc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zoneLoc)
	end := today.AddDate(0, 0, 2) // through tomorrow
	first, last, err := j.store.MarketPriceRange(ctx, zone)
	if err != nil {
		return err
	}
	if last == nil || last.Before(end) {
		from := today.AddDate(0, 0, -1)
		if last != nil && last.After(from) {
			from = *last
		}
		if first == nil { // new zone: start with the recent days, the backfill follows
			from = today.AddDate(0, 0, -backfillChunk)
		}
		ps, err := j.source.Fetch(ctx, zone, from, end)
		if err != nil {
			return err
		}
		if err := j.store.UpsertMarketPrices(ctx, ps); err != nil {
			return err
		}
		if first == nil && len(ps) > 0 {
			first = &ps[0].Start
		}
	}
	if first != nil && first.After(BackfillFrom) {
		to := *first
		from := to.AddDate(0, 0, -backfillChunk)
		if from.Before(BackfillFrom) {
			from = BackfillFrom
		}
		ps, err := j.source.Fetch(ctx, zone, from, to)
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			// Both providers have years of history, so this is a gap at the
			// provider; it is asked again next run.
			j.logger.Info("no older day-ahead prices", "zone", zone, "before", to.Format(time.DateOnly))
			return nil
		}
		return j.store.UpsertMarketPrices(ctx, ps)
	}
	return nil
}
