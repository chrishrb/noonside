package pvoutput

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

// fakeStore has one inverter producing a constant 1200 W from 08:00 to
// 18:00 local every day, with 5-minute samples.
type fakeStore struct {
	sys   systems.System
	inv   uuid.UUID
	last  time.Time
	state []byte
}

func (f *fakeStore) GetSystem(context.Context, uuid.UUID) (*systems.System, error) {
	return &f.sys, nil
}

func (f *fakeStore) LastMeasurementTime(context.Context, uuid.UUID) (*time.Time, error) {
	return &f.last, nil
}

func (f *fakeStore) PowerCurve(_ context.Context, _ uuid.UUID, from, to time.Time, b time.Duration) ([]telemetry.PowerPoint, error) {
	var out []telemetry.PowerPoint
	for t := from.Truncate(b); t.Before(to); t = t.Add(b) {
		if l := t.In(berlin); t.Before(from) || l.Hour() < 8 || l.Hour() >= 18 || t.After(f.last) {
			continue
		}
		out = append(out, telemetry.PowerPoint{Time: t, InverterID: f.inv, PacW: 1200})
	}
	return out, nil
}

func (f *fakeStore) Energy(_ context.Context, _ uuid.UUID, _ string, from, to time.Time) ([]telemetry.EnergyBucket, error) {
	var out []telemetry.EnergyBucket
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		out = append(out, telemetry.EnergyBucket{Start: time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC), InverterID: f.inv, EnergyWh: 12000})
	}
	return out, nil
}

func (f *fakeStore) SetDataSourceState(_ context.Context, _ uuid.UUID, state []byte) error {
	f.state = state
	return nil
}

type request struct {
	path, key, sid string
	rows           []string
}

func pvoServer(t *testing.T, status int) (*httptest.Server, *[]request) {
	var reqs []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		reqs = append(reqs, request{
			path: r.URL.Path, key: r.Header.Get("X-Pvoutput-Apikey"), sid: r.Header.Get("X-Pvoutput-SystemId"),
			rows: strings.Split(r.PostForm.Get("data"), ";"),
		})
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte("Forbidden 403: Exceeded 60 requests per hour"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func TestExporter(t *testing.T) {
	srv, reqs := pvoServer(t, http.StatusOK)
	st := &fakeStore{sys: systems.System{Timezone: "Europe/Berlin"}, inv: uuid.New()}
	ds := Source{ID: uuid.New(), SystemID: uuid.New()}
	now := time.Date(2026, 6, 15, 10, 2, 0, 0, berlin)
	st.last = now.Add(-time.Minute)

	e := New(st, srv.URL, ds, Config{SystemID: "12345"}, "k3y")
	e.SetClock(func() time.Time { return now })
	require.NoError(t, e.Poll(context.Background()))

	// First run: the output of the last 13 finished days, then today's
	// statuses 08:00–09:55 (the bucket at 10:00 may still fill up).
	require.Len(t, *reqs, 2)
	out := (*reqs)[0]
	assert.Equal(t, "/service/r2/addbatchoutput.jsp", out.path)
	assert.Equal(t, "k3y", out.key)
	assert.Equal(t, "12345", out.sid)
	require.Len(t, out.rows, 13)
	assert.Equal(t, "20260602,12000,,1200,08:00", out.rows[0])
	assert.Equal(t, "20260614,12000,,1200,08:00", out.rows[12])

	status := (*reqs)[1]
	assert.Equal(t, "/service/r2/addbatchstatus.jsp", status.path)
	require.Len(t, status.rows, 24)
	assert.Equal(t, "20260615,08:00,100,1200", status.rows[0], "energy of the day so far and power")
	assert.Equal(t, "20260615,09:55,2400,1200", status.rows[23])

	var s State
	require.NoError(t, json.Unmarshal(st.state, &s))
	assert.Equal(t, "2026-06-14", s.OutputUntil)
	assert.True(t, s.StatusUntil.Equal(time.Date(2026, 6, 15, 10, 0, 0, 0, berlin)))

	// Next run: only the new statuses; the day's energy continues.
	*reqs = nil
	now = now.Add(10 * time.Minute)
	st.last = now.Add(-time.Minute)
	e = New(st, srv.URL, Source{ID: ds.ID, SystemID: ds.SystemID, State: st.state}, Config{SystemID: "12345"}, "k3y")
	e.SetClock(func() time.Time { return now })
	require.NoError(t, e.Poll(context.Background()))
	require.Len(t, *reqs, 1)
	assert.Equal(t, []string{"20260615,10:00,2500,1200", "20260615,10:05,2600,1200"}, (*reqs)[0].rows)
}

func TestExporterBacklog(t *testing.T) {
	srv, reqs := pvoServer(t, http.StatusOK)
	st := &fakeStore{sys: systems.System{Timezone: "Europe/Berlin"}, inv: uuid.New()}
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, berlin)
	st.last = now
	// Uploaded until two days ago at 17:00.
	state, _ := json.Marshal(State{StatusUntil: time.Date(2026, 6, 13, 17, 0, 0, 0, berlin), OutputUntil: "2026-06-14"})
	e := New(st, srv.URL, Source{ID: uuid.New(), State: state}, Config{SystemID: "1"}, "k")
	e.SetClock(func() time.Time { return now })
	require.NoError(t, e.Poll(context.Background()))

	// No output due; three status requests at most per run.
	require.Len(t, *reqs, 3)
	assert.Equal(t, "20260613,17:00,10900,1200", (*reqs)[0].rows[0])
	assert.Len(t, (*reqs)[0].rows, 12, "the rest of the 13th")
	assert.Equal(t, "20260614,08:00,100,1200", (*reqs)[1].rows[0])
	assert.Len(t, (*reqs)[1].rows, 30)
	assert.Equal(t, "20260614,10:30,3100,1200", (*reqs)[2].rows[0], "continues where the batch ended")
	var s State
	require.NoError(t, json.Unmarshal(st.state, &s))
	assert.True(t, s.StatusUntil.Equal(time.Date(2026, 6, 14, 13, 0, 0, 0, berlin)))
	assert.True(t, ParseState([]byte("{")).StatusUntil.IsZero(), "an invalid state starts over")
}

func TestExporterErrors(t *testing.T) {
	srv, _ := pvoServer(t, http.StatusForbidden)
	st := &fakeStore{sys: systems.System{Timezone: "Europe/Berlin"}, inv: uuid.New()}
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, berlin)
	st.last = now
	e := New(st, srv.URL, Source{ID: uuid.New()}, Config{SystemID: "1"}, "k")
	e.SetClock(func() time.Time { return now })
	err := e.Poll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Exceeded 60 requests per hour")
	assert.Nil(t, st.state, "nothing recorded as uploaded")

	e = New(st, srv.URL, Source{ID: uuid.New()}, Config{}, "")
	assert.ErrorContains(t, e.Poll(context.Background()), "required")
}
