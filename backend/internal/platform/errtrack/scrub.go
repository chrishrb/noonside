package errtrack

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/getsentry/sentry-go"
)

// secretHeaders carry credentials and are never sent.
var secretHeaders = []string{"Authorization", "X-Api-Key", "Cookie"}

// tokenPath matches path segments that are bearer tokens: share links
// (/public/{token}, SPA /p/{token}) and invitations.
var tokenPath = regexp.MustCompile(`(/(?:public|p|invitations)/)[^/?#]+`)

// ScrubPath replaces tokens in a URL path with "[token]".
func ScrubPath(p string) string { return tokenPath.ReplaceAllString(p, "${1}[token]") }

// scrub removes credentials, tokens and bodies from an event.
func scrub(e *sentry.Event) *sentry.Event {
	if e == nil {
		return nil
	}
	e.Transaction = ScrubPath(e.Transaction)
	if r := e.Request; r != nil {
		r.URL, _, _ = strings.Cut(ScrubPath(r.URL), "?")
		r.QueryString = ""
		r.Cookies = ""
		r.Data = ""
		for k := range r.Headers {
			for _, h := range secretHeaders {
				if http.CanonicalHeaderKey(k) == h {
					delete(r.Headers, k)
				}
			}
		}
	}
	return e
}
