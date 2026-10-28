package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func berlin(t *testing.T) *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	return loc
}

func fakeServer(t *testing.T, calls *atomic.Int32) *httptest.Server {
	body, err := os.ReadFile("testdata/forecast.json")
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/v1/forecast", r.URL.Path)
		q := r.URL.Query()
		assert.Equal(t, "30", q.Get("tilt"))
		assert.Equal(t, "-10", q.Get("azimuth"))
		assert.Equal(t, "Europe/Berlin", q.Get("timezone"))
		assert.Equal(t, "unixtime", q.Get("timeformat"))
		assert.Equal(t, "key", q.Get("apikey"))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchParses(t *testing.T) {
	var calls atomic.Int32
	srv := fakeServer(t, &calls)
	f, err := NewClient(Config{ForecastURL: srv.URL + "/", APIKey: "key"}).Fetch(context.Background(), Request{Lat: 49, Lon: 11, Planes: []Plane{{Tilt: 30, Azimuth: -10, Weight: 1}}, Loc: berlin(t)})
	require.NoError(t, err)
	assert.Len(t, f.Days, Days)
	assert.Equal(t, "2026-09-29", f.Days[0].Date)
	assert.Len(t, f.Slots, Days*96)
	assert.Len(t, f.Hours, Days*24)
	assert.Greater(t, f.Days[0].SunshineH, 0.0)
	assert.True(t, f.Days[0].Sunset.After(f.Days[0].Sunrise))
	// Expected energy of a 10 kWp system on a sunny autumn day.
	day := time.Date(2026, 9, 29, 0, 0, 0, 0, berlin(t))
	kwh := f.ExpectedKWh(day, day.AddDate(0, 0, 1), 10)
	assert.Greater(t, kwh, 5.0)
	assert.Less(t, kwh, 80.0)
}

func TestFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":true,"reason":"Latitude must be in range"}`))
	}))
	defer srv.Close()
	_, err := NewClient(Config{ForecastURL: srv.URL}).Fetch(context.Background(), Request{Lat: 99, Loc: time.UTC})
	assert.ErrorContains(t, err, "Latitude must be in range")
}

func TestExpectedPower(t *testing.T) {
	// STC-like: 1000 W/m², cell at 25 °C → kWp · PR.
	assert.InDelta(t, 8500, ExpectedPower(1000, 0, 10), 1)
	assert.Less(t, ExpectedPower(1000, 30, 10), ExpectedPower(1000, 0, 10)) // hot cells lose power
	assert.Zero(t, ExpectedPower(0, 20, 10))
	assert.Zero(t, ExpectedPower(500, 20, 0))
}

func TestExpectedAtInterpolates(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	f := &Forecast{Slots: []Slot{
		{End: t0.Add(15 * time.Minute), GTI: 0},
		{End: t0.Add(30 * time.Minute), GTI: 800},
	}}
	// Midpoints are 10:07:30 (0 W) and 10:22:30.
	w, ok := f.ExpectedAt(t0.Add(15*time.Minute), 10)
	require.True(t, ok)
	assert.InDelta(t, ExpectedPower(800, 0, 10)/2, w, 1)
	w, ok = f.ExpectedAt(t0.Add(25*time.Minute), 10)
	require.True(t, ok)
	assert.InDelta(t, ExpectedPower(800, 0, 10), w, 1)
	_, ok = f.ExpectedAt(t0.Add(-time.Minute), 10)
	assert.False(t, ok)
	_, ok = f.ExpectedAt(t0.Add(30*time.Minute), 10)
	assert.False(t, ok)
	assert.InDelta(t, ExpectedPower(800, 0, 10)/4/1000, f.ExpectedKWh(t0, t0.Add(time.Hour), 10), 0.001)
}

type memStore struct {
	Store
	row *WeatherCache
}

func (m *memStore) GetWeather(context.Context, uuid.UUID) (*WeatherCache, error) {
	if m.row == nil {
		return nil, db.ErrNotFound
	}
	return m.row, nil
}

func (m *memStore) PutWeather(_ context.Context, w *WeatherCache) error { m.row = w; return nil }

