// Package identity is the identity module: user profiles and accounts.
// Accounts live in Supabase Auth; this module reads public.profiles (kept in
// sync by a trigger) and auth.users, and creates accounts through the Auth
// admin API. It owns the table profiles.
package identity

import (
	"context"
	"strings"
	"time"

	"log/slog"

	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/chrishrb/noonside/internal/platform/supabase"
	"github.com/google/uuid"
)

// UserAdmin manages Supabase Auth accounts (implemented by supabase.Admin).
type UserAdmin interface {
	CreateUser(ctx context.Context, r supabase.CreateUserRequest) (*supabase.User, error)
	SetAppMetadata(ctx context.Context, id uuid.UUID, values map[string]any) (*supabase.User, error)
	UpdateUser(ctx context.Context, id uuid.UUID, r supabase.UserUpdate) (*supabase.User, error)
	DeleteUser(ctx context.Context, id uuid.UUID) error
	SendRecovery(ctx context.Context, email, redirectTo string) error
}

// Memberships lists users' system roles (implemented by the systems module,
// which sits above identity).
type Memberships interface {
	// MembershipsByUser returns the system roles of every user (or of the
	// given ones), each ordered by system name.
	MembershipsByUser(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]AccountSystem, error)
	// SoleOwnerships returns the systems the user is the only owner of.
	SoleOwnerships(ctx context.Context, userID uuid.UUID) ([]AccountSystem, error)
}

// Module is the identity module.
type Module struct {
	db          *db.DB
	admin       UserAdmin
	memberships Memberships
	media       media.Store
	logger      *slog.Logger
}

func New(d *db.DB, admin UserAdmin) *Module {
	return &Module{db: d, admin: admin, logger: slog.Default()}
}

// SetMedia sets where profile pictures are stored (nil: no uploads).
func (m *Module) SetMedia(s media.Store) { m.media = s }

// SetMemberships wires the systems module in once it exists.
func (m *Module) SetMemberships(ms Memberships) { m.memberships = ms }

// NormalizeEmail lower-cases and trims an address.
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// NewUser is an account to create.
type NewUser struct {
	Email, Password, Name string
	Superadmin            bool
}

// CreateUser creates a confirmed account. Errors are those of the admin API
// (supabase.ErrUserExists, *supabase.APIError).
func (m *Module) CreateUser(ctx context.Context, u NewUser) (uuid.UUID, error) {
	created, err := m.admin.CreateUser(ctx, supabase.CreateUserRequest{
		Email: NormalizeEmail(u.Email), Password: u.Password, Name: strings.TrimSpace(u.Name), EmailConfirmed: true,
	})
	if err != nil {
		return uuid.Nil, err
	}
	if u.Superadmin {
		if err := m.SetSuperadmin(ctx, created.ID, true); err != nil {
			return uuid.Nil, err
		}
	}
	return created.ID, nil
}

// SetSuperadmin sets the superadmin flag in the user's app_metadata. It
// applies to the user's next access token.
func (m *Module) SetSuperadmin(ctx context.Context, id uuid.UUID, on bool) error {
	_, err := m.admin.SetAppMetadata(ctx, id, map[string]any{auth.SuperadminClaim: on})
	return err
}

// Profile mirrors a Supabase Auth user (public.profiles, synced by trigger).
type Profile struct {
	ID    uuid.UUID
	Email string
	Name  string
	// AvatarKey names the profile picture in the media store (nil: none).
	AvatarKey *string
	Locale    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Account is a profile plus the Supabase Auth fields superadmins see.
type Account struct {
	Profile
	Superadmin   bool
	LastSignInAt *time.Time
	// Disabled accounts are banned in Supabase Auth and cannot sign in.
	Disabled bool
	Systems  []AccountSystem
}

type AccountSystem struct {
	SystemID   uuid.UUID
	SystemName string
	Role       auth.Role
	// ImageKey is the system's picture (only from UserSystems).
	ImageKey *string
}

// Locale returns the caller's language for server-rendered text ("en" for
// API keys and unknown users).
func (m *Module) Locale(ctx context.Context, p *auth.Principal) string {
	if !p.IsUser() {
		return "en"
	}
	prof, err := m.GetProfile(ctx, p.UserID)
	if err != nil {
		return "en"
	}
	return prof.Locale
}
