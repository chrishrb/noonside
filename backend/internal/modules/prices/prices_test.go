package prices

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAwattar(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/marketdata", r.URL.Path)
		got = r.URL.Query()
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"start_timestamp":1759269600000,"end_timestamp":1759273200000,"marketprice":-5.2,"unit":"Eur/MWh"},
			{"start_timestamp":1759266000000,"end_timestamp":1759269600000,"marketprice":87.41,"unit":"Eur/MWh"}
		],"url":"/de/v1/marketdata"}`))
	}))
	defer srv.Close()

	a := NewAwattar(srv.URL+"/", "")
	from, to := utc("2025-09-30T22:00:00Z"), utc("2025-10-01T22:00:00Z")
	ps, err := a.Fetch(context.Background(), ZoneDELU, from, to)
	require.NoError(t, err)
	assert.Equal(t, "1759269600000", got.Get("start"))
	assert.Equal(t, "1759356000000", got.Get("end"))
	require.Len(t, ps, 2)
	assert.Equal(t, MarketPrice{Zone: "DE-LU", Start: utc("2025-09-30T21:00:00Z"), End: utc("2025-09-30T22:00:00Z"), EURMWh: 87.41}, ps[0], "sorted")
	assert.Equal(t, -5.2, ps[1].EURMWh)

	_, err = a.Fetch(context.Background(), ZoneAT, from, to)
	assert.ErrorContains(t, err, "no prices for zone AT")
}

const entsoeDoc15 = `<?xml version="1.0" encoding="utf-8"?>
<Publication_MarketDocument xmlns="urn:iec62325.351:tc57wg16:451-3:publicationdocument:7:3">
  <TimeSeries>
    <mRID>1</mRID>
    <Period>
      <timeInterval><start>2025-10-01T22:00Z</start><end>2025-10-01T23:00Z</end></timeInterval>
      <resolution>PT15M</resolution>
      <Point><position>1</position><price.amount>90.5</price.amount></Point>
      <Point><position>3</position><price.amount>-1.25</price.amount></Point>
    </Period>
  </TimeSeries>
  <TimeSeries>
    <mRID>2</mRID>
    <Period>
      <timeInterval><start>2025-10-01T22:00Z</start><end>2025-10-01T23:00Z</end></timeInterval>
      <resolution>PT60M</resolution>
      <Point><position>1</position><price.amount>60</price.amount></Point>
    </Period>
  </TimeSeries>
