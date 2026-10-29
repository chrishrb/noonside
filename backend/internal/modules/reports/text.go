package reports

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/notifications"
	"github.com/chrishrb/noonside/internal/platform/email"
)

var phrases = map[string]map[string]string{
	"en": {
		KindDaily: "Daily report", KindWeekly: "Weekly report", KindMonthly: "Monthly report", KindYearly: "Annual report",
		"subject":    "%s · %s %s",
		"intro":      "%s produced %s %s.",
		"when_daily": "on %s", "when_weekly": "in the week %s", "when_monthly": "in %s", "when_yearly": "in %s",
		"intro_pct": " That is %s of the expected yield.", "intro_target_pct": " That is %s of the target.",
		"energy": "Energy", "specific": "Specific yield", "expected": "Expected", "target": "Target",
		"best_day": "Best day", "previous": "Previous year", "revenue": "Revenue", "co2": "CO₂ avoided",
		"inverters": "Inverters", "alerts": "Alerts in this period", "no_alerts": "No alerts in this period.",
		"open": "Open the overview", "manage": "Manage reports",
		"footer":      "You receive this report because you chose it in your notification settings on noonside.",
		"notif_title": "%s: %s %s", "notif_body": "%s produced.", "notif_body_pct": "%s produced, %s of the expected yield.",
	},
	"de": {
		KindDaily: "Tagesbericht", KindWeekly: "Wochenbericht", KindMonthly: "Monatsbericht", KindYearly: "Jahresbericht",
		"subject":    "%s · %s %s",
		"intro":      "%s hat %[3]s %[2]s erzeugt.",
		"when_daily": "am %s", "when_weekly": "in der Woche %s", "when_monthly": "im %s", "when_yearly": "im Jahr %s",
		"intro_pct": " Das sind %s des erwarteten Ertrags.", "intro_target_pct": " Das sind %s des Sollertrags.",
		"energy": "Ertrag", "specific": "Spezifischer Ertrag", "expected": "Erwartet", "target": "Soll",
		"best_day": "Bester Tag", "previous": "Vorjahr", "revenue": "Vergütung", "co2": "CO₂ vermieden",
		"inverters": "Wechselrichter", "alerts": "Meldungen in diesem Zeitraum", "no_alerts": "Keine Meldungen in diesem Zeitraum.",
		"open": "Übersicht öffnen", "manage": "Berichte verwalten",
		"footer":      "Sie erhalten diesen Bericht, weil Sie ihn in Ihren Benachrichtigungseinstellungen auf noonside gewählt haben.",
		"notif_title": "%s: %s %s", "notif_body": "%s erzeugt.", "notif_body_pct": "%s erzeugt, %s des erwarteten Ertrags.",
	},
	"es": {
		KindDaily: "Informe diario", KindWeekly: "Informe semanal", KindMonthly: "Informe mensual", KindYearly: "Informe anual",
		"subject":    "%s · %s %s",
		"intro":      "%s produjo %s %s.",
		"when_daily": "el %s", "when_weekly": "en la semana %s", "when_monthly": "en %s", "when_yearly": "en %s",
		"intro_pct": " Es el %s de la producción esperada.", "intro_target_pct": " Es el %s del objetivo.",
		"energy": "Producción", "specific": "Rendimiento específico", "expected": "Esperado", "target": "Objetivo",
		"best_day": "Mejor día", "previous": "Año anterior", "revenue": "Remuneración", "co2": "CO₂ evitado",
		"inverters": "Inversores", "alerts": "Alertas en este periodo", "no_alerts": "Ninguna alerta en este periodo.",
		"open": "Abrir el resumen", "manage": "Gestionar informes",
		"footer":      "Recibe este informe porque lo eligió en los ajustes de notificaciones de noonside.",
		"notif_title": "%s: %s %s", "notif_body": "%s producidos.", "notif_body_pct": "%s producidos, %s de la producción esperada.",
	},
}

