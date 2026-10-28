package email

import "fmt"

// Alerts reports one or more alert notifications. The caller renders all
// text in the recipient's language (see the alerting and notifications modules).
type Alerts struct {
	Heading     string // used as subject for more than one item
	Items       []AlertItem
	ButtonURL   string
	ButtonLabel string
	ManageURL   string
	ManageLabel string
	Footer      string
}

type AlertItem struct {
	Title    string
	Body     string
	Severity string // info, warning, error
	URL      string
}

func (Alerts) Name() string { return "alerts" }

func (e Alerts) Subject() string {
	if len(e.Items) == 1 {
		return e.Items[0].Title
	}
	return e.Heading
}

func (e Alerts) Preheader() string {
	if len(e.Items) == 0 {
		return ""
	}
	return e.Items[0].Body
}

// Headers lets mail clients offer an unsubscribe action.
func (e Alerts) Headers() map[string]string {
	if e.ManageURL == "" {
		return nil
	}
	return map[string]string{"List-Unsubscribe": fmt.Sprintf("<%s>", e.ManageURL)}
}

// Color is the severity's accent for templates.
func (i AlertItem) Color() string {
	switch i.Severity {
	case "error":
		return "#dc2626"
	case "warning":
		return "#d97706"
	}
	return "#2563eb"
}

func init() {
	Register(func() Email {
		return Alerts{
			Heading: "2 new notifications",
			Items: []AlertItem{
				{Title: "WR 2 reports an error", Body: "PV-Anlage Muster · Isolation fault since Sep 29, 2026 10:42.", Severity: "error", URL: "http://localhost:5173/s/x/status"},
				{Title: "Data source FTP is failing", Body: "No successful run since Sep 29, 2026 08:15: dial tcp: i/o timeout", Severity: "warning", URL: "http://localhost:5173/s/x/settings/sources"},
			},
			ButtonURL: "http://localhost:5173/notifications", ButtonLabel: "Open notifications",
			ManageURL: "http://localhost:5173/profile/notifications", ManageLabel: "Manage notifications",
			Footer: "You received this email because you are a member of a system on noonside.",
		}
	})
}
