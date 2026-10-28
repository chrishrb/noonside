package systems

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (m *Module) ListMembershipsForUser(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT system_id, role FROM system_members WHERE user_id = $1`, userID)
	return db.Collect(rows, err, func(r pgx.Row) (Membership, error) {
		var ms Membership
		return ms, r.Scan(&ms.SystemID, &ms.Role)
	})
}

const memberSelect = `SELECT system_id, user_id, role, created_at FROM system_members`

func scanMember(r pgx.Row) (Member, error) {
	var mem Member
	err := r.Scan(&mem.SystemID, &mem.UserID, &mem.Role, &mem.CreatedAt)
	return mem, db.MapErr(err)
}

// withProfiles fills the members' email, name and locale from their
// profiles (identity module).
func (m *Module) withProfiles(ctx context.Context, ms []Member) error {
	ids := make([]uuid.UUID, len(ms))
	for i := range ms {
		ids[i] = ms[i].UserID
	}
	profiles, err := m.profiles.Profiles(ctx, ids)
	if err != nil {
		return err
	}
	for i := range ms {
		p := profiles[ms[i].UserID]
		ms[i].Email, ms[i].Name, ms[i].Locale, ms[i].AvatarKey = p.Email, p.Name, p.Locale, p.AvatarKey
	}
	return nil
}

// ListMembers returns the system's members ordered by email.
func (m *Module) ListMembers(ctx context.Context, systemID uuid.UUID) ([]Member, error) {
	rows, err := m.db.Q(ctx).Query(ctx, memberSelect+` WHERE system_id = $1`, systemID)
	ms, err := db.Collect(rows, err, scanMember)
	if err != nil {
		return nil, err
	}
	if err := m.withProfiles(ctx, ms); err != nil {
		return nil, err
	}
	slices.SortFunc(ms, func(a, b Member) int { return strings.Compare(a.Email, b.Email) })
	return ms, nil
}

func (m *Module) GetMember(ctx context.Context, systemID, userID uuid.UUID) (*Member, error) {
	mem, err := scanMember(m.db.Q(ctx).QueryRow(ctx, memberSelect+` WHERE system_id = $1 AND user_id = $2`, systemID, userID))
	if err != nil {
		return nil, err
	}
	ms := []Member{mem}
	if err := m.withProfiles(ctx, ms); err != nil {
		return nil, err
	}
	return &ms[0], nil
}

func (m *Module) UpsertMember(ctx context.Context, systemID, userID uuid.UUID, role auth.Role) error {
	_, err := m.db.Q(ctx).Exec(ctx, `
		INSERT INTO system_members (system_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (system_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		systemID, userID, role)
	return db.MapErr(err)
}

func (m *Module) DeleteMember(ctx context.Context, systemID, userID uuid.UUID) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `DELETE FROM system_members WHERE system_id = $1 AND user_id = $2`, systemID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (m *Module) CountOwners(ctx context.Context, systemID uuid.UUID) (int, error) {
	var n int
	err := m.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM system_members WHERE system_id = $1 AND role = 'owner'`, systemID).Scan(&n)
	return n, err
}

const invitationCols = `id, system_id, email, role, token_hash, invited_by, created_at, expires_at, accepted_at, revoked_at`

func scanInvitation(r pgx.Row) (Invitation, error) {
	var i Invitation
	err := r.Scan(&i.ID, &i.SystemID, &i.Email, &i.Role, &i.TokenHash, &i.InvitedBy, &i.CreatedAt, &i.ExpiresAt, &i.AcceptedAt, &i.RevokedAt)
	return i, db.MapErr(err)
}

func (m *Module) CreateInvitation(ctx context.Context, inv *Invitation) (*Invitation, error) {
	i, err := scanInvitation(m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO invitations (system_id, email, role, token_hash, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+invitationCols,
		inv.SystemID, inv.Email, inv.Role, inv.TokenHash, inv.InvitedBy, inv.ExpiresAt))
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (m *Module) ListInvitations(ctx context.Context, systemID uuid.UUID) ([]Invitation, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+invitationCols+` FROM invitations
		WHERE system_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC`, systemID)
	return db.Collect(rows, err, scanInvitation)
}

func (m *Module) GetInvitationByHash(ctx context.Context, hash []byte) (*Invitation, error) {
	i, err := scanInvitation(m.db.Q(ctx).QueryRow(ctx, `SELECT `+invitationCols+` FROM invitations WHERE token_hash = $1`, hash))
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (m *Module) MarkInvitationAccepted(ctx context.Context, id uuid.UUID) error {
	_, err := m.db.Q(ctx).Exec(ctx, `UPDATE invitations SET accepted_at = now() WHERE id = $1`, id)
	return err
}

func (m *Module) RevokeInvitation(ctx context.Context, systemID, id uuid.UUID) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `UPDATE invitations SET revoked_at = now()
		WHERE system_id = $1 AND id = $2 AND revoked_at IS NULL AND accepted_at IS NULL`, systemID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

const apiKeyCols = `id, system_id, name, prefix, key_hash, scopes, created_by, created_at, last_used_at, revoked_at`

func scanAPIKey(r pgx.Row) (APIKey, error) {
	var k APIKey
	err := r.Scan(&k.ID, &k.SystemID, &k.Name, &k.Prefix, &k.KeyHash, &k.Scopes, &k.CreatedBy, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	return k, db.MapErr(err)
}

func (m *Module) CreateAPIKey(ctx context.Context, k *APIKey) (*APIKey, error) {
	out, err := scanAPIKey(m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO api_keys (system_id, name, prefix, key_hash, scopes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+apiKeyCols,
		k.SystemID, k.Name, k.Prefix, k.KeyHash, k.Scopes, k.CreatedBy))
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *Module) ListAPIKeys(ctx context.Context, systemID uuid.UUID) ([]APIKey, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `SELECT `+apiKeyCols+` FROM api_keys
		WHERE system_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`, systemID)
	return db.Collect(rows, err, scanAPIKey)
}

func (m *Module) GetAPIKeyByHash(ctx context.Context, hash []byte) (*APIKey, error) {
	k, err := scanAPIKey(m.db.Q(ctx).QueryRow(ctx, `SELECT `+apiKeyCols+` FROM api_keys WHERE key_hash = $1`, hash))
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (m *Module) TouchAPIKey(ctx context.Context, id uuid.UUID) error {
	_, err := m.db.Q(ctx).Exec(ctx, `UPDATE api_keys SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, id)
	return err
}

