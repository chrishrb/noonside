package alerting

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/notifications"
)

// Message is the rendered text of an alert.
type Message = notifications.Message

func normLocale(l string) string { return notifications.NormLocale(l) }

type texts struct {
	title, body string // fmt with subject name / details, see render
}

var alertTexts = map[string]map[string]texts{
	"en": {
		KindNoData:            {"No data from %[1]s", "No data has arrived for %[2]s (last data %[3]s)."},
		KindInverterOffline:   {"%[1]s is offline", "No data from this inverter for %[2]s while the others report (last data %[3]s)."},
		KindInverterError:     {"%[1]s reports an error", "%[4]s since %[3]s."},
		KindDataSourceFailing: {"Data source %[1]s is failing", "No successful run since %[3]s: %[4]s"},
		KindUnderperformance:  {"%[1]s yields less than expected", "Only %[2]s of the other inverters' specific yield%[4]s on the last two days."},
		KindZeroDay:           {"No production at %[1]s", "The inverters produced 0 kWh on %[3]s although data arrived."},
		KindLowYield:          {"%[1]s yields less than the weather allowed", "Only %[2]s of the expected yield on %[3]s (%[4]s)."},
		KindClipping:          {"%[1]s is clipping", "The power sat at the rated AC power (%[4]s) for at least %[2]s a day on %[3]s of the last 7 days. The modules could deliver more than the inverter converts."},
		KindFeedInCap:         {"%[1]s: power capped at %[2]s", "The power plateaued at about %[2]s of the peak power (%[4]s) on %[3]s of the last 7 days. A feed-in limit is probably active; the energy above it is lost."},
		KindImplausibleData:   {"%[1]s sends implausible values", "%[2]s values from %[3]s were discarded as implausible: %[4]s. Check the inverter's clock, the logger and the kWp in the settings."},
	},
	"de": {
		KindNoData:            {"Keine Daten von %[1]s", "Seit %[2]s sind keine Daten eingegangen (zuletzt %[3]s)."},
		KindInverterOffline:   {"%[1]s ist offline", "Seit %[2]s keine Daten von diesem Wechselrichter, während die anderen melden (zuletzt %[3]s)."},
		KindInverterError:     {"%[1]s meldet einen Fehler", "%[4]s seit %[3]s."},
		KindDataSourceFailing: {"Datenquelle %[1]s schlägt fehl", "Kein erfolgreicher Abruf seit %[3]s: %[4]s"},
		KindUnderperformance:  {"%[1]s liefert weniger als erwartet", "Nur %[2]s des spezifischen Ertrags der anderen Wechselrichter%[4]s an den letzten zwei Tagen."},
		KindZeroDay:           {"Kein Ertrag bei %[1]s", "Die Wechselrichter haben am %[3]s 0 kWh erzeugt, obwohl Daten eingingen."},
		KindLowYield:          {"%[1]s liefert weniger, als das Wetter erlaubt", "Nur %[2]s des erwarteten Ertrags am %[3]s (%[4]s)."},
		KindClipping:          {"%[1]s regelt ab", "Die Leistung lag an %[3]s der letzten 7 Tage mindestens %[2]s täglich an der AC-Nennleistung (%[4]s). Die Module könnten mehr liefern, als der Wechselrichter umsetzt."},
		KindFeedInCap:         {"%[1]s: Leistung bei %[2]s begrenzt", "Die Leistung blieb an %[3]s der letzten 7 Tage bei etwa %[2]s der Spitzenleistung (%[4]s) stehen. Vermutlich ist eine Einspeisebegrenzung aktiv; die Energie darüber geht verloren."},
		KindImplausibleData:   {"%[1]s sendet unplausible Werte", "%[2]s Werte vom %[3]s wurden als unplausibel verworfen: %[4]s. Prüfen Sie die Uhr des Wechselrichters, den Logger und die kWp in den Einstellungen."},
	},
}

