// Package waitlistapi serves the waitlist module's API operations, the
// landing page's form posts.
package waitlistapi

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/waitlist"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
)

// Mailer sends the confirmation emails (implemented by email.Mailer).
type Mailer interface {
	Send(ctx context.Context, to string, e email.Email) error
	Enabled() bool
}

type Config struct {
	// PublicURL is the app's base URL.
	PublicURL string
	// LandingURL is the landing site the waitlist forms come from and
	// redirect back to ("": the waitlist endpoints are not found).
	LandingURL string
	// WaitlistMailsPerHour caps waitlist confirmation emails across all
	// addresses (0: no cap).
	WaitlistMailsPerHour int
}

type Handler struct {
	waitlist *waitlist.Module
	mailer   Mailer
	cfg      Config
	logger   *slog.Logger
	now      func() time.Time
}

func New(w *waitlist.Module, mailer Mailer, cfg Config) *Handler {
	return &Handler{waitlist: w, mailer: mailer, cfg: cfg, logger: slog.Default().With("namespace", "api.handler"), now: time.Now}
}

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Waitlist of the landing page (landing/). Its forms post
// here without JavaScript, so every answer is a redirect to a page of the
// landing site, also for invalid input.

const (
	// waitlistTokenTTL is how long the double opt-in link works.
	waitlistTokenTTL = 7 * 24 * time.Hour
	// waitlistResendAfter is the minimum time between two confirmation
	// emails to the same address.
	waitlistResendAfter = time.Hour
	// waitlistConsentVersion names the consent text shown next to the form
	// (landing/src/components/WaitlistForm.astro); bump it when that changes.
	waitlistConsentVersion = "2026-10"
)

var (
	waitlistRoles = []string{"operator", "home_assistant", "installer", "other"}
	waitlistPlans = []string{"free", "pro", "installer"}
)

func (h *Handler) landingRedirect(path string) (*generated.LandingRedirect, error) {
	if h.cfg.LandingURL == "" {
		return nil, httpapi.NotFound("not found")
	}
	return &generated.LandingRedirect{Location: strings.TrimRight(h.cfg.LandingURL, "/") + path}, nil
}

func (h *Handler) JoinWaitlist(ctx context.Context, req *generated.WaitlistForm) (*generated.LandingRedirect, error) {
	done, err := h.landingRedirect("/warteliste/pruefen/")
	if err != nil {
		return nil, err
	}
	if req.Website.Or("") != "" {
		return done, nil
	}
	addr := normalizeEmail(req.Email.Or(""))
	if !validEmail(addr) || req.Consent.Or("") == "" {
		return h.landingRedirect("/warteliste/fehler/")
	}
	role, plan := req.Role.Or(""), req.Plan.Or("")
	if !slices.Contains(waitlistRoles, role) {
		role = ""
	}
	if !slices.Contains(waitlistPlans, plan) {
		plan = ""
	}
	unsub, _, err := auth.NewOpaqueToken("s0u_")
	if err != nil {
		return nil, err
	}
	entry, err := h.waitlist.UpsertWaitlistEntry(ctx, &waitlist.WaitlistEntry{
		Email: addr, Role: role, Plan: plan, ConsentVersion: waitlistConsentVersion, UnsubscribeToken: unsub,
	})
	if err != nil {
		return nil, err
	}
	now := h.now()
	if entry.ConfirmedAt != nil || (entry.ConfirmSentAt != nil && now.Sub(*entry.ConfirmSentAt) < waitlistResendAfter) {
		return done, nil
	}
	if !h.mailer.Enabled() {
		h.logger.Warn("waitlist: email is not configured; no confirmation sent")
		return done, nil
	}
	sent, err := h.waitlist.CountWaitlistMailsSince(ctx, now.Add(-time.Hour))
	if err != nil {
		return nil, err
	}
	if limit := h.cfg.WaitlistMailsPerHour; limit > 0 && sent >= limit {
		h.logger.Warn("waitlist: hourly email limit reached; no confirmation sent", "limit", limit)
		return done, nil
	}
	token, hash, err := auth.NewOpaqueToken("s0w_")
	if err != nil {
		return nil, err
	}
	if err := h.waitlist.SetWaitlistConfirmToken(ctx, entry.ID, hash, now); err != nil {
		return nil, err
	}
	landing := strings.TrimRight(h.cfg.LandingURL, "/")
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	err = h.mailer.Send(sendCtx, addr, email.WaitlistConfirm{
		ConfirmURL:     landing + "/warteliste/bestaetigen/#t=" + token,
		UnsubscribeURL: landing + "/warteliste/abmelden/#t=" + entry.UnsubscribeToken,
		OneClickURL:    strings.TrimRight(h.cfg.PublicURL, "/") + "/api/v1/waitlist/unsubscribe?token=" + url.QueryEscape(entry.UnsubscribeToken),
		ExpiresAt:      now.Add(waitlistTokenTTL),
	})
	if err != nil {
		// The address can sign up again after waitlistResendAfter.
		h.logger.Error("waitlist: sending the confirmation failed", "err", err)
	}
	return done, nil
}

func (h *Handler) ConfirmWaitlist(ctx context.Context, req *generated.WaitlistTokenForm) (*generated.LandingRedirect, error) {
	if _, err := h.landingRedirect("/"); err != nil {
		return nil, err
	}
	token := strings.TrimSpace(req.Token.Or(""))
	if token == "" {
		return h.landingRedirect("/warteliste/ungueltig/")
	}
	err := h.waitlist.ConfirmWaitlistEntry(ctx, auth.HashToken(token), h.now().Add(-waitlistTokenTTL))
	if errors.Is(err, db.ErrNotFound) {
		return h.landingRedirect("/warteliste/ungueltig/")
	}
	if err != nil {
		return nil, err
	}
	return h.landingRedirect("/warteliste/bestaetigt/")
}

// UnsubscribeWaitlist answers the same for unknown tokens: the entry may
// already be gone (unsubscribed twice, or pruned).
func (h *Handler) UnsubscribeWaitlist(ctx context.Context, req generated.OptWaitlistTokenForm, params generated.UnsubscribeWaitlistParams) (*generated.LandingRedirect, error) {
	done, err := h.landingRedirect("/warteliste/abgemeldet/")
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(req.Or(generated.WaitlistTokenForm{}).Token.Or(""))
	if token == "" {
		token = strings.TrimSpace(params.Token.Or(""))
	}
	if token == "" {
		return h.landingRedirect("/warteliste/ungueltig/")
	}
	if err := h.waitlist.DeleteWaitlistEntry(ctx, token); err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	return done, nil
}

func (h *Handler) CountPlanInterest(ctx context.Context, req *generated.PlanInterestForm) (*generated.LandingRedirect, error) {
	if _, err := h.landingRedirect("/"); err != nil {
		return nil, err
	}
	plan := req.Plan.Or("")
	if !slices.Contains(waitlistPlans, plan) {
		return h.landingRedirect("/#preise")
	}
	if err := h.waitlist.IncrementPricingClick(ctx, plan, h.now().UTC()); err != nil {
		return nil, err
	}
	return h.landingRedirect("/bald/" + plan + "/")
}

// validEmail accepts a bare address (no display name) of sane length.
func validEmail(s string) bool {
	if len(s) > 254 || !strings.Contains(s, "@") {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && a.Name == ""
}
