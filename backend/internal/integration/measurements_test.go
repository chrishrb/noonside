//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Minute data is stored per inverter and UTC day; reads must stitch days
// together and re-ingest must replace samples in place.
func TestMeasurementDays(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")

	var sys obj
	owner.do("POST", "/systems", obj{"name": "Days PV"}, 201, &sys)
	sid := sys["id"].(string)
	var key obj
	owner.do("POST", "/systems/"+sid+"/api-keys", obj{"name": "gateway"}, 201, &key)
	device := &client{e: e, http: e.anon().http, apiKey: key["key"].(string)}

	midnight := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	ingest := func(from, to time.Duration, pac float64) {
		var ms []obj
		for d := from; d <= to; d += 10 * time.Minute {
			ms = append(ms, obj{"inverter": "1", "time": midnight.Add(d).Format(time.RFC3339), "pac_w": pac, "pdc_w": pac + 1})
		}
		device.do("POST", "/systems/"+sid+"/ingest/measurements", obj{"measurements": ms, "create_inverters": true}, 200, nil)
	}
	ingest(-30*time.Minute, 20*time.Minute, 100) // 23:30..00:20 UTC, 6 samples over two days
	ingest(10*time.Minute, 40*time.Minute, 200)  // replaces 00:10, 00:20; adds 00:30, 00:40

	var rows, samples int
	require.NoError(t, e.db.Pool().QueryRow(ctx, `
		SELECT count(*), sum(cardinality(d.secs)) FROM measurement_days d
		JOIN inverters i ON i.id = d.inverter_id WHERE i.system_id = $1`, sid).Scan(&rows, &samples))
	assert.Equal(t, 2, rows)
	assert.Equal(t, 8, samples)

	systemID := uuid.MustParse(sid)
	last, err := e.Telemetry.LastMeasurementTime(ctx, systemID)
	require.NoError(t, err)
	require.NotNil(t, last)
	assert.True(t, last.Equal(midnight.Add(40*time.Minute)), "last = %s", last)

	from, to := midnight.Add(-time.Hour), midnight.Add(time.Hour)
	points, err := e.Telemetry.PowerCurve(ctx, systemID, from, to, 10*time.Minute)
	require.NoError(t, err)
	require.Len(t, points, 8)
	assert.True(t, points[0].Time.Equal(midnight.Add(-30*time.Minute)))
	want := []float64{100, 100, 100, 100, 200, 200, 200, 200}
	for i, p := range points {
		assert.InDelta(t, want[i], p.PacW, 1e-9, "point %d", i)
		assert.InDelta(t, want[i]+1, p.PdcW, 1e-9, "point %d", i)
	}

	// the range is half-open and trims inside a day row
	points, err = e.Telemetry.PowerCurve(ctx, systemID, midnight, midnight.Add(20*time.Minute), time.Hour)
	require.NoError(t, err)
	require.Len(t, points, 1)
	assert.InDelta(t, (100+200)/2.0, points[0].PacW, 1e-9)

	peaks, err := e.Telemetry.PeakPower(ctx, systemID, from, midnight)
	require.NoError(t, err)
	require.Len(t, peaks, 1)
	for _, v := range peaks {
		assert.InDelta(t, 100, v, 1e-9)
	}
}

// Retention deletes whole local calendar years of minute data and keeps
// later data, including the part of the UTC day row after local midnight.
// Deletion runs in batches outside the API request.
func TestMinuteRetention(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")

	var sys obj
	owner.do("POST", "/systems", obj{"name": "Retention PV", "timezone": "Europe/Berlin"}, 201, &sys)
	sid := sys["id"].(string)
	assert.NotContains(t, sys, "minute_retention_years")
	var key obj
	owner.do("POST", "/systems/"+sid+"/api-keys", obj{"name": "gateway"}, 201, &key)
	device := &client{e: e, http: e.anon().http, apiKey: key["key"].(string)}

	berlin := mustLoc(t)
	thisYear := time.Now().In(berlin).Year()
	newYear := time.Date(thisYear-1, 1, 1, 0, 0, 0, 0, berlin) // cutoff for 1 year
	times := []time.Time{
		newYear.AddDate(-1, 3, 0),      // two years ago: deleted
		newYear.Add(-30 * time.Minute), // 23:30 on 31 Dec, same UTC day as the next: deleted
		newYear.Add(30 * time.Minute),  // 00:30 on 1 Jan last year: kept
		newYear.AddDate(0, 6, 0),       // last year: kept
	}
	var ms []obj
	for _, ts := range times {
		ms = append(ms, obj{"inverter": "1", "time": ts.Format(time.RFC3339), "pac_w": 100, "eday_wh": 500})
	}
	device.do("POST", "/systems/"+sid+"/ingest/measurements", obj{"measurements": ms, "create_inverters": true}, 200, nil)

	samples := func() []time.Time {
		rows, err := e.db.Pool().Query(ctx, `
			SELECT d.day::timestamp AT TIME ZONE 'UTC' + s * interval '1 second'
			FROM measurement_days d JOIN inverters i ON i.id = d.inverter_id, unnest(d.secs) s
			WHERE i.system_id = $1 ORDER BY 1`, sid)
		require.NoError(t, err)
		defer rows.Close()
		var out []time.Time
		for rows.Next() {
			var ts time.Time
			require.NoError(t, rows.Scan(&ts))
			out = append(out, ts)
		}
		return out
	}
	require.Len(t, samples(), 4)

	// saving only stores the setting; the scheduler deletes in batches
	owner.do("PATCH", "/systems/"+sid, obj{"minute_retention_years": 1}, 200, &sys)
	assert.EqualValues(t, 1, sys["minute_retention_years"])
	require.Len(t, samples(), 4)
	id := uuid.MustParse(sid)
	var calls int
	var removed int64
	for done := false; !done; calls++ {
		n, d, err := e.Telemetry.DeleteMeasurementsBefore(ctx, id, newYear, 1)
		require.NoError(t, err)
		removed, done = removed+n, d
	}
	assert.Equal(t, 2, calls) // one old day row, then the trim of the boundary row
	assert.EqualValues(t, 2, removed)
	got := samples()
	require.Len(t, got, 2)
	assert.True(t, got[0].Equal(times[2]), "first kept sample %s", got[0])
	assert.True(t, got[1].Equal(times[3]))

	// old minute data is no longer stored, but still counts for daily energy
	old := newYear.AddDate(-1, 5, 0)
	device.do("POST", "/systems/"+sid+"/ingest/measurements", obj{"measurements": []obj{
		{"inverter": "1", "time": old.Format(time.RFC3339), "pac_w": 100, "eday_wh": 700},
	}}, 200, nil)
	assert.Len(t, samples(), 2)
	var energy float64
	require.NoError(t, e.db.Pool().QueryRow(ctx, `
		SELECT d.energy_wh FROM inverter_daily d JOIN inverters i ON i.id = d.inverter_id
		WHERE i.system_id = $1 AND d.day = $2`, sid, old.Format(time.DateOnly)).Scan(&energy))
	assert.InDelta(t, 700, energy, 1e-9)

	// nothing left to delete
	n, done, err := e.Telemetry.DeleteMeasurementsBefore(ctx, id, newYear, 500)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Zero(t, n)

	var cleared obj
	owner.do("PATCH", "/systems/"+sid, obj{"minute_retention_years": nil}, 200, &cleared)
	assert.NotContains(t, cleared, "minute_retention_years")
	owner.do("PATCH", "/systems/"+sid, obj{"minute_retention_years": -1}, 400, nil)
}

