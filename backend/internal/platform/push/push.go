// Package push sends Web Push notifications signed with VAPID.
package push

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"

	"github.com/SherClockHolmes/webpush-go"
)

// ttl is how long push services keep a message for an offline device.
const ttl = 24 * 60 * 60

// Client sends Web Push messages. The zero value (no keys) is disabled.
type Client struct {
	publicKey, privateKey string
	subject               string
	http                  *http.Client
}

// New returns a client; without a private key push is disabled. subject is
// a contact email (or https URL) for the push services.
func New(publicKey, privateKey, subject string) (*Client, error) {
	if privateKey == "" {
		return &Client{}, nil
	}
	if publicKey == "" {
		return nil, fmt.Errorf("vapid_public_key is required with VAPID_PRIVATE_KEY")
	}
	if subject == "" {
		return nil, fmt.Errorf("vapid_subject is required with VAPID_PRIVATE_KEY")
	}
	return &Client{
		publicKey: publicKey, privateKey: privateKey,
		subject: strings.TrimPrefix(subject, "mailto:"),
		http:    errtrack.HTTPClient(15 * time.Second),
	}, nil
}

func (c *Client) Enabled() bool { return c != nil && c.privateKey != "" }

// PublicKey is handed to browsers to subscribe.
func (c *Client) PublicKey() string { return c.publicKey }

// Subscription is where a browser receives pushes.
type Subscription struct {
	Endpoint, P256dh, Auth string
}

// Send delivers payload to sub. gone reports that the subscription no longer
// exists (404/410) and should be deleted.
func (c *Client) Send(ctx context.Context, sub Subscription, payload []byte, urgent bool) (bool, error) {
	if !AllowedEndpoint(sub.Endpoint) {
		return true, fmt.Errorf("push endpoint host not allowed")
	}
	urgency := webpush.UrgencyNormal
	if urgent {
		urgency = webpush.UrgencyHigh
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		HTTPClient:      c.http,
		Subscriber:      c.subject,
		VAPIDPublicKey:  c.publicKey,
		VAPIDPrivateKey: c.privateKey,
		TTL:             ttl,
		Urgency:         urgency,
	})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close() //nolint:errcheck
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return true, fmt.Errorf("push subscription gone (%d)", resp.StatusCode)
	case resp.StatusCode >= 300:
		return false, fmt.Errorf("push service returned %d", resp.StatusCode)
	}
	return false, nil
}

// allowedHosts are the push services of current browsers. The server only
// posts to these, so a client can't make it send requests elsewhere.
var allowedHosts = []string{
	"fcm.googleapis.com",        // Chrome, Edge, Android
	"push.services.mozilla.com", // Firefox (updates.push.services.mozilla.com)
	"push.apple.com",            // Safari, iOS home screen apps (web.push.apple.com)
	"notify.windows.com",        // Edge on Windows (*.notify.windows.com)
}

// AllowedEndpoint reports whether endpoint is an https URL of a known push
// service.
func AllowedEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range allowedHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// GenerateKeys returns a new VAPID key pair (base64url).
func GenerateKeys() (publicKey, privateKey string, err error) {
	privateKey, publicKey, err = webpush.GenerateVAPIDKeys()
	return publicKey, privateKey, err
}