var phrases = map[string]map[string]string{
	"en": {
		"reject_future": "time in the future", "reject_negative_power": "negative power",
		"reject_power_above_limit": "power above the rated power", "reject_eday_above_limit": "daily energy too high for the kWp",
		"reject_daily_above_limit": "daily energy too high for the kWp", "reject_negative_energy": "negative energy",
		"resolved": "Resolved: %s", "reminder": "Still open: %s", "resolved_body": "The problem is no longer detected.",
		"unknown": "unknown", "never": "never", "error": "Error %d",
		"forecast_title": "%s: about %s kWh today", "forecast_body": "Expected from the weather forecast for %s.",
		"forecast_window": "Best time for large loads: %s–%s.",
	},
	"de": {
		"reject_future": "Zeit in der Zukunft", "reject_negative_power": "negative Leistung",
		"reject_power_above_limit": "Leistung über der Nennleistung", "reject_eday_above_limit": "Tagesenergie zu hoch für die kWp",
		"reject_daily_above_limit": "Tagesenergie zu hoch für die kWp", "reject_negative_energy": "negative Energie",
		"resolved": "Behoben: %s", "reminder": "Weiterhin offen: %s", "resolved_body": "Das Problem wird nicht mehr erkannt.",
		"unknown": "unbekannt", "never": "nie", "error": "Fehler %d",
		"forecast_title": "%s: heute etwa %s kWh", "forecast_body": "Erwartet laut Wettervorhersage für den %s.",
		"forecast_window": "Beste Zeit für große Verbraucher: %s–%s Uhr.",
	},
}

// Phrase returns a fixed phrase in the locale.
func Phrase(locale, key string) string { return phrases[normLocale(locale)][key] }

// RenderAlert renders an alert of kind about subject (the system, inverter
// or data source name) with its details, in the locale and the system's
// time zone.
func RenderAlert(locale, kind, subject string, details map[string]any, loc *time.Location) Message {
	l := normLocale(locale)
	t, ok := alertTexts[l][kind]
	if !ok {
		return Message{Title: kind}
	}
	var a2, a3, a4 string
	switch kind {
	case KindNoData, KindInverterOffline:
		a2 = duration(l, num(details["minutes"]))
		a3 = instant(l, details["last_seen"], loc)
	case KindInverterError:
		a3 = instant(l, details["since"], loc)
		a4, _ = details["text"].(string)
		if a4 == "" {
			a4 = fmt.Sprintf(Phrase(l, "error"), int(num(details["code"])))
		}
	case KindDataSourceFailing:
		if name, _ := details["name"].(string); name != "" {
			subject = name
		}
		a3 = instant(l, details["last_success"], loc)
		if a3 == "" {
			a3 = Phrase(l, "never")
		}
		a4, _ = details["error"].(string)
	case KindUnderperformance:
		a2 = fmt.Sprintf("%d %%", int(num(details["percent"])))
		if plane, _ := details["plane"].(string); plane != "" {
			a4 = fmt.Sprintf(" on “%s”", plane)
			if l == "de" {
				a4 = fmt.Sprintf(" auf „%s“", plane)
			}
		}
	case KindZeroDay:
		a3 = day(l, details["day"])
	case KindLowYield:
		a2 = fmt.Sprintf("%d %%", int(num(details["percent"])))
		a3 = day(l, details["day"])
		of := "%s of %s kWh"
		if l == "de" {
			of = "%s von %s kWh"
		}
		a4 = fmt.Sprintf(of, decimal(l, num(details["actual_kwh"])), decimal(l, num(details["expected_kwh"])))
	case KindClipping:
		a2 = duration(l, num(details["minutes"]))
		a3 = strconv.Itoa(int(num(details["days"])))
		a4 = watts(l, num(details["limit_w"]))
	case KindImplausibleData:
		a2 = strconv.Itoa(int(num(details["count"])))
		a3 = day(l, details["day"])
		var rs []string
		switch v := details["reasons"].(type) {
		case []string:
			rs = v
		case []any: // after a JSON round trip
			for _, x := range v {
				if s, ok := x.(string); ok {
					rs = append(rs, s)
				}
			}
		}
		seen := map[string]bool{}
		var parts []string
		for _, r := range rs {
			txt := Phrase(l, "reject_"+r)
			if txt == "" {
				txt = r
			}
			if !seen[txt] {
				seen[txt] = true
				parts = append(parts, txt)
			}
		}
		a4 = strings.Join(parts, ", ")
	case KindFeedInCap:
		a2 = fmt.Sprintf("%d %%", int(num(details["percent"])))
		a3 = strconv.Itoa(int(num(details["days"])))
		a4 = watts(l, num(details["limit_w"]))
	}
	return Message{Title: fmt.Sprintf(t.title, subject, a2, a3, a4), Body: fmt.Sprintf(t.body, subject, a2, a3, a4)}
}