// Implausible values are dropped at ingest and counted per inverter, day and
// reason; the rest of a row is kept.
func TestIngestPlausibility(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")

	var sys obj
	owner.do("POST", "/systems", obj{"name": "Plausible PV", "peak_power_kwp": 5, "timezone": "UTC"}, 201, &sys)
	sid := sys["id"].(string)
	var key obj
	owner.do("POST", "/systems/"+sid+"/api-keys", obj{"name": "gateway"}, 201, &key)
	device := &client{e: e, http: e.anon().http, apiKey: key["key"].(string)}

	now := time.Now().UTC().Truncate(time.Minute)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	ms := []obj{
		{"inverter": "1", "time": at(-3 * time.Minute), "pac_w": 3000, "eday_wh": 10000}, // fine
		{"inverter": "1", "time": at(-2 * time.Minute), "pac_w": -20, "pdc_w": 3100},     // pac dropped
		{"inverter": "1", "time": at(-1 * time.Minute), "pac_w": 9000},                   // above 5 kWp × 1.3
		{"inverter": "1", "time": at(time.Hour), "pac_w": 100},                           // future: dropped
	}
	var res obj
	device.do("POST", "/systems/"+sid+"/ingest/measurements", obj{"measurements": ms, "create_inverters": true}, 200, &res)
	assert.EqualValues(t, 3, res["accepted"])
	assert.EqualValues(t, 3, res["rejected"])

	systemID := uuid.MustParse(sid)
	samples, err := e.Telemetry.Samples(ctx, systemID, now.Add(-time.Hour), now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Len(t, samples, 3)
	assert.Nil(t, samples[1].PacW)
	require.NotNil(t, samples[1].PdcW)
	assert.InDelta(t, 3100, *samples[1].PdcW, 1e-9)
	assert.Nil(t, samples[2].PacW)

	// a second batch adds to the counts
	device.do("POST", "/systems/"+sid+"/ingest/measurements", obj{"measurements": []obj{
		{"inverter": "1", "time": at(-4 * time.Minute), "pac_w": -1},
	}}, 200, &res)
	assert.EqualValues(t, 1, res["rejected"])

	var days []obj
	days = append(days, obj{"inverter": "1", "date": "2025-06-01", "energy_wh": 20000}, obj{"inverter": "1", "date": "2025-06-02", "energy_wh": 70000})
	device.do("POST", "/systems/"+sid+"/ingest/daily", obj{"days": days}, 200, &res)
	assert.EqualValues(t, 1, res["accepted"])
	assert.EqualValues(t, 1, res["rejected"])

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	rs, err := e.Telemetry.ListIngestRejections(ctx, systemID, today)
	require.NoError(t, err)
	got := map[string]int{}
	for _, r := range rs {
		got[r.Reason] += r.Count
	}
	if h := now.Hour(); h >= 1 && h < 23 { // all on one UTC day
		assert.Equal(t, map[string]int{"negative_power": 2, "power_above_limit": 1, "future": 1}, got)
	}
	rs, err = e.Telemetry.ListIngestRejections(ctx, systemID, time.Date(2025, 6, 2, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, rs, 1)
	assert.Equal(t, "daily_above_limit", rs[0].Reason)
	assert.EqualValues(t, 70000, rs[0].Sample["energy_wh"])
}
