package notifications

import (
	"strings"
	"sync"
)

// Message is the rendered text of a notification.
type Message struct {
	Title string
	Body  string
}

// Locales supported for server-rendered text; anything else falls back to
// English.
var Locales = []string{"en", "de"}

// NormLocale maps a locale to one of Locales.
func NormLocale(l string) string {
	if strings.HasPrefix(strings.ToLower(l), "de") {
		return "de"
	}
	return "en"
}

var phrases = map[string]map[string]string{
	"en": {
		"test_title": "Test notification", "test_body": "Push notifications work on this device.",
		"test_target_body": "noonside notifications reach this target.",
		"email_heading":    "%d new notifications", "open_notifications": "Open notifications",
		"manage": "Manage notifications", "email_footer": "You received this email because you are a member of a system on noonside.",
	},
	"de": {
		"test_title": "Testbenachrichtigung", "test_body": "Push-Benachrichtigungen funktionieren auf diesem Gerät.",
		"test_target_body": "Benachrichtigungen von noonside kommen hier an.",
		"email_heading":    "%d neue Benachrichtigungen", "open_notifications": "Benachrichtigungen öffnen",
		"manage": "Benachrichtigungen verwalten", "email_footer": "Sie erhalten diese E-Mail, weil Sie Mitglied einer Anlage auf noonside sind.",
	},
}

// Phrase returns a fixed phrase of the notifications module in the locale.
func Phrase(locale, key string) string { return phrases[NormLocale(locale)][key] }

// Renderer renders a notification of one type in a locale.
type Renderer func(locale string, n Notification) Message

// renderers render the notification types other modules own (alerting:
// alerts and the forecast, reports: reports), so every channel shows them
// the same way.
var (
	renderersMu sync.RWMutex
	renderers   = map[string]Renderer{}
)

// RegisterRenderer sets how notifications of typ are rendered. The modules
// owning a type call it when they are built.
func RegisterRenderer(typ string, r Renderer) {
	renderersMu.Lock()
	defer renderersMu.Unlock()
	renderers[typ] = r
}

// RenderNotification renders a stored notification in the locale.
func RenderNotification(locale string, n Notification) Message {
	renderersMu.RLock()
	r, ok := renderers[n.Type]
	renderersMu.RUnlock()
	if ok {
		return r(locale, n)
	}
	if n.Type == NotificationTest {
		return Message{Title: Phrase(locale, "test_title"), Body: Phrase(locale, "test_body")}
	}
	// announcements, and types this version doesn't know
	return Message{Title: localized(locale, n.Payload["title"]), Body: localized(locale, n.Payload["body"])}
}

// localized picks the locale's text from {"en": "...", "de": "..."},
// falling back to English, or takes a plain string as is.
func localized(locale string, v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		for _, l := range []string{NormLocale(locale), "en"} {
			if s, ok := t[l].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
