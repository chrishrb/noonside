package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/supabase"
	"github.com/google/uuid"
)

// ErrSoleOwner is returned by DeleteUser while the user is the only owner
// of systems; SoleOwnerError lists them.
var ErrSoleOwner = errors.New("user is the only owner of systems")

// SoleOwnerError names the systems that would be left without an owner.
type SoleOwnerError struct{ Systems []AccountSystem }

func (e *SoleOwnerError) Error() string { return ErrSoleOwner.Error() }
func (e *SoleOwnerError) Unwrap() error { return ErrSoleOwner }

// disabledBan is how long a disabled account is banned (100 years).
const disabledBan = "876000h"

// UserChanges are a superadmin's changes to an account; nil fields stay.
type UserChanges struct {
	Name       *string
	Email      *string
	Superadmin *bool
	Disabled   *bool
}

// UpdateUser applies the changes. Errors are those of the admin API
// (supabase.ErrUserExists for a taken email) or db.ErrNotFound.
func (m *Module) UpdateUser(ctx context.Context, id uuid.UUID, c UserChanges) error {
	prof, err := m.GetProfile(ctx, id)
	if err != nil {
		return err
	}
	name := prof.Name
	if c.Name != nil {
		name = strings.TrimSpace(*c.Name)
	}
	var upd supabase.UserUpdate
	if c.Email != nil {
		if email := NormalizeEmail(*c.Email); email != prof.Email {
			// Supabase answers a taken address with a 500 (unique violation).
			switch other, err := m.GetProfileByEmail(ctx, email); {
			case err == nil && other.ID != id:
				return supabase.ErrUserExists
			case err != nil && !errors.Is(err, db.ErrNotFound):
				return err
			}
			upd.Email = &email
		}
	}
	if upd.Email != nil || name != prof.Name {
		// The profile trigger copies user_metadata.name into profiles.name
		// whenever the email or the metadata changes, so the metadata has to
		// carry the current name.
		upd.UserMetadata = map[string]any{"name": name}
	}
	if c.Disabled != nil {
		ban := "none"
		if *c.Disabled {
			ban = disabledBan
		}
		upd.BanDuration = &ban
	}
	if upd.Email != nil || upd.UserMetadata != nil || upd.BanDuration != nil {
		if _, err := m.admin.UpdateUser(ctx, id, upd); err != nil {
			return err
		}
	}
	if name != prof.Name {
		// Also set directly: the trigger keeps a non-empty name only.
		if _, err := m.UpdateProfileName(ctx, id, name); err != nil {
			return err
		}
	}
	if c.Superadmin != nil {
		return m.SetSuperadmin(ctx, id, *c.Superadmin)
	}
	return nil
}

// RenameUser changes the name in the profile and in user_metadata, which
// the profile trigger reads on later email changes.
func (m *Module) RenameUser(ctx context.Context, id uuid.UUID, name string) (*Profile, error) {
	name = strings.TrimSpace(name)
	if _, err := m.admin.UpdateUser(ctx, id, supabase.UserUpdate{UserMetadata: map[string]any{"name": name}}); err != nil {
		return nil, err
	}
	return m.UpdateProfileName(ctx, id, name)
}

// DeleteUser deletes the account and its profile picture. It refuses with a
// *SoleOwnerError while the user is the only owner of a system.
func (m *Module) DeleteUser(ctx context.Context, id uuid.UUID) error {
	prof, err := m.GetProfile(ctx, id)
	if err != nil {
		return err
	}
	if m.memberships != nil {
		owned, err := m.memberships.SoleOwnerships(ctx, id)
		if err != nil {
			return err
		}
		if len(owned) > 0 {
			return &SoleOwnerError{Systems: owned}
		}
	}
	if err := m.admin.DeleteUser(ctx, id); err != nil {
		return err
	}
	m.deleteObject(ctx, prof.AvatarKey)
	return nil
}

// SendPasswordReset emails the user a link to choose a new password at
// redirectTo.
func (m *Module) SendPasswordReset(ctx context.Context, id uuid.UUID, redirectTo string) error {
	prof, err := m.GetProfile(ctx, id)
	if err != nil {
		return err
	}
	return m.admin.SendRecovery(ctx, prof.Email, redirectTo)
}
