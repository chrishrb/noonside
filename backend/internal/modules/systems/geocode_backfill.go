package systems

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems/geocode"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
)

// Geocoder finds addresses (implemented by geocode.Client).
type Geocoder interface {
	Search(ctx context.Context, q, lang string, limit int) ([]geocode.Place, error)
	Reverse(ctx context.Context, lat, lon float64, lang string) (*geocode.Place, error)
}

// backfillEvery retries addresses that were not found.
const backfillEvery = time.Hour

// BackfillPause between lookups keeps within Photon's fair use.
const BackfillPause = time.Second

// GeocodeBackfill returns the scheduler job that geocodes systems with an
// address but no coordinates (e.g. created before addresses were geocoded),
// about once an hour, waiting pause between lookups.
func (m *Module) GeocodeBackfill(g Geocoder, pause time.Duration) scheduler.Job {
	logger := slog.Default().With("component", "geocode")
	return scheduler.Job{Name: "geocode_backfill", Run: func(ctx context.Context, deadline time.Time) error {
		unlock, ok, err := m.db.TryLockJob(ctx, "geocode_backfill", 5*time.Minute, backfillEvery)
		if err != nil || !ok {
			return err
		}
		defer unlock()
		systems, err := m.ListSystems(ctx, nil)
		if err != nil {
			return err
		}
		for _, sw := range systems {
			sys := sw.System
			if sys.Location == "" || sys.Latitude != nil || sys.Longitude != nil {
				continue
			}
			if time.Now().Add(pause).After(deadline) {
				return nil
			}
			places, err := g.Search(ctx, sys.Location, "", 1)
			if err != nil {
				return err
			}
			if len(places) == 0 {
				logger.InfoContext(ctx, "system address not found", "system_id", sys.ID)
			} else if err := m.SetSystemCoordinates(ctx, sys.ID, sys.Location, places[0].Lat, places[0].Lon); err != nil {
				return err
			}
			time.Sleep(pause)
		}
		return nil
	}}
}
