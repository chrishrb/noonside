package identity

import (
	"context"
	"errors"
	"io"

	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/google/uuid"
)

// ErrUploadsDisabled is returned when no media store is configured.
var ErrUploadsDisabled = errors.New("uploads are not enabled")

// UploadsEnabled reports whether pictures can be uploaded.
func (m *Module) UploadsEnabled() bool { return m.media != nil }

// SetAvatar validates and stores a new profile picture and deletes the old
// one. Invalid images return an error wrapping media.ErrInvalid.
func (m *Module) SetAvatar(ctx context.Context, userID uuid.UUID, r io.Reader) error {
	if m.media == nil {
		return ErrUploadsDisabled
	}
	img, err := media.Read(r)
	if err != nil {
		return err
	}
	if _, err := m.GetProfile(ctx, userID); err != nil {
		return err
	}
	key := media.NewKey("avatars/"+userID.String(), img.ContentType)
	if err := m.media.Put(ctx, key, img.ContentType, img.Body); err != nil {
		return err
	}
	old, err := m.SetAvatarKey(ctx, userID, &key)
	if err != nil {
		m.deleteObject(ctx, &key)
		return err
	}
	m.deleteObject(ctx, old)
	return nil
}

// ClearAvatar removes the profile picture.
func (m *Module) ClearAvatar(ctx context.Context, userID uuid.UUID) error {
	old, err := m.SetAvatarKey(ctx, userID, nil)
	if err != nil {
		return err
	}
	m.deleteObject(ctx, old)
	return nil
}

// deleteObject removes a replaced picture; a failure only leaves an
// unreferenced object behind.
func (m *Module) deleteObject(ctx context.Context, key *string) {
	if key == nil || m.media == nil {
		return
	}
	if err := m.media.Delete(ctx, *key); err != nil {
		m.logger.WarnContext(ctx, "delete profile picture", "key", *key, "error", err)
	}
}
