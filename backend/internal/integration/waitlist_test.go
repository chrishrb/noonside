//go:build integration

package integration

import (
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	waitlistConfirmRe = regexp.MustCompile(`/warteliste/bestaetigen/#t=(s0w_[A-Za-z0-9_-]+)`)
	waitlistUnsubRe   = regexp.MustCompile(`/warteliste/abmelden/#t=(s0u_[A-Za-z0-9_-]+)`)
)

// postForm posts a landing page form and returns the redirect target.
func (e *env) postForm(path string, form url.Values) string {
	e.t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.PostForm(e.server.URL+"/api/v1"+path, form)
	require.NoError(e.t, err)
	defer resp.Body.Close()
	require.Equal(e.t, http.StatusSeeOther, resp.StatusCode, "POST %s", path)
	return resp.Header.Get("Location")
}

// waitlistMail returns the newest email to addr with its confirm and
// unsubscribe tokens.
func (e *env) waitlistMail(addr string) (confirm, unsub string) {
	e.t.Helper()
	body := e.outbox.find("To: <" + addr + ">")
	require.NotEmpty(e.t, body, "no email to %s", addr)
	if dec, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body))); err == nil {
		body = string(dec)
	}
	m := waitlistConfirmRe.FindStringSubmatch(body)
	require.NotNil(e.t, m, "no confirm link")
	u := waitlistUnsubRe.FindStringSubmatch(body)
	require.NotNil(e.t, u, "no unsubscribe link")
	assert.Contains(e.t, body, "List-Unsubscribe-Post: List-Unsubscribe=One-Click")
	return m[1], u[1]
}

func (e *env) waitlistEntry(addr string) (confirmed, found bool) {
	e.t.Helper()
	entries, err := e.Waitlist.ListWaitlistEntries(e.t.Context())
	require.NoError(e.t, err)
	for _, x := range entries {
		if x.Email == addr {
			return x.ConfirmedAt != nil, true
		}
	}
	return false, false
}

func TestWaitlistDoubleOptIn(t *testing.T) {
	e := newEnv(t)
	addr := uniq("waitlist")
	form := url.Values{"email": {strings.ToUpper(addr)}, "role": {"operator"}, "plan": {"pro"}, "consent": {"on"}}

	loc := e.postForm("/waitlist", form)
	assert.Equal(t, "http://landing.test/warteliste/pruefen/", loc)
	confirm, unsub := e.waitlistMail(addr)
	confirmed, found := e.waitlistEntry(addr)
	require.True(t, found)
	assert.False(t, confirmed)

	// A second sign-up within the hour sends no second email.
	n := len(e.outbox.msgs)
	assert.Equal(t, "http://landing.test/warteliste/pruefen/", e.postForm("/waitlist", form))
	assert.Len(t, e.outbox.msgs, n)

	assert.Equal(t, "http://landing.test/warteliste/ungueltig/", e.postForm("/waitlist/confirm", url.Values{"token": {"s0w_nope"}}))
	assert.Equal(t, "http://landing.test/warteliste/bestaetigt/", e.postForm("/waitlist/confirm", url.Values{"token": {confirm}}))
	assert.Equal(t, "http://landing.test/warteliste/bestaetigt/", e.postForm("/waitlist/confirm", url.Values{"token": {confirm}}))
	confirmed, _ = e.waitlistEntry(addr)
	assert.True(t, confirmed)

	// Confirmed addresses get no new email.
	e.postForm("/waitlist", form)
	assert.Len(t, e.outbox.msgs, n)

	assert.Equal(t, "http://landing.test/warteliste/abgemeldet/", e.postForm("/waitlist/unsubscribe", url.Values{"token": {unsub}}))
	_, found = e.waitlistEntry(addr)
	assert.False(t, found)
	// Unsubscribing twice answers the same.
	assert.Equal(t, "http://landing.test/warteliste/abgemeldet/", e.postForm("/waitlist/unsubscribe", url.Values{"token": {unsub}}))
}

func TestWaitlistOneClickUnsubscribe(t *testing.T) {
	e := newEnv(t)
	addr := uniq("waitlist-oneclick")
	e.postForm("/waitlist", url.Values{"email": {addr}, "consent": {"on"}})
	_, unsub := e.waitlistMail(addr)

	// RFC 8058: the mail client posts List-Unsubscribe=One-Click to the URL
	// from the header, with the token in the query.
	loc := e.postForm("/waitlist/unsubscribe?token="+url.QueryEscape(unsub), url.Values{"List-Unsubscribe": {"One-Click"}})
	assert.Equal(t, "http://landing.test/warteliste/abgemeldet/", loc)
	_, found := e.waitlistEntry(addr)
	assert.False(t, found)
}

func TestWaitlistRejects(t *testing.T) {
	e := newEnv(t)
	n := len(e.outbox.msgs)

	// Honeypot: looks like success, stores and sends nothing.
	bot := uniq("waitlist-bot")
	assert.Equal(t, "http://landing.test/warteliste/pruefen/",
		e.postForm("/waitlist", url.Values{"email": {bot}, "consent": {"on"}, "website": {"http://spam"}}))
	_, found := e.waitlistEntry(bot)
	assert.False(t, found)

	for _, form := range []url.Values{
		{"email": {uniq("waitlist-noconsent")}},
		{"email": {"not-an-email"}, "consent": {"on"}},
		{"email": {"Name <" + uniq("x") + ">"}, "consent": {"on"}},
	} {
		assert.Equal(t, "http://landing.test/warteliste/fehler/", e.postForm("/waitlist", form))
	}
	assert.Len(t, e.outbox.msgs, n)
}

func TestPlanInterest(t *testing.T) {
	e := newEnv(t)
	count := func(plan string) int {
		clicks, err := e.Waitlist.ListPricingClicks(t.Context())
		require.NoError(t, err)
		today := time.Now().UTC().Format(time.DateOnly)
		for _, c := range clicks {
			if c.Plan == plan && c.Day.Format(time.DateOnly) == today {
				return c.Count
			}
		}
		return 0
	}
	before := count("pro")
	assert.Equal(t, "http://landing.test/bald/pro/", e.postForm("/waitlist/interest", url.Values{"plan": {"pro"}}))
	assert.Equal(t, before+1, count("pro"))

	// Unknown plans go back to the prices and aren't counted, so the
	// redirect can never leave the landing site.
	assert.Equal(t, "http://landing.test/#preise", e.postForm("/waitlist/interest", url.Values{"plan": {"//evil.example"}}))
}
