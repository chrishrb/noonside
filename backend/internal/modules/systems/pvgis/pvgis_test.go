package pvgis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fake answers 100 kWh per kWp and month, recording the queries.
func fake(t *testing.T, queries *[]map[string]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/PVcalc", r.URL.Path)
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		*queries = append(*queries, q)
		kwp, _ := strconv.ParseFloat(q["peakpower"], 64)
		var months []map[string]any
		for m := 1; m <= 12; m++ {
			months = append(months, map[string]any{"month": m, "E_m": 100 * kwp})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"outputs": map[string]any{"monthly": map[string]any{"fixed": months}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMonthlySumsPlanes(t *testing.T) {
	var qs []map[string]string
	c := NewClient(fake(t, &qs).URL + "/api/")
	m, err := c.Monthly(context.Background(), 48.1, 11.5, []Plane{{KWp: 6, Tilt: 30, Azimuth: -90}, {KWp: 4, Tilt: 30, Azimuth: 90}, {KWp: 0}}, 10)
	require.NoError(t, err)
	assert.Equal(t, 1000.0, m[0])
	require.Len(t, qs, 2, "planes without kWp are skipped")
	assert.Equal(t, map[string]string{"lat": "48.1", "lon": "11.5", "peakpower": "6", "loss": "14", "angle": "30", "aspect": "-90", "outputformat": "json"}, qs[0])
}

func TestMonthlyOptimalWithoutPlanes(t *testing.T) {
	var qs []map[string]string
	m, err := NewClient(fake(t, &qs).URL+"/api").Monthly(context.Background(), 48, 11, nil, 8)
	require.NoError(t, err)
	assert.Equal(t, 800.0, m[11])
	assert.Equal(t, "1", qs[0]["optimalangles"])
}

func TestMonthlyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Location over the sea"}`))
	}))
	t.Cleanup(srv.Close)
	_, err := NewClient(srv.URL).Monthly(context.Background(), 0, 0, nil, 1)
	assert.ErrorContains(t, err, "Location over the sea")
}
