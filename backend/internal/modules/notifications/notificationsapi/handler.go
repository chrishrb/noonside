// Package notificationsapi serves the notifications module's API
// operations: the inbox, preferences, push subscriptions and the user's own
// ntfy and webhook targets.
package notificationsapi

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/chrishrb/noonside/internal/platform/secretbox"
	"github.com/google/uuid"
)

// Mailer reports whether emails are delivered (implemented by email.Mailer).
type Mailer interface {
	Send(ctx context.Context, to string, e email.Email) error
	Enabled() bool
}

// TargetNotifier sends to ntfy and webhooks (implemented by notify.Client).
type TargetNotifier interface {
	notifications.TargetSender
	// CheckURL validates a target URL (https and, unless allowed, public).
	CheckURL(raw string) error
}

// Systems lists the caller's systems (implemented by the systems module).
type Systems interface {
	ListSystems(ctx context.Context, userID *uuid.UUID) ([]systems.SystemWithRole, error)
	ListMembershipsForUser(ctx context.Context, userID uuid.UUID) ([]systems.Membership, error)
}

type Config struct {
	// PublicURL is the app's base URL for links in test messages.
	PublicURL string
	// Pusher sends Web Push (nil: disabled); VAPIDPublicKey is handed to
	// browsers to subscribe.
	Pusher         notifications.Pusher
	VAPIDPublicKey string
	// Notify sends to users' ntfy topics and webhooks (nil: disabled).
	Notify TargetNotifier
	// Box seals the targets' token and secret (nil: they cannot be set).
	Box *secretbox.Box
}

type Handler struct {
	notifications *notifications.Module
	identity      *identity.Module
	systems       Systems
	mailer        Mailer
	cfg           Config
	now           func() time.Time
}

func New(n *notifications.Module, id *identity.Module, sys Systems, mailer Mailer, cfg Config) *Handler {
	return &Handler{notifications: n, identity: id, systems: sys, mailer: mailer, cfg: cfg, now: time.Now}
}

// locale returns the caller's language for server-rendered text.
func (h *Handler) locale(ctx context.Context, p *auth.Principal) string {
	return h.identity.Locale(ctx, p)
}
