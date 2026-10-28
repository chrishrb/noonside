// Package notify posts notifications to a user's own targets: an ntfy topic
// and a webhook. The URLs come from users, so requests go through netguard.
package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/netguard"
)

// Timeout bounds each request.
const Timeout = 10 * time.Second

// Message is a rendered notification.
type Message struct {
	ID       string
	Type     string // notifications.Notification* type
	Severity string // info, warning, error
	Title    string
	Body     string
	// URL opens the notification in the app (absolute).
	URL       string
	SystemID  string
	AlertID   string
	Kind      string // alert rule kind
	CreatedAt time.Time
}

// Client sends to ntfy and webhooks.
type Client struct {
	http      *http.Client
	userAgent string
	// AllowPrivate permits private and local addresses (self-hosted setups).
	AllowPrivate bool
}

// New returns a client; allowPrivate permits private and local addresses
// and plain http (allow_private_data_source_hosts).
func New(allowPrivate bool, userAgent string) *Client {
	return &Client{http: netguard.HTTPClient(Timeout, allowPrivate), userAgent: userAgent, AllowPrivate: allowPrivate}
}

// CheckURL validates a target URL as the client will use it.
func (c *Client) CheckURL(raw string) error { return netguard.CheckURL(raw, c.AllowPrivate) }

func (c *Client) do(ctx context.Context, req *http.Request) error {
	if err := c.CheckURL(req.URL.String()); err != nil {
		return err
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	res, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		// Without the URL: webhook URLs often carry a token, and the error
		// ends up in logs and the delivery row.
		if ue, ok := err.(*url.Error); ok {
			return fmt.Errorf("%s request failed: %w", ue.Op, ue.Err)
		}
		return err
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode < 200 || res.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		msg := strings.TrimSpace(string(b))
		// An HTML error page says nothing the status doesn't.
		if strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") || strings.HasPrefix(strings.ToLower(msg), "<") {
			msg = ""
		}
		if msg != "" {
			return fmt.Errorf("%s: %s", res.Status, msg)
		}
		return fmt.Errorf("%s", res.Status)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return nil
}
