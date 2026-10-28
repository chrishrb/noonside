package identity

import (
	"context"

	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const profileCols = `id, email, name, avatar_key, locale, created_at, updated_at`

func scanProfile(r pgx.Row) (*Profile, error) {
	var p Profile
	if err := r.Scan(&p.ID, &p.Email, &p.Name, &p.AvatarKey, &p.Locale, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, db.MapErr(err)
	}
	return &p, nil
}

func (m *Module) GetProfile(ctx context.Context, id uuid.UUID) (*Profile, error) {
	return scanProfile(m.db.Q(ctx).QueryRow(ctx, `SELECT `+profileCols+` FROM profiles WHERE id = $1`, id))
}

func (m *Module) GetProfileByEmail(ctx context.Context, email string) (*Profile, error) {
	return scanProfile(m.db.Q(ctx).QueryRow(ctx, `SELECT `+profileCols+` FROM profiles WHERE email = $1`, email))
}

// Profiles returns the profiles with the given ids by id; unknown ids are
// left out.
func (m *Module) Profiles(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Profile, error) {
	out := map[uuid.UUID]Profile{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+profileCols+` FROM profiles WHERE id = ANY($1)`, ids)
	ps, err := db.Collect(rows, err, func(r pgx.Row) (*Profile, error) { return scanProfile(r) })
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		out[p.ID] = *p
	}
	return out, nil
}

func (m *Module) UpdateProfileName(ctx context.Context, id uuid.UUID, name string) (*Profile, error) {
	return scanProfile(m.db.Q(ctx).QueryRow(ctx, `
		UPDATE profiles SET name = $2, updated_at = now() WHERE id = $1 RETURNING `+profileCols, id, name))
}

func (m *Module) UpdateProfileLocale(ctx context.Context, id uuid.UUID, locale string) (*Profile, error) {
	return scanProfile(m.db.Q(ctx).QueryRow(ctx, `
		UPDATE profiles SET locale = $2, updated_at = now() WHERE id = $1 RETURNING `+profileCols, id, locale))
}

// SetAvatarKey sets the profile picture's key (nil: none) and returns the
// previous one.
func (m *Module) SetAvatarKey(ctx context.Context, id uuid.UUID, key *string) (old *string, err error) {
	err = m.db.Q(ctx).QueryRow(ctx, `
		UPDATE profiles p SET avatar_key = $2, updated_at = now()
		FROM (SELECT avatar_key FROM profiles WHERE id = $1 FOR UPDATE) o
		WHERE p.id = $1 RETURNING o.avatar_key`, id, key).Scan(&old)
	return old, db.MapErr(err)
}

// Accounts read auth.users directly (the server connects as postgres) for the
// last sign-in, the ban and the superadmin flag in app_metadata.
const accountSelect = `
	SELECT p.id, p.email, p.name, p.avatar_key, p.created_at, p.updated_at,
		coalesce((u.raw_app_meta_data ->> '` + auth.SuperadminClaim + `')::boolean, false),
		u.last_sign_in_at, coalesce(u.banned_until > now(), false)
	FROM profiles p JOIN auth.users u ON u.id = p.id`

func scanAccount(r pgx.Row) (Account, error) {
	var a Account
	err := r.Scan(&a.ID, &a.Email, &a.Name, &a.AvatarKey, &a.CreatedAt, &a.UpdatedAt, &a.Superadmin, &a.LastSignInAt, &a.Disabled)
	return a, db.MapErr(err)
}

// ListAccounts lists every user with their superadmin flag, last sign-in
// and system roles, ordered by name.
func (m *Module) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := m.db.Q(ctx).Query(ctx, accountSelect+` ORDER BY lower(p.name), p.email`)
	accounts, err := db.Collect(rows, err, scanAccount)
	if err != nil {
		return nil, err
	}
	return accounts, m.addSystems(ctx, accounts, nil)
}

func (m *Module) GetAccount(ctx context.Context, id uuid.UUID) (*Account, error) {
	a, err := scanAccount(m.db.Q(ctx).QueryRow(ctx, accountSelect+` WHERE p.id = $1`, id))
	if err != nil {
		return nil, err
	}
	accounts := []Account{a}
	if err := m.addSystems(ctx, accounts, []uuid.UUID{id}); err != nil {
		return nil, err
	}
	return &accounts[0], nil
}

func (m *Module) addSystems(ctx context.Context, accounts []Account, ids []uuid.UUID) error {
	var byUser map[uuid.UUID][]AccountSystem
	if m.memberships != nil {
		var err error
		if byUser, err = m.memberships.MembershipsByUser(ctx, ids); err != nil {
			return err
		}
	}
	for i := range accounts {
		accounts[i].Systems = byUser[accounts[i].ID]
		if accounts[i].Systems == nil {
			accounts[i].Systems = []AccountSystem{}
		}
	}
	return nil
}
