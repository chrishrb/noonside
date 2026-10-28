package httpapi

import (
	"context"
	"errors"

	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
)

// Authz checks the caller's scopes on systems. It reads a user's role
// through Roles (implemented by the systems module) on every request, so
// membership changes take effect immediately.
type Authz struct {
	Roles auth.RoleLookup
}

// Authorize checks scope on systemID. Callers that cannot see the system get
// 404 so system IDs are not disclosed; members without the scope get 403.
func (a Authz) Authorize(ctx context.Context, systemID uuid.UUID, scope auth.Scope) (*auth.Principal, error) {
	p := auth.FromContext(ctx)
	if p == nil {
		return nil, Unauthorized("unauthorized")
	}
	if err := a.loadScopes(ctx, p, systemID); err != nil {
		return nil, err
	}
	if !p.IsMember(systemID) {
		return nil, NotFound("system not found")
	}
	if !p.Has(systemID, scope) {
		return nil, Forbidden("missing scope " + string(scope))
	}
	return p, nil
}

// loadScopes resolves a user's scopes on systemID from their membership.
func (a Authz) loadScopes(ctx context.Context, p *auth.Principal, systemID uuid.UUID) error {
	if !p.IsUser() || p.Superadmin {
		return nil
	}
	if _, ok := p.Systems[systemID]; ok {
		return nil
	}
	role, err := a.Roles.MemberRole(ctx, systemID, p.UserID)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if p.Systems == nil {
		p.Systems = map[uuid.UUID][]auth.Scope{}
	}
	p.Systems[systemID] = auth.RoleScopes(role)
	return nil
}

// RoleOf returns the caller's role on a system (owner for superadmins).
func (a Authz) RoleOf(ctx context.Context, p *auth.Principal, systemID uuid.UUID) (auth.Role, error) {
	role, err := a.Roles.MemberRole(ctx, systemID, p.UserID)
	if err == nil {
		return role, nil
	}
	if errors.Is(err, db.ErrNotFound) && p.Superadmin {
		return auth.RoleOwner, nil
	}
	return "", err
}

// CurrentUser returns the authenticated user principal.
func CurrentUser(ctx context.Context) (*auth.Principal, error) {
	p := auth.FromContext(ctx)
	if p == nil || !p.IsUser() {
		return nil, Unauthorized("unauthorized")
	}
	return p, nil
}
