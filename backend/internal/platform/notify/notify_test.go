package notify

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captured struct {
	header http.Header
	body   []byte
}

func server(t *testing.T, status int) (*httptest.Server, *captured) {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.header = r.Header.Clone()
		c.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte("nope"))
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

var msg = Message{
	ID: "n1", Type: "alert_opened", Severity: "error", Title: "WR 1 meldet einen Fehler", Body: "Fehler 42 seit 10:00.",
	URL: "https://solar.example.com/s/x/status", SystemID: "s1", AlertID: "a1", Kind: "inverter_error",
	CreatedAt: time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
}

func TestNtfy(t *testing.T) {
	srv, got := server(t, 200)
	c := New(true, "noonside-test")
	require.NoError(t, c.Ntfy(context.Background(), srv.URL+"/topic", "tk_secret", msg))

	title, err := new(mime.WordDecoder).DecodeHeader(got.header.Get("Title"))
	require.NoError(t, err)
	assert.Equal(t, msg.Title, title)
	assert.Equal(t, "5", got.header.Get("Priority"))
	assert.Equal(t, "noonside,rotating_light", got.header.Get("Tags"))
	assert.Equal(t, msg.URL, got.header.Get("Click"))
	assert.Equal(t, "Bearer tk_secret", got.header.Get("Authorization"))
	assert.Equal(t, "noonside-test", got.header.Get("User-Agent"))
	assert.Equal(t, msg.Body, string(got.body))

	require.NoError(t, c.Ntfy(context.Background(), srv.URL+"/topic", "", msg))
	assert.Empty(t, got.header.Get("Authorization"))
}

func TestNtfyPriority(t *testing.T) {
	for _, tc := range []struct {
		typ, sev string
		want     int
	}{
		{"alert_opened", "error", 5}, {"alert_reminder", "warning", 4}, {"alert_opened", "info", 3},
		{"alert_resolved", "error", 3}, {"forecast", "info", 3}, {"test", "", 3},
	} {
		assert.Equal(t, tc.want, NtfyPriority(Message{Type: tc.typ, Severity: tc.sev}), "%s/%s", tc.typ, tc.sev)
	}
}

func TestWebhook(t *testing.T) {
	srv, got := server(t, 202)
	c := New(true, "")
	require.NoError(t, c.Webhook(context.Background(), srv.URL, "s3cret", msg))

	assert.Equal(t, "application/json", got.header.Get("Content-Type"))
	assert.Equal(t, Sign("s3cret", got.body), got.header.Get(SignatureHeader))
	// known vector, so receivers can check their implementation
	assert.Equal(t, "sha256=f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8", Sign("key", []byte("The quick brown fox jumps over the lazy dog")))
	var p WebhookPayload
	require.NoError(t, json.Unmarshal(got.body, &p))
	assert.Equal(t, WebhookPayload{
		ID: "n1", Type: "alert_opened", Severity: "error", Title: msg.Title, Body: msg.Body, URL: msg.URL,
		SystemID: "s1", AlertID: "a1", Kind: "inverter_error", CreatedAt: msg.CreatedAt,
	}, p)

	require.NoError(t, c.Webhook(context.Background(), srv.URL, "", msg))
	assert.Empty(t, got.header.Get(SignatureHeader), "unsigned without a secret")
}

func TestErrors(t *testing.T) {
	srv, _ := server(t, 500)
	err := New(true, "").Webhook(context.Background(), srv.URL, "", msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "nope")

	// local addresses and plain http are refused unless allowed
	ok, _ := server(t, 200)
	err = New(false, "").Webhook(context.Background(), ok.URL, "", msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
	err = New(false, "").Ntfy(context.Background(), "https://127.0.0.1:1/topic?auth=tk_leak", "", msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
	assert.NotContains(t, err.Error(), "tk_leak", "the URL is not in the error")
}
