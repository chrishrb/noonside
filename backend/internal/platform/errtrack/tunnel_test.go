package errtrack

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTunnel(t *testing.T) {
	var gotPath, gotBody, gotType string
	sentrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotType = r.URL.Path, r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("X-Sentry-Rate-Limits", "60::organization")
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer sentrySrv.Close()
	host := strings.TrimPrefix(sentrySrv.URL, "http://")
	dsn := "http://key@" + host + "/42"

	h, err := Tunnel(dsn, sentrySrv.Client())
	require.NoError(t, err)
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, TunnelPath, strings.NewReader(body)))
		return rec
	}

	envelope := `{"dsn":"` + dsn + `","sent_at":"2026-10-06T00:00:00Z"}` + "\n" + `{"type":"event"}` + "\n" + `{}`
	rec := post(envelope)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/api/42/envelope/", gotPath)
	assert.Equal(t, "application/x-sentry-envelope", gotType)
	assert.Equal(t, envelope, gotBody)
	assert.Equal(t, "60::organization", rec.Header().Get("X-Sentry-Rate-Limits"))
	assert.JSONEq(t, `{"id":"abc"}`, rec.Body.String())

	gotBody = ""
	for name, body := range map[string]string{
		"other project": `{"dsn":"http://key@` + host + `/43"}` + "\n{}",
		"other host":    `{"dsn":"https://key@o1.ingest.de.sentry.io/42"}` + "\n{}",
		"no dsn":        `{}` + "\n{}",
		"not json":      "nope",
	} {
		assert.Equal(t, http.StatusBadRequest, post(body).Code, name)
	}
	assert.Empty(t, gotBody, "nothing forwarded")

	big := `{"dsn":"` + dsn + `"}` + "\n" + strings.Repeat("x", maxEnvelopeBytes)
	assert.Equal(t, http.StatusRequestEntityTooLarge, post(big).Code)

	_, err = Tunnel("not a dsn", nil)
	assert.Error(t, err)
}
