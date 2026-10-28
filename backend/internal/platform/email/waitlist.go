package email

import (
	"fmt"
	"time"
)

// WaitlistConfirm is the double opt-in email of the landing page's waitlist.
// The landing page is German only, so is this email.
type WaitlistConfirm struct {
	ConfirmURL     string
	UnsubscribeURL string
	// OneClickURL takes a POST without a body (RFC 8058 one-click unsubscribe).
	OneClickURL string
	ExpiresAt   time.Time
}

func (WaitlistConfirm) Name() string    { return "waitlist_confirm" }
func (WaitlistConfirm) Subject() string { return "Bitte bestätigen Sie Ihre Anmeldung zur Warteliste" }
func (WaitlistConfirm) Preheader() string {
	return "Ein Klick, dann sind Sie auf der Warteliste von noonside."
}

// Headers lets mail clients offer one-click unsubscribe.
func (e WaitlistConfirm) Headers() map[string]string {
	if e.OneClickURL == "" {
		return nil
	}
	return map[string]string{
		"List-Unsubscribe":      fmt.Sprintf("<%s>", e.OneClickURL),
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	}
}

// ExpiresDate is the last day of the link in German notation.
func (e WaitlistConfirm) ExpiresDate() string { return e.ExpiresAt.Format("02.01.2006") }

func init() {
	Register(func() Email {
		return WaitlistConfirm{
			ConfirmURL:     "http://localhost:4321/warteliste/bestaetigen/#t=s0w_example",
			UnsubscribeURL: "http://localhost:4321/warteliste/abmelden/#t=s0u_example",
			OneClickURL:    "http://localhost:8080/api/v1/waitlist/unsubscribe?token=s0u_example",
			ExpiresAt:      time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		}
	})
}
