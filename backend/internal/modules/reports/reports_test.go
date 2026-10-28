package reports

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDue(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, loc) }
	date := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, loc) }

	assert.Empty(t, Due(at(2026, 9, 29, 5)), "before the window")
	assert.Empty(t, Due(at(2026, 9, 29, 12)), "after the window")

	// A Tuesday: only the daily report.
	due := Due(at(2026, 9, 29, 6))
	assert.Equal(t, map[string][2]time.Time{KindDaily: {date(2026, 9, 28), date(2026, 9, 29)}}, due)

	// A Monday: also last week.
	due = Due(at(2026, 9, 28, 7))
	assert.Equal(t, [2]time.Time{date(2026, 9, 21), date(2026, 9, 28)}, due[KindWeekly])
	assert.NotContains(t, due, KindMonthly)

	// New Year's Day 2027 (a Friday): last month and last year.
	due = Due(at(2027, 1, 1, 8))
	assert.Equal(t, [2]time.Time{date(2026, 12, 1), date(2027, 1, 1)}, due[KindMonthly])
	assert.Equal(t, [2]time.Time{date(2026, 1, 1), date(2027, 1, 1)}, due[KindYearly])
	assert.NotContains(t, due, KindWeekly)
}

func TestNumber(t *testing.T) {
	assert.Equal(t, "1,234.5", number("en", 1234.5, 1))
	assert.Equal(t, "1.234,5", number("de", 1234.5, 1))
	assert.Equal(t, "1.234.567", number("de", 1234567, 0))
	assert.Equal(t, "999", number("en", 999, 0))
	assert.Equal(t, "-12.30", number("en", -12.3, 2))
	assert.Equal(t, "12.35 MWh", kwh("en", 12345))
}

func TestEmail(t *testing.T) {
	exp, kwp := 1000.0, 9.5
	rep := &Report{
		Kind: KindMonthly, SystemID: uuid.Nil, SystemName: "Dach", Timezone: "Europe/Berlin", Currency: "EUR",
		From: "2026-09-01", To: "2026-10-01", EnergyKWh: 950, KWhPerKWp: &kwp, ExpectedKWh: &exp, ExpectedSource: "weather",
		BestDay: "2026-09-03", BestDayKWh: 51.2, Revenue: 76, CO2Kg: 380,
		Inverters: []InverterShare{{Name: "WR 1", EnergyKWh: 950, SharePct: 100}},
		Alerts:    []AlertLine{{Kind: "no_data", Subject: "Dach", Severity: "warning", OpenedAt: time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC), Details: map[string]any{"minutes": 120.0}}},
		Link:      "/s/x/overview?period=month&date=2026-09-01",
	}
	e := Email("de", rep, "https://app.test/")
	assert.Equal(t, "Dach · Monatsbericht September 2026", e.Heading)
	assert.Equal(t, "Dach hat im September 2026 950,0 kWh erzeugt. Das sind 95 % des erwarteten Ertrags.", e.Intro)
	assert.Contains(t, e.Facts, [2]string{"Erwartet", "1.000,0 kWh (95 %)"})
	assert.Contains(t, e.Facts, [2]string{"Bester Tag", "03.09.2026 · 51,2 kWh"})
	assert.Equal(t, "https://app.test/s/x/overview?period=month&date=2026-09-01", e.ButtonURL)
	require.Len(t, e.Alerts, 1)
	assert.Equal(t, "Keine Daten von Dach", e.Alerts[0].Title)
	assert.Equal(t, "12.09.2026 10:00", e.Alerts[0].Body)

	rep.Kind, rep.From, rep.To = KindWeekly, "2026-09-21", "2026-09-28"
	assert.Equal(t, "Sep 21, 2026 – Sep 27, 2026", PeriodLabel("en", rep))
	prev := 1900.0
	rep.Kind, rep.From, rep.PreviousKWh = KindYearly, "2026-01-01", &prev
	e = Email("en", rep, "https://app.test")
	assert.Equal(t, "Dach produced 950.0 kWh in 2026. That is 95 % of the expected yield.", e.Intro)
	assert.Contains(t, e.Facts, [2]string{"Previous year", "1,900.0 kWh (−50 %)"})
}
