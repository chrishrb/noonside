package systems

import (
	"context"
	"errors"
	"io"

	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/google/uuid"
)

// ErrUploadsDisabled is returned when no media store is configured.
var ErrUploadsDisabled = errors.New("uploads are not enabled")

// SetMedia sets where system pictures are stored (nil: no uploads).
func (m *Module) SetMedia(s media.Store) { m.media = s }

// SetImage validates and stores a new picture for the system and deletes
// the old one. Invalid images return an error wrapping media.ErrInvalid.
func (m *Module) SetImage(ctx context.Context, systemID uuid.UUID, r io.Reader) (*System, error) {
	if m.media == nil {
		return nil, ErrUploadsDisabled
	}
	img, err := media.Read(r)
	if err != nil {
		return nil, err
	}
	key := media.NewKey("systems/"+systemID.String(), img.ContentType)
	if err := m.media.Put(ctx, key, img.ContentType, img.Body); err != nil {
		return nil, err
	}
	old, err := m.SetImageKey(ctx, systemID, &key)
	if err != nil {
		m.deleteObject(ctx, &key)
		return nil, err
	}
	m.deleteObject(ctx, old)
	return m.GetSystem(ctx, systemID)
}

// ClearImage removes the system's picture.
func (m *Module) ClearImage(ctx context.Context, systemID uuid.UUID) (*System, error) {
	old, err := m.SetImageKey(ctx, systemID, nil)
	if err != nil {
		return nil, err
	}
	m.deleteObject(ctx, old)
	return m.GetSystem(ctx, systemID)
}

// deleteObject removes a replaced or orphaned picture; a failure only
// leaves an unreferenced object behind.
func (m *Module) deleteObject(ctx context.Context, key *string) {
	if key == nil || m.media == nil {
		return
	}
	if err := m.media.Delete(ctx, *key); err != nil {
		m.logger.WarnContext(ctx, "delete system picture", "key", *key, "error", err)
	}
}