var monthsDE = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
var monthsES = []string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
var monthsESShort = []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"}

func norm(l string) string { return notifications.NormLocale(l) }

func phrase(l, key string) string { return phrases[l][key] }

// Email renders the report for a recipient. publicURL is the app's base URL.
func Email(locale string, rep *Report, publicURL string) email.YieldReport {
	l := norm(locale)
	base := strings.TrimRight(publicURL, "/")
	loc := time.UTC
	if z, err := time.LoadLocation(rep.Timezone); err == nil {
		loc = z
	}
	label := PeriodLabel(l, rep)
	e := email.YieldReport{
		Heading:          fmt.Sprintf(phrase(l, "subject"), rep.SystemName, phrase(l, rep.Kind), label),
		InvertersHeading: phrase(l, "inverters"),
		AlertsHeading:    phrase(l, "alerts"),
		NoAlerts:         phrase(l, "no_alerts"),
		ButtonURL:        base + rep.Link,
		ButtonLabel:      phrase(l, "open"),
		ManageURL:        base + "/profile/notifications",
		ManageLabel:      phrase(l, "manage"),
		Footer:           phrase(l, "footer"),
	}
	when := fmt.Sprintf(phrase(l, "when_"+rep.Kind), label)
	e.Intro = fmt.Sprintf(phrase(l, "intro"), rep.SystemName, kwh(l, rep.EnergyKWh), when)
	pct := ""
	if rep.ExpectedKWh != nil && *rep.ExpectedKWh > 0 {
		pct = percent(l, rep.EnergyKWh / *rep.ExpectedKWh * 100)
		key := "intro_pct"
		if rep.ExpectedSource != "weather" {
			key = "intro_target_pct"
		}
		e.Intro += fmt.Sprintf(phrase(l, key), pct)
	}

	e.Facts = append(e.Facts, [2]string{phrase(l, "energy"), kwh(l, rep.EnergyKWh)})
	if rep.KWhPerKWp != nil {
		e.Facts = append(e.Facts, [2]string{phrase(l, "specific"), number(l, *rep.KWhPerKWp, 2) + " kWh/kWp"})
	}
	if rep.ExpectedKWh != nil {
		key := "expected"
		if rep.ExpectedSource != "weather" {
			key = "target"
		}
		v := kwh(l, *rep.ExpectedKWh)
		if pct != "" {
			v += " (" + pct + ")"
		}
		e.Facts = append(e.Facts, [2]string{phrase(l, key), v})
	}
	if rep.BestDay != "" {
		e.Facts = append(e.Facts, [2]string{phrase(l, "best_day"), day(l, rep.BestDay) + " · " + kwh(l, rep.BestDayKWh)})
	}
	if rep.PreviousKWh != nil {
		v := kwh(l, *rep.PreviousKWh)
		if *rep.PreviousKWh > 0 {
			d := (rep.EnergyKWh / *rep.PreviousKWh - 1) * 100
			sign := "+"
			if d < 0 {
				sign = "−"
			}
			v += " (" + sign + percent(l, math.Abs(d)) + ")"
		}
		e.Facts = append(e.Facts, [2]string{phrase(l, "previous"), v})
	}
	if rep.Revenue > 0 {
		e.Facts = append(e.Facts, [2]string{phrase(l, "revenue"), number(l, rep.Revenue, 2) + " " + rep.Currency})
	}
	if rep.CO2Kg > 0 {
		e.Facts = append(e.Facts, [2]string{phrase(l, "co2"), number(l, rep.CO2Kg, 1) + " kg"})
	}

	for _, inv := range rep.Inverters {
		e.Inverters = append(e.Inverters, email.ReportInverter{
			Name: inv.Name, Energy: kwh(l, inv.EnergyKWh), Share: percent(l, inv.SharePct), SharePct: inv.SharePct,
		})
	}
	for _, a := range rep.Alerts {
		m := alerting.RenderAlert(l, a.Kind, a.Subject, a.Details, loc)
		e.Alerts = append(e.Alerts, email.AlertItem{
			Title: m.Title, Body: instant(l, a.OpenedAt.In(loc)), Severity: a.Severity,
			URL: base + "/s/" + rep.SystemID.String() + "/status",
		})
	}
	return e
}

