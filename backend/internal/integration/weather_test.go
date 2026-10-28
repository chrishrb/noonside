//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOpenMeteo serves a clear-sky forecast for three days from today
// (in the requested time zone), counting requests.
func fakeOpenMeteo(calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		loc, err := time.LoadLocation(r.URL.Query().Get("timezone"))
		if err != nil {
			http.Error(w, `{"error":true,"reason":"bad timezone"}`, http.StatusBadRequest)
			return
		}
		if q := r.URL.Query(); q.Get("start_date") != "" {
			serveHistory(w, q, loc)
			return
		}
		now := time.Now().In(loc)
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		var m15t, h1t, dt, rise, set []int64
		var gti, temp, codes, cloud, prob, tmax, tmin, sun []float64
		for i := 0; i < 3*96; i++ {
			t := day.Add(time.Duration(i) * 15 * time.Minute)
			m15t = append(m15t, t.Unix())
			gti = append(gti, sunGTI(t))
			temp = append(temp, 15)
		}
		for i := 0; i < 3*24; i++ {
			h1t = append(h1t, day.Add(time.Duration(i)*time.Hour).Unix())
			codes, cloud, prob = append(codes, 1), append(cloud, 20), append(prob, 0)
		}
		for i := 0; i < 3; i++ {
			d := day.AddDate(0, 0, i)
			dt = append(dt, d.Unix())
			rise, set = append(rise, d.Add(7*time.Hour).Unix()), append(set, d.Add(19*time.Hour).Unix())
			tmax, tmin, sun = append(tmax, 20), append(tmin, 8), append(sun, 36000)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"current":     map[string]any{"time": now.Unix(), "temperature_2m": 14.5, "weather_code": 1, "cloud_cover": 20, "is_day": 1},
			"minutely_15": map[string]any{"time": m15t, "global_tilted_irradiance": gti, "temperature_2m": temp},
			"hourly":      map[string]any{"time": h1t, "weather_code": codes, "temperature_2m": temp[:len(h1t)], "cloud_cover": cloud, "precipitation_probability": prob},
			"daily": map[string]any{"time": dt, "weather_code": codes[:3], "temperature_2m_max": tmax, "temperature_2m_min": tmin,
				"sunrise": rise, "sunset": set, "sunshine_duration": sun},
		})
	}
}

// sunGTI is a clear-sky sine between 07:00 and 19:00 local, 800 W/m² at noon.
func sunGTI(t time.Time) float64 {
	hour := float64(t.Hour()) + float64(t.Minute())/60
	return math.Max(0, 800*math.Sin((hour-7)/12*math.Pi))
}

// serveHistory answers the historical forecast and archive APIs with sunGTI
// (sampled at the middle of each interval) for start_date..end_date.
func serveHistory(w http.ResponseWriter, q url.Values, loc *time.Location) {
	from, _ := time.ParseInLocation(time.DateOnly, q.Get("start_date"), loc)
	to, _ := time.ParseInLocation(time.DateOnly, q.Get("end_date"), loc)
	res, step := "hourly", time.Hour
	if q.Get("minutely_15") != "" {
		res, step = "minutely_15", 15*time.Minute
	}
	var ts []int64
	var gti, temp []float64
	for x := from; x.Before(to.AddDate(0, 0, 1)); x = x.Add(step) { // like the API: 00:00 .. 23:00/23:45
		ts = append(ts, x.Unix())
		gti, temp = append(gti, sunGTI(x.Add(-step/2))), append(temp, 15)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"latitude": 48.4, res: map[string]any{"time": ts, "global_tilted_irradiance": gti, "temperature_2m": temp}})
}

