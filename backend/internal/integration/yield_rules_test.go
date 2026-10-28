//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/alerting"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInverterRatedPower(t *testing.T) {
	e := newEnv(t)
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")
	var sys, inv obj
	owner.do("POST", "/systems", obj{"name": "Rated PV", "peak_power_kwp": 10}, 201, &sys)
	path := "/systems/" + sys["id"].(string) + "/inverters"
	owner.do("POST", path, obj{"external_ref": "1", "name": "WR 1", "max_ac_power_w": 8000}, 201, &inv)
	assert.Equal(t, 8000.0, inv["max_ac_power_w"])
	var cleared obj
	owner.do("PATCH", path+"/"+inv["id"].(string), obj{"max_ac_power_w": nil}, 200, &cleared)
	assert.NotContains(t, cleared, "max_ac_power_w")
	owner.do("PATCH", path+"/"+inv["id"].(string), obj{"max_ac_power_w": 0}, 400, nil)
}

func TestLowYieldAlert(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	email := uniq("owner")
	e.createUser(email, "owner-password-1", true)
	owner := e.login(email, "owner-password-1")

	loc := mustLoc(t)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	var sys, inv obj
	owner.do("POST", "/systems", obj{"name": "Low PV", "peak_power_kwp": 10, "latitude": 48.4, "longitude": 10.8,
		"commissioned_at": today.AddDate(0, 0, -5).Format(time.DateOnly)}, 201, &sys)
	sid := uuid.MustParse(sys["id"].(string))
	owner.do("POST", "/systems/"+sid.String()+"/inverters", obj{"external_ref": "1", "name": "WR 1", "peak_power_kwp": 10}, 201, &inv)
	// Yesterday: 10 kWh where the weather allowed ≈ 50.
	y := today.AddDate(0, 0, -1)
	require.NoError(t, e.Telemetry.UpsertDailyEnergy(ctx, []telemetry.DailyEnergy{{
		InverterID: uuid.MustParse(inv["id"].(string)), Day: time.Date(y.Year(), y.Month(), y.Day(), 0, 0, 0, 0, time.UTC), EnergyWh: 10000,
	}}))
	engine := alerting.NewEngine(e.Alerting.Store()).WithExpected(e.Weather.ExpectedDays)
	// Rules about past days are held while the sun is down, so evaluate at noon.
	noon := today.Add(12 * time.Hour)
	engine.SetClock(func() time.Time { return noon })
	got, err := e.Systems.GetSystem(ctx, sid)
	require.NoError(t, err)

	// Before the backfill has yesterday's weather the rule can't judge.
	require.NoError(t, engine.EvaluateSystem(ctx, *got))
	open, err := e.Alerting.ListAlerts(ctx, sid, alerting.AlertFilter{OpenOnly: true, Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, open)

	_, err = e.db.Pool().Exec(ctx, `DELETE FROM job_leases WHERE name = 'expected_daily'`)
	require.NoError(t, err)
	require.NoError(t, e.Weather.Backfill().Run(ctx, time.Now().Add(time.Minute)))
	require.NoError(t, engine.EvaluateSystem(ctx, *got))
	open, err = e.Alerting.ListAlerts(ctx, sid, alerting.AlertFilter{OpenOnly: true, Limit: 10})
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, alerting.KindLowYield, open[0].Kind)
	assert.InDelta(t, 20, open[0].Details["percent"], 3)
}
