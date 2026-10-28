package weather

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hourlyServer serves 200 W/m² at 15 °C for every hour of the requested
// days, with the last hour missing when gap is set.
func hourlyServer(t *testing.T, gap bool, paths *[]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		*paths = append(*paths, r.URL.Path+" "+q.Get("start_date")+".."+q.Get("end_date"))
		loc, err := time.LoadLocation(q.Get("timezone"))
		require.NoError(t, err)
		from, _ := time.ParseInLocation(time.DateOnly, q.Get("start_date"), loc)
		to, _ := time.ParseInLocation(time.DateOnly, q.Get("end_date"), loc)
		res, step := "hourly", time.Hour
		if q.Get("minutely_15") != "" {
			res, step = "minutely_15", 15*time.Minute
		}
		var ts []int64
		var gti, temp []*float64
		v, c := 200.0, 15.0
		for x := from; x.Before(to.AddDate(0, 0, 1)); x = x.Add(step) { // like the API: 00:00 .. 23:00/23:45
			ts = append(ts, x.Unix())
			g := &v
			if gap && x.Equal(to) {
				g = nil // the last interval of the day before end_date
			}
			gti, temp = append(gti, g), append(temp, &c)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"latitude": 48.4, "utc_offset_seconds": 3600, res: map[string]any{"time": ts, "global_tilted_irradiance": gti, "temperature_2m": temp}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHistorySplitsAtHistoricalFrom(t *testing.T) {
	var paths []string
	srv := hourlyServer(t, false, &paths)
	c := NewClient(Config{HistoricalURL: srv.URL + "/hist", ArchiveURL: srv.URL + "/era5"})
	loc := berlin(t)
	from := time.Date(2021, 12, 30, 0, 0, 0, 0, loc)
	slots, err := c.History(context.Background(), Request{Lat: 48, Lon: 10, Loc: loc}, from, time.Date(2022, 1, 2, 0, 0, 0, 0, loc), false)
	require.NoError(t, err)
	assert.Equal(t, []string{"/era5/v1/archive 2021-12-30..2022-01-01", "/hist/v1/forecast 2022-01-01..2022-01-03"}, paths)
	assert.Len(t, slots, 4*24)
	assert.Equal(t, time.Hour, slots[0].Len())

	days := Daily(slots, loc)
	require.Len(t, days, 4)
	// 24 h at 200 W/m², 15 °C air (cell 20 °C): 0.2 · 0.85 · 1.02 · 24 kWh/kWp
	// from 0.2 · 24 kWh/m².
	assert.InDelta(t, 0.2*0.85*1.02*24, days["2022-01-01"].KWhPerKWp, 0.001)
	assert.InDelta(t, 0.2*24, days["2022-01-01"].IrradiationKWhM2, 0.001)
}

func TestDailySkipsIncompleteDays(t *testing.T) {
	var paths []string
	srv := hourlyServer(t, true, &paths)
	loc := berlin(t)
	// Spring DST day (23 h) is complete; the last day misses its last hour.
	from := time.Date(2024, 3, 31, 0, 0, 0, 0, loc)
	slots, err := NewClient(Config{HistoricalURL: srv.URL}).History(context.Background(), Request{Loc: loc}, from, from.AddDate(0, 0, 1), true)
	require.NoError(t, err)
	assert.Equal(t, 15*time.Minute, slots[0].Len())
	days := Daily(slots, loc)
	assert.Contains(t, days, "2024-03-31")
	assert.NotContains(t, days, "2024-04-01")
	assert.InDelta(t, 0.2*0.85*1.02*23, days["2024-03-31"].KWhPerKWp, 0.001)
}

func TestIrradiationCountsPartialSlots(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	slots := []Slot{{End: t0.Add(SlotLen), GTI: 800}, {End: t0.Add(2 * SlotLen), GTI: 400}}
	// All of the first slot (0.2 kWh/m²) and a third of the second (0.1/3).
	assert.InDelta(t, 0.2+0.1/3, IrradiationKWhM2(slots, t0, t0.Add(20*time.Minute)), 1e-9)
}
