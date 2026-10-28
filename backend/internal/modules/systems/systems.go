// Package systems is the systems module: PV systems with their planes,
// monthly targets and inverters, and who may access them (members,
// invitations, API keys, share links). It owns the tables systems,
// system_planes, system_targets, inverters, system_members, invitations,
// api_keys and share_links.
package systems

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/google/uuid"
)

// Module is the systems module.
type Module struct {
	db          *db.DB
	profiles    Profiles
	deleteHooks []DeleteHook
	media       media.Store
	logger      *slog.Logger
}

// DeleteHook runs before a system is deleted (its rows cascade). The
// returned function, if any, runs after the deletion succeeded, e.g. to
// clean up external resources of the deleted rows.
type DeleteHook func(ctx context.Context, systemID uuid.UUID) (after func(ctx context.Context), err error)

// Profiles reads user profiles (implemented by the identity module).
type Profiles interface {
	Profiles(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]identity.Profile, error)
}

func New(d *db.DB, profiles Profiles) *Module {
	return &Module{db: d, profiles: profiles, logger: slog.Default()}
}

// OnDelete registers a hook for system deletions.
func (m *Module) OnDelete(h DeleteHook) { m.deleteHooks = append(m.deleteHooks, h) }

// InTx runs fn in a transaction (see db.DB.InTx).
func (m *Module) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.db.InTx(ctx, fn)
}

// DeleteSystem deletes the system with everything that belongs to it.
func (m *Module) DeleteSystem(ctx context.Context, id uuid.UUID) error {
	var after []func(context.Context)
	for _, h := range m.deleteHooks {
		fn, err := h(ctx, id)
		if err != nil {
			return err
		}
		if fn != nil {
			after = append(after, fn)
		}
	}
	sys, err := m.GetSystem(ctx, id)
	if err != nil {
		return err
	}
	if err := m.deleteSystem(ctx, id); err != nil {
		return err
	}
	m.deleteObject(ctx, sys.ImageKey)
	for _, fn := range after {
		fn(ctx)
	}
	return nil
}

// MemberRole returns the user's role on the system, or db.ErrNotFound
// (implements auth.RoleLookup).
func (m *Module) MemberRole(ctx context.Context, systemID, userID uuid.UUID) (auth.Role, error) {
	mem, err := m.GetMember(ctx, systemID, userID)
	if err != nil {
		return "", err
	}
	return mem.Role, nil
}

type System struct {
	ID             uuid.UUID
	Name           string
	Location       string
	Latitude       *float64
	Longitude      *float64
	Timezone       string
	PeakPowerKWp   float64
	CommissionedAt *time.Time
	Orientation    string
	// Planes are the module fields for the weather model; none means
	// horizontal panels.
	Planes       []Plane
	ModuleInfo   string
	Operator     string
	TariffPerKWh float64
	Currency     string
	CO2KgPerKWh  float64
	// MinuteRetentionYears keeps minute data for the current year plus this
	// many full years; nil keeps everything.
	MinuteRetentionYears *int
	// PriceZone selects the day-ahead prices (nil: none).
	PriceZone *string
	// UnpaidAtNegativePrices drops the feed-in revenue while the day-ahead
	// price is negative (§ 51 EEG).
	UnpaidAtNegativePrices bool
	// ImageKey names the system's picture in the media store (nil: none).
	ImageKey  *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Plane is a module field with one orientation (azimuth 0 = south,
// -90 = east, 90 = west).
type Plane struct {
	// ID is zero for a new plane; saving keeps the planes with a known ID.
	ID         uuid.UUID
	Name       string
	KWp        float64
	TiltDeg    float64
	AzimuthDeg float64
}

// MaxPlanes is the most planes a system may have.
const MaxPlanes = 10

// MinuteCutoff returns the start of the oldest local calendar year whose
// minute data is kept, or nil if everything is kept.
func (s *System) MinuteCutoff(now time.Time) *time.Time {
	if s.MinuteRetentionYears == nil {
		return nil
	}
	loc := s.Loc()
	t := time.Date(now.In(loc).Year()-*s.MinuteRetentionYears, time.January, 1, 0, 0, 0, 0, loc)
	return &t
}

// Loc returns the system's time zone, falling back to UTC.
func (s *System) Loc() *time.Location {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// SystemWithRole is a system as seen by one user.
type SystemWithRole struct {
	System
	Role auth.Role
}

// AdminSystem is a system with the counts and owners the superadmin's
// system list shows.
type AdminSystem struct {
	System
	InverterCount int
	MemberCount   int
	Owners        []Member // ordered by name, then email
}

// Member is a user's membership with their profile's name and email.
type Member struct {
	SystemID  uuid.UUID
	UserID    uuid.UUID
	Email     string
	Name      string
	Locale    string
	AvatarKey *string
	Role      auth.Role
	CreatedAt time.Time
}

type Membership struct {
	SystemID uuid.UUID
	Role     auth.Role
}

type Invitation struct {
	ID         uuid.UUID
	SystemID   uuid.UUID
	Email      string
	Role       auth.Role
	TokenHash  []byte
	InvitedBy  *uuid.UUID
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	RevokedAt  *time.Time
}

type APIKey struct {
	ID         uuid.UUID
	SystemID   uuid.UUID
	Name       string
	Prefix     string
	KeyHash    []byte
	Scopes     []string
	CreatedBy  *uuid.UUID
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// ShareLink is a read-only public link to a system. Only the token's hash is
// stored.
type ShareLink struct {
	ID             uuid.UUID
	SystemID       uuid.UUID
	Label          string
	Prefix         string
	TokenHash      []byte
	ShowFinancials bool
	ExpiresAt      *time.Time
	CreatedBy      *uuid.UUID
	CreatedByName  string
	CreatedAt      time.Time
	RevokedAt      *time.Time
}

// Active reports whether the link may be used at now.
func (l *ShareLink) Active(now time.Time) bool {
	return l.RevokedAt == nil && (l.ExpiresAt == nil || l.ExpiresAt.After(now))
}

type Inverter struct {
	ID           uuid.UUID
	SystemID     uuid.UUID
	ExternalRef  string
	Name         string
	Model        string
	SerialNumber string
	PeakPowerKWp float64
	// MaxACPowerW is the rated AC power (nil: unknown), for clipping.
	MaxACPowerW *float64
	// PlaneID is the system plane the inverter's modules are on (nil: none).
	PlaneID   *uuid.UUID
	Color     string
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}