// RenderEmail renders a report notification as email (for the dispatcher).
func RenderEmail(publicURL string) func(locale string, n notifications.Notification) (email.Email, error) {
	return func(locale string, n notifications.Notification) (email.Email, error) {
		rep, err := FromPayload(n.Payload)
		if err != nil {
			return nil, err
		}
		return Email(locale, rep, publicURL), nil
	}
}

// Notification renders a report notification (in-app, push, ntfy, webhook).
func Notification(locale string, n notifications.Notification) notifications.Message {
	l := norm(locale)
	rep, err := FromPayload(n.Payload)
	if err != nil {
		return notifications.Message{Title: phrase(l, KindMonthly)}
	}
	m := notifications.Message{
		Title: fmt.Sprintf(phrase(l, "notif_title"), rep.SystemName, phrase(l, rep.Kind), PeriodLabel(l, rep)),
		Body:  fmt.Sprintf(phrase(l, "notif_body"), kwh(l, rep.EnergyKWh)),
	}
	if rep.ExpectedKWh != nil && *rep.ExpectedKWh > 0 && rep.ExpectedSource == "weather" {
		m.Body = fmt.Sprintf(phrase(l, "notif_body_pct"), kwh(l, rep.EnergyKWh), percent(l, rep.EnergyKWh / *rep.ExpectedKWh * 100))
	}
	return m
}

// PeriodLabel names the report's period, e.g. "September 2026".
func PeriodLabel(l string, rep *Report) string {
	from, err := time.Parse(time.DateOnly, rep.From)
	if err != nil {
		return rep.From
	}
	switch rep.Kind {
	case KindDaily:
		return day(l, rep.From)
	case KindWeekly:
		return day(l, rep.From) + " – " + day(l, from.AddDate(0, 0, 6).Format(time.DateOnly))
	case KindMonthly:
		switch l {
		case "de":
			return monthsDE[from.Month()-1] + " " + strconv.Itoa(from.Year())
		case "es":
			return monthsES[from.Month()-1] + " " + strconv.Itoa(from.Year())
		default:
			return from.Format("January 2006")
		}
	}
	return strconv.Itoa(from.Year())
}

func kwh(l string, v float64) string {
	if v >= 10000 {
		return number(l, v/1000, 2) + " MWh"
	}
	return number(l, v, 1) + " kWh"
}

func percent(l string, v float64) string { return number(l, v, 0) + " %" }

// number formats v with digits decimals and thousands separators.
func number(l string, v float64, digits int) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', digits, 64)
	intPart, frac, _ := strings.Cut(s, ".")
	sep, dec := ",", "."
	if l == "de" || l == "es" {
		sep, dec = ".", ","
	}
	var b strings.Builder
	if v < 0 {
		b.WriteString("-")
	}
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(c)
	}
	if frac != "" {
		b.WriteString(dec + frac)
	}
	return b.String()
}

func day(l, s string) string {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return s
	}
	switch l {
	case "de":
		return t.Format("02.01.2006")
	case "es":
		return fmt.Sprintf("%d %s %d", t.Day(), monthsESShort[t.Month()-1], t.Year())
	default:
		return t.Format("Jan 2, 2006")
	}
}

func instant(l string, t time.Time) string {
	switch l {
	case "de":
		return t.Format("02.01.2006 15:04")
	case "es":
		return fmt.Sprintf("%d %s %d %s", t.Day(), monthsESShort[t.Month()-1], t.Year(), t.Format("15:04"))
	default:
		return t.Format("Jan 2, 2006 15:04")
	}
}
