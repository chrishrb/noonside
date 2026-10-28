package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearch(t *testing.T) {
	body, err := os.ReadFile("testdata/photon.json")
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/", r.URL.Path)
		assert.Equal(t, "Hauptstraße 1, Augsburg", r.URL.Query().Get("q"))
		assert.Equal(t, "de", r.URL.Query().Get("lang"))
		assert.Equal(t, "noonside-test", r.Header.Get("User-Agent"))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	places, err := NewClient(srv.URL+"/", "noonside-test").Search(context.Background(), "Hauptstraße 1, Augsburg", "de", 3)
	require.NoError(t, err)
	require.Len(t, places, 3)
	assert.Equal(t, "Hauptstraße 1, 86199 Augsburg, Deutschland", places[0].Label)
	assert.Equal(t, "house", places[0].Kind)
	assert.Equal(t, "DE", places[0].CountryCode)
	assert.InDelta(t, 48.319, places[0].Lat, 0.001)
	assert.InDelta(t, 10.822, places[0].Lon, 0.001)
	assert.Equal(t, "Maria Himmelfahrt, Hauptstraße 1, 86830 Schwabmünchen, Deutschland", places[2].Label)
}

func TestReverse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/reverse", r.URL.Path)
		assert.Equal(t, "48.3194", r.URL.Query().Get("lat"))
		assert.Equal(t, "10.8223", r.URL.Query().Get("lon"))
		assert.Equal(t, "en", r.URL.Query().Get("lang"))
		// Nearest first: a park, a shop (a house without a number), the street, a numbered building.
		_, _ = w.Write([]byte(`{"features":[
			{"properties":{"type":"other","name":"Stadtpark","city":"Augsburg","country":"Germany"},"geometry":{"coordinates":[10.8222,48.3193]}},
			{"properties":{"type":"house","name":"Bäckerei","street":"Hauptstraße","city":"Augsburg","country":"Germany"},"geometry":{"coordinates":[10.8222,48.3193]}},
			{"properties":{"type":"street","name":"Hauptstraße","postcode":"86199","city":"Augsburg","country":"Germany"},"geometry":{"coordinates":[10.8222,48.3193]}},
			{"properties":{"type":"house","name":"Rathaus","street":"Hauptstraße","housenumber":"1","postcode":"86199","city":"Augsburg","country":"Germany"},"geometry":{"coordinates":[10.8224,48.3195]}}
		]}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "noonside-test")
	p, err := c.Reverse(context.Background(), 48.3194, 10.8223, "en")
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "Hauptstraße 1, 86199 Augsburg, Germany", p.Label)
	assert.Equal(t, "house", p.Kind)
}

func TestReverseNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"features":[]}`)) }))
	defer srv.Close()
	p, err := NewClient(srv.URL, "x").Reverse(context.Background(), 0, 0, "")
	require.NoError(t, err)
	assert.Nil(t, p)
}

func TestLabel(t *testing.T) {
	f := func(typ, name, city, state, country string) feature {
		var x feature
		x.Properties.Type, x.Properties.Name, x.Properties.City, x.Properties.State, x.Properties.Country = typ, name, city, state, country
		return x
	}
	assert.Equal(t, "Augsburg, Deutschland", label(f("city", "Augsburg", "Augsburg", "Bayern", "Deutschland")))
	assert.Equal(t, "Oberbayern, Bayern, Deutschland", label(f("county", "Oberbayern", "", "Bayern", "Deutschland")))
	assert.Equal(t, "Bayern, Deutschland", label(f("state", "Bayern", "", "Bayern", "Deutschland")))
}

func TestSearchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer srv.Close()
	_, err := NewClient(srv.URL, "x").Search(context.Background(), "x", "fr", 1)
	assert.ErrorContains(t, err, "429")
}
