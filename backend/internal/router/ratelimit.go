package router

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitConfig configures the per-IP limits in front of the API.
type RateLimitConfig struct {
	// Disabled turns the limits off (dev, tests).
	Disabled bool
	// TrustedProxyHops is the number of proxies in front of the server
	// that append to X-Forwarded-For (1 behind the gateway). The client is
	// the address that many entries from the right; 0 uses the peer address.
	TrustedProxyHops int
}

// Limits per IP, as burst plus refill over 5 minutes (they replace the WAF
// rules the API had on AWS).
var rateLimitRules = []rateLimitRule{
	// Server-to-server hooks (hosted FTP uploads, all from the one SFTPGo
	// address) are not limited: they carry a token, and a limit would drop
	// imports once many devices upload.
	{name: "hooks", match: func(p string) bool { return strings.HasPrefix(p, apiPrefix+"/hooks/") }},
	// Public endpoints without an account. A kiosk screen needs about 15.
	{name: "public", per5Min: 60, match: func(p string) bool {
		return strings.HasPrefix(p, apiPrefix+"/invitations/") ||
			strings.HasPrefix(p, apiPrefix+"/public/") ||
			strings.HasPrefix(p, apiPrefix+"/waitlist")
	}},
	{name: "api", per5Min: 600, match: func(p string) bool { return strings.HasPrefix(p, "/api/") }},
}

type rateLimitRule struct {
	name    string
	per5Min int
	match   func(path string) bool
}

const (
	// maxLimiterEntries bounds the memory a flood of addresses can take;
	// when full, idle entries are dropped first, then everything.
	maxLimiterEntries = 100_000
	limiterIdle       = 10 * time.Minute
)

// WithRateLimit answers 429 when an IP exceeds a rule in rateLimitRules.
func WithRateLimit(next http.Handler, cfg RateLimitConfig) http.Handler {
	if cfg.Disabled {
		return next
	}
	l := &limiters{entries: map[string]*limiterEntry{}, now: time.Now}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rule := range rateLimitRules {
			if !rule.match(r.URL.Path) {
				continue
			}
			if rule.per5Min > 0 && !l.allow(rule, clientIP(r, cfg.TrustedProxyHops)) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"message":"too many requests"}`))
				return
			}
			break // the first matching rule applies
		}
		next.ServeHTTP(w, r)
	})
}

type limiterEntry struct {
	lim  *rate.Limiter
	seen time.Time
}

type limiters struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	now     func() time.Time
}

func (l *limiters) allow(rule rateLimitRule, ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	key := rule.name + "|" + ip
	e, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= maxLimiterEntries {
			l.evict(now)
		}
		e = &limiterEntry{lim: rate.NewLimiter(rate.Limit(float64(rule.per5Min)/300), rule.per5Min)}
		l.entries[key] = e
	}
	e.seen = now
	return e.lim.AllowN(now, 1)
}

// evict drops idle entries (their buckets are full again anyway), or all
// of them if none is idle.
func (l *limiters) evict(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.seen) > limiterIdle {
			delete(l.entries, k)
		}
	}
	if len(l.entries) >= maxLimiterEntries {
		clear(l.entries)
	}
}

// clientIP is the address hops entries from the right of X-Forwarded-For
// (each trusted proxy appends the address it received the request from),
// else the peer address.
func clientIP(r *http.Request, hops int) string {
	if hops > 0 {
		var addrs []string
		for _, h := range r.Header.Values("X-Forwarded-For") {
			for _, a := range strings.Split(h, ",") {
				if a = strings.TrimSpace(a); a != "" {
					addrs = append(addrs, a)
				}
			}
		}
		if i := len(addrs) - hops; i >= 0 && i < len(addrs) {
			return addrs[i]
		}
		if len(addrs) > 0 {
			return addrs[0]
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
