package hostedftp

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GCStore is what the clean-up job needs from the store.
type GCStore interface {
	TryLockJob(ctx context.Context, name string, ttl, every time.Duration) (unlock func(), ok bool, err error)
	// DataSourceIDsByKind returns the IDs of all data sources of the kind.
	DataSourceIDsByKind(ctx context.Context, kind string) ([]uuid.UUID, error)
}

const gcEvery = time.Hour

// GC returns the hourly job that deletes upload accounts without a data
// source (the system was deleted, or deleting the account failed). It also
// re-applies the upload hook, in case setting it up at start failed or the
// rule was changed on the server.
func (s *Service) GC(st GCStore) func(ctx context.Context, deadline time.Time) error {
	return func(ctx context.Context, deadline time.Time) error {
		unlock, ok, err := st.TryLockJob(ctx, "hosted_ftp_gc", 5*time.Minute, gcEvery-time.Minute)
		if err != nil || !ok {
			return err
		}
		defer unlock()

		if err := s.EnsureHook(ctx); err != nil {
			s.logger.Warn("set up the hosted FTP upload hook", "error", err)
		}

		ids, err := st.DataSourceIDsByKind(ctx, KindHosted)
		if err != nil {
			return err
		}
		known := make(map[string]bool, len(ids))
		for _, id := range ids {
			known[id.String()] = true
		}
		users, err := s.api.ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, u := range users {
			id, managed := strings.CutPrefix(u.Description, descPrefix)
			if !managed || known[id] {
				continue
			}
			// The API inserts the data source before creating its account,
			// so an account being created is never an orphan here.
			if _, err := uuid.Parse(id); err != nil {
				continue
			}
			if time.Now().After(deadline) {
				break
			}
			if err := s.Remove(ctx, Account{Username: u.Username}); err != nil {
				s.logger.Error("delete orphaned upload account", "username", u.Username, "data_source_id", id, "error", err)
			}
		}
		if limit := s.settings.MaxAccounts; limit > 0 && len(ids) >= limit-5 {
			s.logger.Warn("hosted FTP accounts are running out", "accounts", len(ids), "limit", limit)
		}
		return nil
	}
}