func TestWeather(t *testing.T) {
	e := newEnv(t)
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	strangerEmail := uniq("stranger")
	e.createUser(strangerEmail, "stranger-password-1", false)
	stranger := e.login(strangerEmail, "stranger-password-1")

	var sys obj
	owner.do("POST", "/systems", obj{"name": "Weather PV", "peak_power_kwp": 10, "timezone": "Europe/Berlin"}, 201, &sys)
	path := "/systems/" + sys["id"].(string)

	// Without coordinates there is no weather and no call.
	var wx obj
	owner.do("GET", path+"/weather", nil, 200, &wx)
	assert.Equal(t, false, wx["available"])
	assert.Zero(t, e.weatherCalls.Load())

	var updated obj
	owner.do("PATCH", path, obj{"latitude": 49.0, "longitude": 11.0,
		"planes": []obj{{"name": "South", "kwp": 10, "tilt_deg": 30, "azimuth_deg": -10}}}, 200, &updated)
	require.Len(t, updated["planes"], 1)
	plane := updated["planes"].([]any)[0].(map[string]any)
	assert.NotEmpty(t, plane["id"])
	delete(plane, "id")
	assert.Equal(t, map[string]any{"name": "South", "kwp": 10.0, "tilt_deg": 30.0, "azimuth_deg": -10.0}, plane)

	owner.do("GET", path+"/weather", nil, 200, &wx)
	require.Equal(t, true, wx["available"], wx)
	assert.Equal(t, true, wx["oriented"])
	days := wx["days"].([]any)
	require.Len(t, days, 3)
	today := days[0].(map[string]any)
	assert.Equal(t, time.Now().In(mustLoc(t)).Format(time.DateOnly), today["date"])
	// 10 kWp at a 12 h sine peaking at 800 W/m²: ~ 10·0.8·0.85·12·2/π ≈ 52 kWh, minus heat losses.
	assert.InDelta(t, 50, today["expected_kwh"], 6)
	assert.NotEmpty(t, wx["hourly"])
	assert.EqualValues(t, 1, e.weatherCalls.Load())

	// Cached for the next request; changing the orientation refetches.
	owner.do("GET", path+"/weather", nil, 200, nil)
	assert.EqualValues(t, 1, e.weatherCalls.Load())
	var cleared, wx2 obj
	// Leaving planes out keeps them; an empty list removes them.
	owner.do("PATCH", path, obj{"name": "Weather PV 2"}, 200, &cleared)
	assert.Len(t, cleared["planes"], 1)
	owner.do("PATCH", path, obj{"planes": []obj{}}, 200, &cleared)
	assert.Empty(t, cleared["planes"])
	owner.do("GET", path+"/weather", nil, 200, &wx2)
	assert.Equal(t, false, wx2["oriented"])
	assert.EqualValues(t, 2, e.weatherCalls.Load())

	owner.do("PATCH", path, obj{"planes": []obj{{"kwp": 5, "tilt_deg": 91, "azimuth_deg": 0}}}, 400, nil)
	stranger.do("GET", path+"/weather", nil, 404, nil)
}

