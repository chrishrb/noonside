//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/systems/geocode"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePhoton knows two places; every other query finds nothing. Reverse
// lookups find the house near its coordinates.
func fakePhoton() http.HandlerFunc {
	type place struct {
		props    map[string]string
		lon, lat float64
	}
	known := map[string]place{
		"neusäß": {map[string]string{"type": "city", "name": "Neusäß", "city": "Neusäß", "state": "Bayern", "country": "Deutschland", "countrycode": "DE"}, 10.83, 48.40},
		"hauptstraße 1, augsburg": {map[string]string{"type": "house", "street": "Hauptstraße", "housenumber": "1", "postcode": "86199", "city": "Augsburg",
			"country": "Deutschland", "countrycode": "DE"}, 10.822, 48.319},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		features := []any{}
		if r.URL.Path == "/reverse" {
			if r.URL.Query().Get("lat") == "48.3191" {
				p := known["hauptstraße 1, augsburg"]
				features = append(features, map[string]any{"properties": p.props, "geometry": map[string]any{"coordinates": []float64{p.lon, p.lat}}})
			}
		} else if p, ok := known[strings.ToLower(r.URL.Query().Get("q"))]; ok {
			features = append(features, map[string]any{"properties": p.props, "geometry": map[string]any{"coordinates": []float64{p.lon, p.lat}}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "FeatureCollection", "features": features})
	}
}

func TestGeocodeAndAddressValidation(t *testing.T) {
	e := newEnv(t)
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")

	var res obj
	owner.do("GET", "/geocode?q=Hauptstra%C3%9Fe+1,+Augsburg&lang=de", nil, 200, &res)
	assert.Equal(t, true, res["available"])
	places := res["places"].([]any)
	require.Len(t, places, 1)
	p := places[0].(map[string]any)
	assert.Equal(t, "Hauptstraße 1, 86199 Augsburg, Deutschland", p["label"])
	assert.Equal(t, "house", p["kind"])
	owner.do("GET", "/geocode?q=ab", nil, 400, nil)
	e.anon().do("GET", "/geocode?q=Neus%C3%A4%C3%9F", nil, 401, nil)

	// Reverse: the address nearest to a pin, or none.
	res = obj{}
	owner.do("GET", "/geocode/reverse?lat=48.3191&lon=10.8221&lang=de", nil, 200, &res)
	places = res["places"].([]any)
	require.Len(t, places, 1)
	assert.Equal(t, "Hauptstraße 1, 86199 Augsburg, Deutschland", places[0].(map[string]any)["label"])
	res = obj{}
	owner.do("GET", "/geocode/reverse?lat=10&lon=10", nil, 200, &res)
	assert.Equal(t, true, res["available"])
	assert.Empty(t, res["places"])
	owner.do("GET", "/geocode/reverse?lat=91&lon=10", nil, 400, nil)
	e.anon().do("GET", "/geocode/reverse?lat=10&lon=10", nil, 401, nil)

	// Creating with an address sets the coordinates.
	var sys obj
	owner.do("POST", "/systems", obj{"name": "Geo PV", "location": "Hauptstraße 1, Augsburg"}, 201, &sys)
	assert.InDelta(t, 48.319, sys["latitude"], 0.001)
	assert.InDelta(t, 10.822, sys["longitude"], 0.001)
	path := "/systems/" + sys["id"].(string)

	// An unknown address is rejected.
	status, raw := owner.raw("PATCH", path, obj{"location": "Nowhere Street 999"})
	assert.Equal(t, 400, status)
	assert.Contains(t, string(raw), "location not found")

	// Explicit coordinates win over the lookup; unchanged addresses are not looked up.
	var got obj
	owner.do("PATCH", path, obj{"location": "Somewhere picked", "latitude": 50.0, "longitude": 8.0}, 200, &got)
	assert.Equal(t, 50.0, got["latitude"])
	got = obj{}
	owner.do("PATCH", path, obj{"location": "Somewhere picked", "name": "Geo PV 2"}, 200, &got)
	assert.Equal(t, 50.0, got["latitude"])

	// A new address moves the system; an empty one clears the coordinates.
	got = obj{}
	owner.do("PATCH", path, obj{"location": " Neusäß "}, 200, &got)
	assert.Equal(t, "Neusäß", got["location"])
	assert.Equal(t, 48.40, got["latitude"])
	got = obj{}
	owner.do("PATCH", path, obj{"location": ""}, 200, &got)
	assert.NotContains(t, got, "latitude")
}

func TestGeocodeBackfill(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	photon := httptest.NewServer(fakePhoton())
	defer photon.Close()

	sys, err := e.Systems.CreateSystem(ctx, &systems.System{Name: "Backfill PV", Location: "Neusäß", Timezone: "Europe/Berlin", Currency: "EUR"})
	require.NoError(t, err)
	unknown, err := e.Systems.CreateSystem(ctx, &systems.System{Name: "Backfill PV 2", Location: "Nowhere " + uuid.NewString(), Timezone: "Europe/Berlin", Currency: "EUR"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = e.Systems.DeleteSystem(ctx, sys.ID)
		_ = e.Systems.DeleteSystem(ctx, unknown.ID)
	})
	// Other systems in the shared database may need lookups too; the fake
	// finds none of them, and the lease must be free for this run.
	_, err = e.db.Pool().Exec(ctx, `DELETE FROM job_leases WHERE name = 'geocode_backfill'`)
	require.NoError(t, err)

	require.NoError(t, e.Systems.GeocodeBackfill(geocode.NewClient(photon.URL, "noonside-test"), 0).Run(ctx, time.Now().Add(10*time.Minute)))
	got, err := e.Systems.GetSystem(ctx, sys.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Latitude)
	assert.Equal(t, 48.40, *got.Latitude)
	got, err = e.Systems.GetSystem(ctx, unknown.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Latitude)
}
