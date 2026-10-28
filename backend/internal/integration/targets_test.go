//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePVGIS answers 100 kWh per kWp and month, 400 for latitude 0 (at sea).
func fakePVGIS(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("lat") == "0" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"Location over the sea"}`))
			return
		}
		kwp, _ := strconv.ParseFloat(q.Get("peakpower"), 64)
		var months []map[string]any
		for m := 1; m <= 12; m++ {
			months = append(months, map[string]any{"month": m, "E_m": 100 * kwp})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"outputs": map[string]any{"monthly": map[string]any{"fixed": months}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEstimateTargets(t *testing.T) {
	e := newEnv(t)
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	viewerEmail := uniq("viewer")
	viewerID := e.createUser(viewerEmail, "viewer-password-1", false)
	viewer := e.login(viewerEmail, "viewer-password-1")

	var sys obj
	owner.do("POST", "/systems", obj{"name": "PVGIS PV", "peak_power_kwp": 8}, 201, &sys)
	sid := sys["id"].(string)
	path := "/systems/" + sid
	require.NoError(t, e.Systems.UpsertMember(t.Context(), uuid.MustParse(sid), viewerID, "viewer"))

	// Without coordinates there is nothing to estimate.
	owner.do("POST", path+"/targets/estimate", nil, 400, nil)

	// No planes: the system's kWp at the optimal orientation.
	owner.do("PATCH", path, obj{"latitude": 48.1, "longitude": 11.5}, 200, nil)
	var est obj
	owner.do("POST", path+"/targets/estimate", nil, 200, &est)
	assert.Equal(t, 800.0, est["target_kwh"].([]any)[0])

	// With planes, their kWp are summed; nothing is saved.
	owner.do("PATCH", path, obj{"planes": []obj{{"kwp": 5, "tilt_deg": 30, "azimuth_deg": -90}, {"kwp": 4, "tilt_deg": 30, "azimuth_deg": 90}}}, 200, nil)
	owner.do("POST", path+"/targets/estimate", nil, 200, &est)
	assert.Equal(t, 900.0, est["target_kwh"].([]any)[6])
	var saved obj
	owner.do("GET", path+"/targets", nil, 200, &saved)
	assert.Equal(t, 0.0, saved["target_kwh"].([]any)[0])

	// PVGIS without data for the location is a bad request.
	owner.do("PATCH", path, obj{"latitude": 0, "longitude": 0}, 200, nil)
	owner.do("POST", path+"/targets/estimate", nil, 400, nil)

	// Viewers may not change targets; strangers don't see the system.
	viewer.do("POST", path+"/targets/estimate", nil, 403, nil)
	e.anon().do("POST", path+"/targets/estimate", nil, 401, nil)
}
