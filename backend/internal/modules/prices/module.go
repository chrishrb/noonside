package prices

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/scheduler"
	"github.com/google/uuid"
)

// The prices module keeps day-ahead prices per bidding zone in the table
// market_prices, fetched only by its scheduler job.

// Bidding zones with day-ahead prices.
const (
	ZoneDELU = "DE-LU"
	ZoneAT   = "AT"
)

// PriceZones are the supported bidding zones.
var PriceZones = []string{ZoneDELU, ZoneAT}

// MarketPrice is the day-ahead price of one interval.
type MarketPrice struct {
	Zone   string
	Start  time.Time
	End    time.Time
	EURMWh float64
}

// PriceStore holds day-ahead prices per bidding zone.
type PriceStore interface {
	UpsertMarketPrices(ctx context.Context, ps []MarketPrice) error
	// MarketPrices returns the zone's intervals overlapping [from, to),
	// ordered by start.
	MarketPrices(ctx context.Context, zone string, from, to time.Time) ([]MarketPrice, error)
	// NegativeMarketPrices returns the zone's intervals with a negative
	// price overlapping [from, to), and whether there are any prices there.
	NegativeMarketPrices(ctx context.Context, zone string, from, to time.Time) (neg []MarketPrice, any bool, err error)
	// MarketPriceRange returns the start of the zone's first interval and
	// the end of its last (nil if there are none).
	MarketPriceRange(ctx context.Context, zone string) (first, last *time.Time, err error)
}

// Systems lists the systems whose zones are fetched (implemented by the
// systems module).
type Systems interface {
	ListSystems(ctx context.Context, userID *uuid.UUID) ([]systems.SystemWithRole, error)
}

// Telemetry measures production within intervals (implemented by the
// telemetry module).
type Telemetry interface {
	EnergyDuring(ctx context.Context, systemID uuid.UUID, intervals []telemetry.Interval) (map[uuid.UUID]float64, error)
}

// Module is the prices module.
type Module struct {
	*pgStore
	tel Telemetry
	job *Job
}

// jobStore is what the job reads: the prices and the systems' zones.
type jobStore struct {
	*pgStore
	Systems
	db.JobLocker
}

// New returns the module; a nil source disables fetching.
func New(d *db.DB, sys Systems, tel Telemetry, src Source) *Module {
	st := &pgStore{db: d}
	return &Module{pgStore: st, tel: tel, job: NewJob(jobStore{st, sys, d}, src)}
}

// Job is the scheduler job fetching prices.
func (m *Module) Job() scheduler.Job { return scheduler.Job{Name: "market_prices", Run: m.job.Run} }

// FetchJob exposes the job (tests drive its clock and zones).
func (m *Module) FetchJob() *Job { return m.job }

// SourceName is the provider's name for attribution ("" if disabled).
func (m *Module) SourceName() string { return m.job.SourceName() }

// NegativeDuring returns the energy produced while prices were negative
// (see the package function).
func (m *Module) NegativeDuring(ctx context.Context, sys *systems.System, from, to time.Time, only map[uuid.UUID]bool) (*NegativeEnergy, error) {
	return NegativeDuring(ctx, energyStore{m.pgStore, m.tel}, sys, from, to, only)
}

type energyStore struct {
	*pgStore
	Telemetry
}
