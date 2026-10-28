package auth

import (
	"context"
	"slices"

	"github.com/google/uuid"
)

// Principal is the authenticated caller of a request.
type Principal struct {
	// UserID is set for users; APIKeyID for device keys.
	UserID     uuid.UUID
	APIKeyID   uuid.UUID
	Email      string
	Superadmin bool
	// Systems maps system IDs to the scopes granted on them. For users it is
	// filled lazily from the database (see httpapi.Authz); API keys
	// carry their single system here.
	Systems map[uuid.UUID][]Scope
}

func (p *Principal) IsUser() bool { return p.UserID != uuid.Nil }

// Has reports whether the principal holds scope on systemID.
func (p *Principal) Has(systemID uuid.UUID, scope Scope) bool {
	if p == nil {
		return false
	}
	if p.Superadmin {
		return true
	}
	return slices.Contains(p.Systems[systemID], scope)
}

// IsMember reports whether the principal can see systemID at all.
func (p *Principal) IsMember(systemID uuid.UUID) bool {
	if p == nil {
		return false
	}
	_, ok := p.Systems[systemID]
	return ok || p.Superadmin
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}
