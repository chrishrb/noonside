package email

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AuthAction is a generic "click to continue" email. It is used to render the
// GoTrue email templates (confirm sign-up, reset password, …) in the
// noonside layout; see ExportAuthTemplates.
type AuthAction struct {
	Title       string
	Intro       string
	ButtonLabel string
	URL         string
	Note        string
	Preview     string
}

func (AuthAction) Name() string        { return "auth_action" }
func (e AuthAction) Subject() string   { return e.Title }
func (e AuthAction) Preheader() string { return e.Preview }

func init() {
	Register(func() Email {
		return AuthAction{
			Title:       "Reset your password",
			Intro:       "We received a request to reset the password of your noonside account.",
			ButtonLabel: "Choose a new password",
			URL:         "http://localhost:5173/reset#token=example",
			Note:        "If you didn't ask for this, you can ignore this email. Your password stays the same.",
			Preview:     "Use this link to choose a new password.",
		}
	})
}

// AuthTemplate is one Auth (GoTrue) email (GOTRUE_MAILER_TEMPLATES_<KIND>).
type AuthTemplate struct {
	Kind    string // confirmation, recovery, magic_link, email_change
	Subject string
	Email   AuthAction
}

// urlSentinel stands in for GoTrue's {{ .ConfirmationURL }} while rendering,
// because html/template would escape the braces inside href attributes.
const urlSentinel = "https://gotrue-confirmation-url.invalid/"

// siteSentinel stands in for the app's URL, which becomes GoTrue's
// {{ .SiteURL }} (GOTRUE_SITE_URL), so the templates fit every installation.
const siteSentinel = "https://gotrue-site-url.invalid"

// AuthTemplates are the GoTrue emails used by noonside.
var AuthTemplates = []AuthTemplate{
	{Kind: "confirmation", Subject: "Confirm your email", Email: AuthAction{
		Title: "Confirm your email", Intro: "Please confirm this address to finish setting up your noonside account.",
		ButtonLabel: "Confirm email", Note: "If you didn't create an account, you can ignore this email.",
		Preview: "Confirm your email address for noonside.",
	}},
	{Kind: "recovery", Subject: "Reset your password", Email: AuthAction{
		Title: "Reset your password", Intro: "We received a request to reset the password of your noonside account.",
		ButtonLabel: "Choose a new password", Note: "If you didn't ask for this, you can ignore this email. Your password stays the same.",
		Preview: "Use this link to choose a new password.",
	}},
	{Kind: "magic_link", Subject: "Your sign-in link", Email: AuthAction{
		Title: "Sign in to noonside", Intro: "Use the button below to sign in. The link can only be used once.",
		ButtonLabel: "Sign in", Note: "If you didn't try to sign in, you can ignore this email.",
		Preview: "Your one-time sign-in link for noonside.",
	}},
	{Kind: "email_change", Subject: "Confirm your new email", Email: AuthAction{
		Title: "Confirm your new email", Intro: "Please confirm that you want to use this address for your noonside account.",
		ButtonLabel: "Confirm new email", Note: "If you didn't request this change, secure your account by resetting your password.",
		Preview: "Confirm the change of your noonside email address.",
	}},
}

// RenderAuthTemplate renders t with GoTrue's placeholders in place of
// the link and the app's URL.
func (r *Renderer) RenderAuthTemplate(t AuthTemplate) (string, error) {
	e := t.Email
	e.URL = urlSentinel
	site := *r
	site.app.URL = siteSentinel
	out, err := site.Render(e)
	if err != nil {
		return "", err
	}
	html := strings.ReplaceAll(out.HTML, urlSentinel, "{{ .ConfirmationURL }}")
	return strings.ReplaceAll(html, siteSentinel, "{{ .SiteURL }}"), nil
}

// ExportAuthTemplates writes <dir>/<kind>.html for every Auth template.
func (r *Renderer) ExportAuthTemplates(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	for _, t := range AuthTemplates {
		html, err := r.RenderAuthTemplate(t)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.Kind, err)
		}
		path := filepath.Join(dir, t.Kind+".html")
		if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}