func TestServiceCaches(t *testing.T) {
	var calls atomic.Int32
	srv := fakeServer(t, &calls)
	st := &memStore{}
	svc := newService(NewClient(Config{ForecastURL: srv.URL, APIKey: "key"}), st)
	now := time.Now()
	svc.now = func() time.Time { return now }
	lat, lon := 49.0, 11.0
	sys := &systems.System{ID: uuid.New(), Latitude: &lat, Longitude: &lon, Planes: []systems.Plane{{KWp: 10, TiltDeg: 30, AzimuthDeg: -10}}, Timezone: "Europe/Berlin"}
	ctx := context.Background()

	f, err := svc.Get(ctx, sys)
	require.NoError(t, err)
	require.NotNil(t, f)
	_, err = svc.Get(ctx, sys)
	require.NoError(t, err)
	assert.EqualValues(t, 1, calls.Load(), "fresh cache is reused")

	now = now.Add(TTL)
	_, _ = svc.Get(ctx, sys)
	assert.EqualValues(t, 2, calls.Load(), "stale cache is refreshed")

	// A failed refresh returns the stale forecast.
	srv.Close()
	now = now.Add(TTL)
	f, err = svc.Get(ctx, sys)
	require.NoError(t, err)
	assert.NotNil(t, f)

	// No coordinates, or disabled: no forecast.
	f, err = svc.Get(ctx, &systems.System{ID: uuid.New(), Timezone: "UTC"})
	require.NoError(t, err)
	assert.Nil(t, f)
	f, err = (*Service)(nil).Get(ctx, sys)
	require.NoError(t, err)
	assert.Nil(t, f)
}

func TestRequestForWeightsPlanes(t *testing.T) {
	lat, lon := 49.0, 11.0
	sys := &systems.System{Latitude: &lat, Longitude: &lon, Timezone: "UTC",
		Planes: []systems.Plane{{KWp: 6, TiltDeg: 30, AzimuthDeg: -90}, {KWp: 2, TiltDeg: 30, AzimuthDeg: 90}}}
	r, ok := RequestFor(sys)
	require.True(t, ok)
	assert.Equal(t, []Plane{{30, -90, 0.75}, {30, 90, 0.25}}, r.Planes)
	// Without kWp the planes count equally; a changed plane changes the key.
	sys.Planes[0].KWp, sys.Planes[1].KWp = 0, 0
	r2, _ := RequestFor(sys)
	assert.Equal(t, 0.5, r2.Planes[0].Weight)
	assert.NotEqual(t, r.Key(), r2.Key())
	// No planes: one horizontal plane.
	r3, _ := RequestFor(&systems.System{Latitude: &lat, Longitude: &lon, Timezone: "UTC"})
	assert.Equal(t, []Plane{{Weight: 1}}, r3.planes())
}

func TestFetchCombinesPlanes(t *testing.T) {
	body, err := os.ReadFile("testdata/forecast.json")
	require.NoError(t, err)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("azimuth") == "90" {
			// The west plane only asks for its irradiance.
			assert.Equal(t, "global_tilted_irradiance", r.URL.Query().Get("minutely_15"))
			assert.Empty(t, r.URL.Query().Get("daily"))
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(Config{ForecastURL: srv.URL})
	one, err := c.Fetch(context.Background(), Request{Lat: 49, Lon: 11, Planes: []Plane{{Tilt: 30, Azimuth: -90, Weight: 1}}, Loc: berlin(t)})
	require.NoError(t, err)
	two, err := c.Fetch(context.Background(), Request{Lat: 49, Lon: 11, Planes: []Plane{{30, -90, 0.5}, {30, 90, 0.5}}, Loc: berlin(t)})
	require.NoError(t, err)
	assert.EqualValues(t, 3, calls.Load())
	// The same irradiance on both planes: the weighted sum equals one plane.
	require.Len(t, two.Slots, len(one.Slots))
	for i := range one.Slots {
		assert.InDelta(t, one.Slots[i].GTI, two.Slots[i].GTI, 1e-9)
	}
}

func TestCombineDropsMissingSlots(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	base := []Slot{{End: t0, GTI: 100}, {End: t0.Add(SlotLen), GTI: 200}}
	other := []Slot{{End: t0, GTI: 300}}
	out := combine(base, []Plane{{Weight: 0.5}, {Weight: 0.5}}, [][]Slot{other})
	require.Len(t, out, 1)
	assert.Equal(t, 200.0, out[0].GTI)
}
