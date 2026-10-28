package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientIP(t *testing.T) {
	req := func(xff ...string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		r.RemoteAddr = "10.0.0.5:4321"
		for _, h := range xff {
			r.Header.Add("X-Forwarded-For", h)
		}
		return r
	}
	assert.Equal(t, "10.0.0.5", clientIP(req("1.2.3.4"), 0), "no trusted proxy: peer")
	assert.Equal(t, "1.2.3.4", clientIP(req("1.2.3.4"), 1))
	// A client can prepend anything; only the entry the gateway added counts.
	assert.Equal(t, "1.2.3.4", clientIP(req("6.6.6.6, 1.2.3.4"), 1))
	assert.Equal(t, "1.2.3.4", clientIP(req("6.6.6.6", "1.2.3.4"), 1))
	assert.Equal(t, "6.6.6.6", clientIP(req("6.6.6.6, 1.2.3.4"), 2))
	assert.Equal(t, "1.2.3.4", clientIP(req("1.2.3.4"), 3), "fewer entries than hops: leftmost")
	assert.Equal(t, "10.0.0.5", clientIP(req(), 1), "no header: peer")
}

func TestRateLimit(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := WithRateLimit(ok, RateLimitConfig{TrustedProxyHops: 1})
	do := func(path, ip string) int {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	for range 60 {
		assert.Equal(t, http.StatusNoContent, do("/api/v1/public/tok/live", "1.1.1.1"))
	}
	assert.Equal(t, http.StatusTooManyRequests, do("/api/v1/public/tok/live", "1.1.1.1"))
	assert.Equal(t, http.StatusNoContent, do("/api/v1/public/tok/live", "2.2.2.2"), "per IP")
	assert.Equal(t, http.StatusNoContent, do("/api/v1/me", "1.1.1.1"), "separate bucket for the rest of the API")
	assert.Equal(t, http.StatusNoContent, do("/assets/app.js", "1.1.1.1"), "the SPA is not limited")
	for range 700 {
		require.Equal(t, http.StatusNoContent, do("/api/v1/hooks/hosted-ftp", "3.3.3.3"), "hooks are not limited")
	}

	off := WithRateLimit(ok, RateLimitConfig{Disabled: true})
	for range 100 {
		w := httptest.NewRecorder()
		off.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/x", nil))
		assert.Equal(t, http.StatusNoContent, w.Code)
	}
}

func TestLimiterEviction(t *testing.T) {
	now := time.Unix(0, 0)
	l := &limiters{entries: map[string]*limiterEntry{}, now: func() time.Time { return now }}
	rule := rateLimitRules[0]
	l.allow(rule, "a")
	now = now.Add(limiterIdle + time.Second)
	l.allow(rule, "b")
	l.evict(now)
	assert.Len(t, l.entries, 1)
	assert.Contains(t, l.entries, rule.name+"|b")
}
