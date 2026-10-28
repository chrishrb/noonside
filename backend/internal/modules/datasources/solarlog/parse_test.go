package solarlog

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return b
}

func TestParseMinuteLine(t *testing.T) {
	rec, err := ParseMinuteLine(`m[mi++]="12.10.24 14:10:00|3424;3552;12944;501;53|2358;2423;8870;478"`, berlin)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 10, 12, 14, 10, 0, 0, berlin), rec.Time)
	require.Len(t, rec.Readings, 2)

	r := rec.Readings[0]
	assert.Equal(t, 1, r.Inverter)
	assert.Equal(t, 3424.0, r.Pac)
	assert.Equal(t, 3552.0, r.Pdc)
	assert.Equal(t, 12944.0, r.Eday, "third value is the daily energy counter")
	assert.Equal(t, 501.0, r.Udc)
	require.NotNil(t, r.Temperature)
	assert.Equal(t, 53.0, *r.Temperature)

	assert.Nil(t, rec.Readings[1].Temperature)

	rec, err = ParseMinuteLine("   ", berlin)
	assert.NoError(t, err)
	assert.Nil(t, rec)

	_, err = ParseMinuteLine(`m[mi++]="garbage|1;2;3;4"`, berlin)
	assert.Error(t, err)
	_, err = ParseMinuteLine(`m[mi++]="12.10.24 14:10:00|1;2"`, berlin)
	assert.Error(t, err)
}

func TestParseMinuteFile(t *testing.T) {
	recs, err := ParseLines(read(t, "min241011.js"), func(l string) (*MinuteRecord, error) { return ParseMinuteLine(l, berlin) })
	require.NoError(t, err)
	assert.Len(t, recs, 284)
	for _, r := range recs {
		assert.Len(t, r.Readings, 11)
	}
}

func TestParseLinesTruncatedRecord(t *testing.T) {
	data := []byte(`m[mi++]="07.12.16 23:45:00|0;0;4100;0|0;0;3900;0"
m[mi++]="07.12.16 23:50:00|0;0;4100;0|0;0;3m[mi++]="07.12.16 23:55:00|0;0;4100;0|0;0;3900;0"
m[mi++]="08.12.16 00:00:00|0;0;4100;0|0;0;3900;0";
`)
	recs, err := ParseLines(data, func(l string) (*MinuteRecord, error) { return ParseMinuteLine(l, berlin) })
	require.NoError(t, err)
	require.Len(t, recs, 3)
	assert.Equal(t, time.Date(2016, 12, 7, 23, 45, 0, 0, berlin), recs[0].Time)
	assert.Equal(t, time.Date(2016, 12, 7, 23, 55, 0, 0, berlin), recs[1].Time)
	assert.Equal(t, 3900.0, recs[1].Readings[1].Eday)
	assert.Equal(t, time.Date(2016, 12, 8, 0, 0, 0, 0, berlin), recs[2].Time)
}

func TestParseDays(t *testing.T) {
	recs, err := ParseLines(read(t, "days_hist.js"), ParseDayLine)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, time.Date(2024, 10, 11, 0, 0, 0, 0, time.UTC), recs[0].Date)
	require.Len(t, recs[0].EnergyWh, 11)
	assert.Equal(t, 20246.0, recs[0].EnergyWh[0])
	assert.Equal(t, 14726.0, recs[0].EnergyWh[10])
}

func TestDaysMatchMinuteCounter(t *testing.T) {
	// The last eday of a minute file must equal the day total in days_hist.
	mins, err := ParseLines(read(t, "min241011.js"), func(l string) (*MinuteRecord, error) { return ParseMinuteLine(l, berlin) })
	require.NoError(t, err)
	days, err := ParseLines(read(t, "days_hist.js"), ParseDayLine)
	require.NoError(t, err)

	last := mins[0] // newest first
	for i, r := range last.Readings {
		assert.Equal(t, days[0].EnergyWh[i], r.Eday, "inverter %d", i+1)
	}
}

func TestParseBaseVars(t *testing.T) {
	bv, err := ParseBaseVars(read(t, "base_vars.js"))
	require.NoError(t, err)
	assert.Equal(t, 78360.0, bv.PeakPowerW)
	assert.Equal(t, 950.0, bv.TargetYearKWp)
	assert.Equal(t, 3914.0, bv.TariffCtPer)
	assert.Equal(t, []float64{2, 6, 9, 11, 11, 13, 13, 12, 10, 6, 4, 3}, bv.TargetMonthly)
	assert.Equal(t, "PV-Anlage Muster", bv.Title)
	assert.Equal(t, "35 Grad; 18 Grad von süd nach west", bv.Orientation)

	require.Len(t, bv.Inverters, 11)
	first := bv.Inverters[0]
	assert.Equal(t, 1, first.Index)
	assert.Equal(t, "WR 1", first.Name)
	assert.Equal(t, "PAC7", first.Model)
	assert.Equal(t, "10002579", first.Serial)
	assert.Equal(t, 7800.0, first.PeakPowerW)
	assert.Equal(t, "MPP", Label(first.StatusCodes, 3))
	assert.Equal(t, "Isolationsfehler", Label(first.ErrorCodes, 2))

	last := bv.Inverters[10]
	assert.Equal(t, "WR 11", last.Name)
	assert.Equal(t, "4800 TL", last.Model)
	assert.Equal(t, "Standby", Label(last.StatusCodes, 0))
	assert.Equal(t, "", Label(last.StatusCodes, 99))
}

func TestParseCurrent(t *testing.T) {
	c, err := ParseCurrent(read(t, "min_cur.js"), berlin)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 10, 12, 13, 0, 36, 0, berlin), c.Time)
	require.Len(t, c.Pac, 11)
	assert.Equal(t, 4625.0, c.Pac[0])
	assert.Equal(t, 4788.0, c.Pdc[0])
	assert.Len(t, c.StatusCodes, 11)
	assert.Equal(t, 3, c.StatusCodes[0])
	assert.Equal(t, 0, c.ErrorCodes[10])
}

func TestMinuteFiles(t *testing.T) {
	names := []string{"min241012.js", "min241010.js", "min241011.js", "min_day.js", "base_vars.js", "min_cur.js"}
	assert.Equal(t, []string{"min241010.js", "min241011.js", "min241012.js", "min_day.js"}, minuteFiles(names, nil, nil, berlin))

	last := time.Date(2024, 10, 11, 12, 0, 0, 0, berlin)
	assert.Equal(t, []string{"min241011.js", "min241012.js", "min_day.js"}, minuteFiles(names, &last, nil, berlin))

	// the later of last import and retention cutoff wins
	names = append(names, "min231231.js", "min250101.js")
	cutoff := time.Date(2025, 1, 1, 0, 0, 0, 0, berlin)
	assert.Equal(t, []string{"min250101.js", "min_day.js"}, minuteFiles(names, &last, &cutoff, berlin))
	assert.Equal(t, []string{"min241011.js", "min241012.js", "min250101.js", "min_day.js"}, minuteFiles(names, &last, &time.Time{}, berlin))
}