// registerRenderers tells the notifications module how alert and forecast
// notifications render.
func registerRenderers() {
	for _, typ := range []string{notifications.NotificationAlertOpened, notifications.NotificationAlertResolved,
		notifications.NotificationAlertReminder, notifications.NotificationForecast} {
		notifications.RegisterRenderer(typ, RenderNotification)
	}
}

// RenderNotification renders an alert or forecast notification in the
// locale; other types render through the notifications module.
func RenderNotification(locale string, n notifications.Notification) Message {
	p := n.Payload
	str := func(k string) string { s, _ := p[k].(string); return s }
	switch n.Type {
	case notifications.NotificationAlertOpened, notifications.NotificationAlertResolved, notifications.NotificationAlertReminder:
	case notifications.NotificationForecast:
		l := normLocale(locale)
		m := Message{
			Title: fmt.Sprintf(Phrase(l, "forecast_title"), str("system_name"), decimal(l, num(p["expected_kwh"]))),
			Body:  fmt.Sprintf(Phrase(l, "forecast_body"), day(l, p["date"])),
		}
		loc, _ := time.LoadLocation(str("timezone"))
		if loc == nil {
			loc = time.UTC
		}
		from, err1 := time.Parse(time.RFC3339, str("window_start"))
		to, err2 := time.Parse(time.RFC3339, str("window_end"))
		if err1 == nil && err2 == nil {
			m.Body += " " + fmt.Sprintf(Phrase(l, "forecast_window"), from.In(loc).Format("15:04"), to.In(loc).Format("15:04"))
		}
		return m
	default:
		return notifications.RenderNotification(locale, n)
	}
	loc := time.UTC
	if tz := str("timezone"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	details, _ := p["details"].(map[string]any)
	m := RenderAlert(locale, str("kind"), str("subject"), details, loc)
	if sys := str("system_name"); sys != "" && str("subject") != sys {
		m.Body = sys + " · " + m.Body
	}
	switch n.Type {
	case notifications.NotificationAlertResolved:
		m.Title = fmt.Sprintf(Phrase(locale, "resolved"), m.Title)
		m.Body = Phrase(locale, "resolved_body")
		if sys := str("system_name"); sys != "" {
			m.Body = sys + " · " + m.Body
		}
	case notifications.NotificationAlertReminder:
		m.Title = fmt.Sprintf(Phrase(locale, "reminder"), m.Title)
	}
	return m
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

// decimal formats v with one decimal in the locale.
func decimal(l string, v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if l == "de" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// watts formats a power, in kW from 1000 W.
func watts(l string, w float64) string {
	if w >= 1000 {
		return decimal(l, w/1000) + " kW"
	}
	return strconv.Itoa(int(w+0.5)) + " W"
}

func duration(l string, minutes float64) string {
	m := int(minutes)
	if m < 120 {
		if l == "de" {
			return fmt.Sprintf("%d Minuten", m)
		}
		return fmt.Sprintf("%d minutes", m)
	}
	h := m / 60
	if h < 48 {
		if l == "de" {
			return fmt.Sprintf("%d Stunden", h)
		}
		return fmt.Sprintf("%d hours", h)
	}
	if l == "de" {
		return fmt.Sprintf("%d Tagen", h/24)
	}
	return fmt.Sprintf("%d days", h/24)
}

func instant(l string, v any, loc *time.Location) string {
	s, _ := v.(string)
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	if l == "de" {
		return t.In(loc).Format("02.01.2006 15:04")
	}
	return t.In(loc).Format("Jan 2, 2006 15:04")
}

func day(l string, v any) string {
	s, _ := v.(string)
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return s
	}
	if l == "de" {
		return t.Format("02.01.2006")
	}
	return t.Format("Jan 2, 2006")
}