func TestExpectedEnergy(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")

	loc := mustLoc(t)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	commissioned := today.AddDate(0, 0, -40)
	var sys obj
	owner.do("POST", "/systems", obj{"name": "Expected PV", "peak_power_kwp": 10, "latitude": 48.4, "longitude": 10.8,
		"planes": []obj{{"kwp": 10, "tilt_deg": 30, "azimuth_deg": 0}}, "commissioned_at": commissioned.Format(time.DateOnly)}, 201, &sys)
	path := "/systems/" + sys["id"].(string)
	var inv obj
	owner.do("POST", path+"/inverters", obj{"external_ref": "1", "name": "WR 1", "peak_power_kwp": 10}, 201, &inv)

	// Without the backfill, past days have no expectation (and no targets).
	var month obj
	pastDay := today.AddDate(0, 0, -20)
	past := pastDay.Format(time.DateOnly)
	pastTarget := func(m obj) float64 { return m["target"].([]any)[pastDay.Day()-1].(float64) }
	owner.do("GET", path+"/overview?period=month&date="+past, nil, 200, &month)
	assert.Zero(t, pastTarget(month))

	_, err := e.db.Pool().Exec(ctx, `DELETE FROM job_leases WHERE name = 'expected_daily'`)
	require.NoError(t, err)
	require.NoError(t, e.Weather.Backfill().Run(ctx, time.Now().Add(time.Minute)))
	key, first, last, err := e.Weather.ExpectedDailyRange(ctx, uuid.MustParse(sys["id"].(string)))
	require.NoError(t, err)
	assert.NotEmpty(t, key)
	require.NotNil(t, first)
	assert.Equal(t, commissioned.Format(time.DateOnly), first.Format(time.DateOnly))
	assert.Equal(t, today.AddDate(0, 0, -1).Format(time.DateOnly), last.Format(time.DateOnly))

	// Month bars and the summary use the weather-based expectation:
	// a 12 h sine peaking at 800 W/m² on 10 kWp ≈ 50 kWh a day.
	month = obj{}
	owner.do("GET", path+"/overview?period=month&date="+past, nil, 200, &month)
	sum := month["summary"].(map[string]any)
	assert.Equal(t, "weather", sum["target_source"])
	day := pastTarget(month)
	assert.InDelta(t, 50, day, 6)

	// The performance ratio: 45 kWh a day from the sine's 800·12·2/π Wh/m²
	// (≈ 6.11 kWh/m²) on 10 kWp ≈ 74 %, for the days with energy only.
	day20 := time.Date(pastDay.Year(), pastDay.Month(), pastDay.Day(), 0, 0, 0, 0, time.UTC)
	require.NoError(t, e.Telemetry.UpsertDailyEnergy(ctx, []telemetry.DailyEnergy{
		{InverterID: uuid.MustParse(inv["id"].(string)), Day: day20, EnergyWh: 45000},
		{InverterID: uuid.MustParse(inv["id"].(string)), Day: day20.AddDate(0, 0, 1), EnergyWh: 45000},
	}))
	irr := 800 * 12 * 2 / math.Pi / 1000
	month = obj{}
	owner.do("GET", path+"/overview?period=month&date="+past, nil, 200, &month)
	sum = month["summary"].(map[string]any)
	assert.InDelta(t, 45/(irr*10)*100, sum["performance_ratio_pct"], 1)
	assert.InDelta(t, 2*irr, sum["irradiation_kwh_m2"], 0.2)
	prs := month["performance_ratio"].([]any)
	assert.InDelta(t, 45/(irr*10)*100, prs[pastDay.Day()-1], 1)
	var year obj
	owner.do("GET", path+"/overview?period=year&date="+past, nil, 200, &year)
	assert.InDelta(t, 45/(irr*10)*100, year["summary"].(map[string]any)["performance_ratio_pct"], 1)

	// A past day has its expected curve (15-minute points) and daily expectation.
	var d obj
	owner.do("GET", path+"/overview?period=day&date="+past, nil, 200, &d)
	assert.Len(t, d["expected"], 96)
	assert.InDelta(t, day, d["summary"].(map[string]any)["target_kwh"], 1)
	assert.InDelta(t, 45/(irr*10)*100, d["summary"].(map[string]any)["performance_ratio_pct"], 1)

	// Today's curve comes from the forecast.
	d = obj{}
	owner.do("GET", path+"/overview?period=day", nil, 200, &d)
	assert.NotEmpty(t, d["expected"])
	assert.Equal(t, "weather", d["summary"].(map[string]any)["target_source"])

	// Changing the orientation invalidates the stored days.
	owner.do("PATCH", path, obj{"planes": []obj{{"kwp": 10, "tilt_deg": 30, "azimuth_deg": 90}}}, 200, nil)
	month = obj{}
	owner.do("GET", path+"/overview?period=month&date="+past, nil, 200, &month)
	assert.Zero(t, pastTarget(month))
}
