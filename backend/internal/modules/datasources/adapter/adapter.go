// Package adapter contains pollers that pull data from external sources.
package adapter

import "context"

// Poller fetches new data from one source and ingests it. The scheduler
// calls Poll on the source's interval; implementations must be idempotent.
type Poller interface {
	Poll(ctx context.Context) error
}
