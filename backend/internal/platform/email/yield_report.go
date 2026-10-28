package email

import "fmt"

// YieldReport is a periodic yield report. The caller renders all text in the
// recipient's language (see the reports module).
type YieldReport struct {
	Heading string // also the subject
	Intro   string
	// Facts are label/value rows (energy, expected, best day, ...).
	Facts            [][2]string
	InvertersHeading string
	Inverters        []ReportInverter
	AlertsHeading    string
	Alerts           []AlertItem
	NoAlerts         string
	ButtonURL        string
	ButtonLabel      string
	ManageURL        string
	ManageLabel      string
	Footer           string
}

type ReportInverter struct {
	Name   string
	Energy string
	Share  string
	// SharePct sizes the bar (0-100).
	SharePct float64
}

func (YieldReport) Name() string        { return "yield_report" }
func (e YieldReport) Subject() string   { return e.Heading }
func (e YieldReport) Preheader() string { return e.Intro }

// Headers lets mail clients offer an unsubscribe action.
func (e YieldReport) Headers() map[string]string {
	if e.ManageURL == "" {
		return nil
	}
	return map[string]string{"List-Unsubscribe": fmt.Sprintf("<%s>", e.ManageURL)}
}

// FactRows converts Facts for the "facts" partial.
func (e YieldReport) FactRows() [][2]any {
	out := make([][2]any, len(e.Facts))
	for i, f := range e.Facts {
		out[i] = [2]any{f[0], f[1]}
	}
	return out
}

// BarWidth is the share bar's width in % (at least 1, so it stays visible).
func (i ReportInverter) BarWidth() int { return max(1, min(100, int(i.SharePct+0.5))) }

func init() {
	Register(func() Email {
		return YieldReport{
			Heading: "PV-Anlage Muster: September 2026",
			Intro:   "1,234.5 kWh in September 2026, 96 % of the expected yield.",
			Facts: [][2]string{
				{"Energy", "1,234.5 kWh"}, {"Specific yield", "123.45 kWh/kWp"}, {"Expected", "1,286.0 kWh (96 %)"},
				{"Best day", "Sep 3, 2026 · 62.1 kWh"}, {"Revenue", "98.76 EUR"}, {"CO₂ avoided", "493.8 kg"},
			},
			InvertersHeading: "Inverters",
			Inverters: []ReportInverter{
				{Name: "WR 1", Energy: "700.0 kWh", Share: "56.7 %", SharePct: 56.7},
				{Name: "WR 2", Energy: "534.5 kWh", Share: "43.3 %", SharePct: 43.3},
			},
			AlertsHeading: "Alerts in this period",
			Alerts: []AlertItem{
				{Title: "WR 2 is offline", Body: "Sep 12, 2026 09:14", Severity: "warning", URL: "http://localhost:5173/s/x/status"},
			},
			NoAlerts:  "No alerts in this period.",
			ButtonURL: "http://localhost:5173/s/x/overview?period=month&date=2026-09-01", ButtonLabel: "Open the month",
			ManageURL: "http://localhost:5173/profile/notifications", ManageLabel: "Manage reports",
			Footer: "You receive this report because you chose it in your notification settings on noonside.",
		}
	})
}