func (m *Module) RevokeAPIKey(ctx context.Context, systemID, id uuid.UUID) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `UPDATE api_keys SET revoked_at = now()
		WHERE system_id = $1 AND id = $2 AND revoked_at IS NULL`, systemID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

const shareLinkSelect = `
	SELECT l.id, l.system_id, l.label, l.prefix, l.token_hash, l.show_financials, l.expires_at,
		l.created_by, l.created_at, l.revoked_at
	FROM share_links l`

func scanShareLink(r pgx.Row) (ShareLink, error) {
	var l ShareLink
	err := r.Scan(&l.ID, &l.SystemID, &l.Label, &l.Prefix, &l.TokenHash, &l.ShowFinancials, &l.ExpiresAt,
		&l.CreatedBy, &l.CreatedAt, &l.RevokedAt)
	return l, db.MapErr(err)
}

func (m *Module) CreateShareLink(ctx context.Context, l *ShareLink) (*ShareLink, error) {
	var id uuid.UUID
	if err := m.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO share_links (system_id, label, prefix, token_hash, show_financials, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		l.SystemID, l.Label, l.Prefix, l.TokenHash, l.ShowFinancials, l.ExpiresAt, l.CreatedBy).Scan(&id); err != nil {
		return nil, db.MapErr(err)
	}
	out, err := scanShareLink(m.db.Q(ctx).QueryRow(ctx, shareLinkSelect+` WHERE l.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return m.withCreator(ctx, out)
}

func (m *Module) ListShareLinks(ctx context.Context, systemID uuid.UUID, now time.Time) ([]ShareLink, error) {
	rows, err := m.db.Q(ctx).Query(ctx, shareLinkSelect+`
		WHERE l.system_id = $1 AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > $2)
		ORDER BY l.created_at DESC`, systemID, now)
	ls, err := db.Collect(rows, err, scanShareLink)
	if err != nil {
		return nil, err
	}
	return ls, m.withCreators(ctx, ls)
}

func (m *Module) GetShareLinkByHash(ctx context.Context, hash []byte) (*ShareLink, error) {
	l, err := scanShareLink(m.db.Q(ctx).QueryRow(ctx, shareLinkSelect+` WHERE l.token_hash = $1`, hash))
	if err != nil {
		return nil, err
	}
	return m.withCreator(ctx, l)
}

func (m *Module) RevokeShareLink(ctx context.Context, systemID, id uuid.UUID) error {
	tag, err := m.db.Q(ctx).Exec(ctx, `UPDATE share_links SET revoked_at = now()
		WHERE system_id = $1 AND id = $2 AND revoked_at IS NULL`, systemID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (m *Module) withCreator(ctx context.Context, l ShareLink) (*ShareLink, error) {
	ls := []ShareLink{l}
	if err := m.withCreators(ctx, ls); err != nil {
		return nil, err
	}
	return &ls[0], nil
}

// withCreators fills the links' creator names from their profiles.
func (m *Module) withCreators(ctx context.Context, ls []ShareLink) error {
	var ids []uuid.UUID
	for _, l := range ls {
		if l.CreatedBy != nil {
			ids = append(ids, *l.CreatedBy)
		}
	}
	profiles, err := m.profiles.Profiles(ctx, ids)
	if err != nil {
		return err
	}
	for i := range ls {
		if ls[i].CreatedBy != nil {
			ls[i].CreatedByName = profiles[*ls[i].CreatedBy].Name
		}
	}
	return nil
}

// UserSystems returns all systems (as owner) for nil, else the user's
// memberships, ordered by name.
func (m *Module) UserSystems(ctx context.Context, userID *uuid.UUID) ([]identity.AccountSystem, error) {
	systems, err := m.ListSystems(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]identity.AccountSystem, len(systems))
	for i, s := range systems {
		out[i] = identity.AccountSystem{SystemID: s.ID, SystemName: s.Name, Role: s.Role, ImageKey: s.ImageKey}
	}
	return out, nil
}

// MembershipsByUser returns the system roles of every user (or of the given
// ones), each ordered by system name.
func (m *Module) MembershipsByUser(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]identity.AccountSystem, error) {
	q := `SELECT m.user_id, s.id, s.name, m.role FROM system_members m JOIN systems s ON s.id = m.system_id`
	args := []any{}
	if userIDs != nil {
		q += ` WHERE m.user_id = ANY($1)`
		args = append(args, userIDs)
	}
	rows, err := m.db.Q(ctx).Query(ctx, q+` ORDER BY s.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]identity.AccountSystem{}
	for rows.Next() {
		var user uuid.UUID
		var a identity.AccountSystem
		if err := rows.Scan(&user, &a.SystemID, &a.SystemName, &a.Role); err != nil {
			return nil, err
		}
		out[user] = append(out[user], a)
	}
	return out, rows.Err()
}

