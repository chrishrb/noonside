package auth

import (
	"context"

	"github.com/google/uuid"
)

// Role is a user's role on a single system.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleViewer:
		return true
	}
	return false
}

// RoleLookup returns a user's role on a system, or db.ErrNotFound if they
// are no member (implemented by the systems module).
type RoleLookup interface {
	MemberRole(ctx context.Context, systemID, userID uuid.UUID) (Role, error)
}
