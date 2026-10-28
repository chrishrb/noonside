package notify

import (
	"context"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// NtfyPriority maps a notification to ntfy's 1-5 scale: errors are urgent,
// warnings high, anything else (and resolved alerts) default.
func NtfyPriority(m Message) int {
	if m.Type == "alert_resolved" {
		return 3
	}
	switch m.Severity {
	case "error":
		return 5
	case "warning":
		return 4
	}
	return 3
}

// ntfyTags are shown as emoji in the ntfy app.
func ntfyTags(m Message) string {
	tags := []string{"noonside"}
	switch {
	case m.Type == "alert_resolved":
		tags = append(tags, "white_check_mark")
	case m.Severity == "error":
		tags = append(tags, "rotating_light")
	case m.Severity == "warning":
		tags = append(tags, "warning")
	case m.Type == "forecast":
		tags = append(tags, "sunny")
	}
	return strings.Join(tags, ",")
}

// Ntfy publishes m to the topic URL (e.g. https://ntfy.sh/my-topic). token
// is an optional access token.
func (c *Client) Ntfy(ctx context.Context, topicURL, token string, m Message) error {
	req, err := http.NewRequest(http.MethodPost, topicURL, strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	// ntfy decodes RFC 2047 headers, so titles with umlauts survive.
	// Names in titles come from users; a line break would break the header.
	title := strings.Join(strings.Fields(m.Title), " ")
	req.Header.Set("Title", mime.BEncoding.Encode("utf-8", title))
	req.Header.Set("Priority", strconv.Itoa(NtfyPriority(m)))
	req.Header.Set("Tags", ntfyTags(m))
	if m.URL != "" {
		req.Header.Set("Click", m.URL)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return c.do(ctx, req)
}