// SoleOwnerships returns the systems the user is the only owner of, ordered
// by name.
func (m *Module) SoleOwnerships(ctx context.Context, userID uuid.UUID) ([]identity.AccountSystem, error) {
	rows, err := m.db.Q(ctx).Query(ctx, `
		SELECT s.id, s.name, m.role FROM system_members m JOIN systems s ON s.id = m.system_id
		WHERE m.user_id = $1 AND m.role = 'owner'
			AND NOT EXISTS (SELECT 1 FROM system_members o
				WHERE o.system_id = m.system_id AND o.role = 'owner' AND o.user_id <> m.user_id)
		ORDER BY s.name`, userID)
	return db.Collect(rows, err, func(r pgx.Row) (identity.AccountSystem, error) {
		var a identity.AccountSystem
		return a, r.Scan(&a.SystemID, &a.SystemName, &a.Role)
	})
}

// ShareLinkPrefix starts every share link token.
const ShareLinkPrefix = "s0p_"

// ResolveShareLink returns an active share link and its system. Unknown,
// revoked and expired links are all db.ErrNotFound.
func (m *Module) ResolveShareLink(ctx context.Context, token string, now time.Time) (*ShareLink, *System, error) {
	if !strings.HasPrefix(token, ShareLinkPrefix) {
		return nil, nil, db.ErrNotFound
	}
	l, err := m.GetShareLinkByHash(ctx, auth.HashToken(token))
	if errors.Is(err, db.ErrNotFound) || (err == nil && !l.Active(now)) {
		return nil, nil, db.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	sys, err := m.GetSystem(ctx, l.SystemID)
	if err != nil {
		return nil, nil, err
	}
	return l, sys, nil
}

// APIKeyPrefix starts every API key.
const APIKeyPrefix = "s0k_"

// AuthenticateAPIKey returns the principal of an API key: its system with
// the key's scopes. Unknown and revoked keys are db.ErrNotFound.
func (m *Module) AuthenticateAPIKey(ctx context.Context, key string) (*auth.Principal, error) {
	if !strings.HasPrefix(key, APIKeyPrefix) {
		return nil, db.ErrNotFound
	}
	k, err := m.GetAPIKeyByHash(ctx, auth.HashToken(key))
	if err == nil && k.RevokedAt != nil {
		err = db.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := m.TouchAPIKey(ctx, k.ID); err != nil {
		slog.WarnContext(ctx, "touch api key", "error", err)
	}
	scopes := make([]auth.Scope, len(k.Scopes))
	for i, s := range k.Scopes {
		scopes[i] = auth.Scope(s)
	}
	return &auth.Principal{
		APIKeyID: k.ID,
		Systems:  map[uuid.UUID][]auth.Scope{k.SystemID: scopes},
	}, nil
}