</Publication_MarketDocument>`

func TestEntsoE(t *testing.T) {
	var got url.Values
	body := entsoeDoc15
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	e := NewEntsoE(srv.URL, "tok")
	ps, err := e.Fetch(context.Background(), ZoneDELU, utc("2025-10-01T22:00:00Z"), utc("2025-10-01T23:00:00Z"))
	require.NoError(t, err)
	assert.Equal(t, "A44", got.Get("documentType"))
	assert.Equal(t, "10Y1001A1001A82H", got.Get("in_Domain"))
	assert.Equal(t, "202510012200", got.Get("periodStart"))
	assert.Equal(t, "tok", got.Get("securityToken"))

	// A03: position 2 repeats position 1, position 4 repeats 3; the hourly
	// series overlapping the 15-minute one is dropped.
	require.Len(t, ps, 4)
	for i, want := range []float64{90.5, 90.5, -1.25, -1.25} {
		assert.Equal(t, want, ps[i].EURMWh, i)
		assert.Equal(t, utc("2025-10-01T22:00:00Z").Add(time.Duration(i)*15*time.Minute), ps[i].Start, i)
		assert.Equal(t, 15*time.Minute, ps[i].End.Sub(ps[i].Start))
	}

	body = `<Acknowledgement_MarketDocument><Reason><code>999</code><text>No matching data found for Data item ...</text></Reason></Acknowledgement_MarketDocument>`
	ps, err = e.Fetch(context.Background(), ZoneAT, utc("2025-10-01T22:00:00Z"), utc("2025-10-01T23:00:00Z"))
	require.NoError(t, err)
	assert.Empty(t, ps)

	body = `<Acknowledgement_MarketDocument><Reason><code>999</code><text>Invalid token</text></Reason></Acknowledgement_MarketDocument>`
	_, err = e.Fetch(context.Background(), ZoneAT, utc("2025-10-01T22:00:00Z"), utc("2025-10-01T23:00:00Z"))
	assert.ErrorContains(t, err, "Invalid token")
}

func TestNegativeAndClip(t *testing.T) {
	h := func(hour int, p float64) MarketPrice {
		s := utc("2026-05-01T00:00:00Z").Add(time.Duration(hour) * time.Hour)
		return MarketPrice{Start: s, End: s.Add(time.Hour), EURMWh: p}
	}
	ps := []MarketPrice{h(10, 5), h(11, -1), h(12, -3), h(13, 0), h(14, -2)}
	neg := Negative(ps)
	require.Len(t, neg, 2)
	assert.Equal(t, h(11, 0).Start, neg[0].Start)
	assert.Equal(t, h(12, 0).End, neg[0].End, "adjacent intervals merge")
	assert.Equal(t, -3.0, neg[0].EURMWh, "with the lowest price")
	assert.Equal(t, 3.0, Hours(neg))

	c := Clip(ps, h(11, 0).Start.Add(30*time.Minute), h(12, 0).End)
	require.Len(t, c, 2)
	assert.Equal(t, 30*time.Minute, c[0].End.Sub(c[0].Start))
}

type fakeStore struct {
	systems []systems.SystemWithRole
	prices  []MarketPrice
	locks   int
}

func (f *fakeStore) ListSystems(context.Context, *uuid.UUID) ([]systems.SystemWithRole, error) {
	return f.systems, nil
}

func (f *fakeStore) TryLockJob(context.Context, string, time.Duration, time.Duration) (func(), bool, error) {
	f.locks++
	return func() {}, true, nil
}

func (f *fakeStore) UpsertMarketPrices(_ context.Context, ps []MarketPrice) error {
	f.prices = normalize(append(f.prices, ps...))
	return nil
}

func (f *fakeStore) MarketPrices(context.Context, string, time.Time, time.Time) ([]MarketPrice, error) {
	return f.prices, nil
}

func (f *fakeStore) NegativeMarketPrices(context.Context, string, time.Time, time.Time) ([]MarketPrice, bool, error) {
	return Negative(f.prices), len(f.prices) > 0, nil
}

func (f *fakeStore) MarketPriceRange(context.Context, string) (*time.Time, *time.Time, error) {
	if len(f.prices) == 0 {
		return nil, nil, nil
	}
	return &f.prices[0].Start, &f.prices[len(f.prices)-1].End, nil
}

// hourly returns hourly prices for [from, to).
type hourly struct{ calls [][2]time.Time }

func (h *hourly) Name() string { return "test" }

func (h *hourly) Fetch(_ context.Context, zone string, from, to time.Time) ([]MarketPrice, error) {
	h.calls = append(h.calls, [2]time.Time{from, to})
	var out []MarketPrice
	for t := from; t.Before(to); t = t.Add(time.Hour) {
		out = append(out, MarketPrice{Zone: zone, Start: t, End: t.Add(time.Hour), EURMWh: 50})
	}
	return out, nil
}

func TestJob(t *testing.T) {
	zone := ZoneDELU
	st := &fakeStore{systems: []systems.SystemWithRole{
		{System: systems.System{PriceZone: &zone}}, {System: systems.System{}}, {System: systems.System{PriceZone: &zone}},
	}}
	src := &hourly{}
	j := NewJob(st, src)
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, zoneLoc)
	j.SetClock(func() time.Time { return now })
	require.NoError(t, j.Run(context.Background(), now.Add(time.Minute)))

	today := time.Date(2026, 3, 10, 0, 0, 0, 0, zoneLoc)
	require.Len(t, src.calls, 2, "one zone: recent days, then one chunk of history")
	assert.Equal(t, [2]time.Time{today.AddDate(0, 0, -backfillChunk), today.AddDate(0, 0, 2)}, src.calls[0])
	assert.Equal(t, [2]time.Time{today.AddDate(0, 0, -2*backfillChunk), today.AddDate(0, 0, -backfillChunk)}, src.calls[1])

	// Tomorrow is complete: only the backfill continues.
	src.calls = nil
	require.NoError(t, j.RunZone(context.Background(), zone))
	require.Len(t, src.calls, 1)
	assert.True(t, src.calls[0][1].Equal(today.AddDate(0, 0, -2*backfillChunk)))

	// The backfill stops at BackfillFrom.
	for range 40 {
		require.NoError(t, j.RunZone(context.Background(), zone))
	}
	src.calls = nil
	require.NoError(t, j.RunZone(context.Background(), zone))
	assert.Empty(t, src.calls)
	assert.True(t, st.prices[0].Start.Equal(BackfillFrom))

	// The next day, tomorrow is fetched from where the prices end.
	now = now.AddDate(0, 0, 1)
	require.NoError(t, j.RunZone(context.Background(), zone))
	require.Len(t, src.calls, 1)
	assert.Equal(t, [2]time.Time{today.AddDate(0, 0, 2), today.AddDate(0, 0, 3)}, src.calls[0])

	assert.False(t, NewJob(st, nil).Enabled())
	assert.NoError(t, NewJob(st, nil).Run(context.Background(), now))
}
