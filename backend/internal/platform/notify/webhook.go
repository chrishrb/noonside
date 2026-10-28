package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

// SignatureHeader carries the HMAC of the body when the target has a secret.
const SignatureHeader = "X-Noonside-Signature"

// WebhookPayload is the JSON body of a webhook.
type WebhookPayload struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Severity  string    `json:"severity"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	URL       string    `json:"url"`
	SystemID  string    `json:"system_id,omitempty"`
	AlertID   string    `json:"alert_id,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Sign returns the signature header value for body: "sha256=" and the hex
// HMAC-SHA256 of the raw body with the secret.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Webhook posts m as JSON to url, signed with secret if it is set. Any 2xx
// response counts as delivered.
func (c *Client) Webhook(ctx context.Context, url, secret string, m Message) error {
	body, err := json.Marshal(WebhookPayload{
		ID: m.ID, Type: m.Type, Severity: m.Severity, Title: m.Title, Body: m.Body, URL: m.URL,
		SystemID: m.SystemID, AlertID: m.AlertID, Kind: m.Kind, CreatedAt: m.CreatedAt.UTC(),
	})
	if err != nil {
		return err
	}
	return c.PostJSON(ctx, url, secret, nil, body)
}

// PostJSON posts body as JSON to url with the extra headers, signed with
// secret if it is set. Any 2xx response counts as delivered.
func (c *Client) PostJSON(ctx context.Context, url, secret string, header http.Header, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set(SignatureHeader, Sign(secret, body))
	}
	return c.do(ctx, req)
}
