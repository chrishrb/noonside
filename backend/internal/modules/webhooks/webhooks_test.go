package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrishrb/noonside/internal/modules/advice"
	"github.com/chrishrb/noonside/internal/modules/prices"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/notify"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type box struct{}

func (box) Open(sealed string) (string, error) {
	if sealed != "sealed" {
		return "", errors.New("bad")
	}
	return "s3cret", nil
}

func TestSend(t *testing.T) {
	var header http.Header
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		body, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()

	sys := &systems.System{ID: uuid.New(), Name: "Roof"}
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	p := TestPayload("https://solar.example.com/", sys, now)
	w := Webhook{URL: srv.URL, SecretEnc: "sealed"}
	require.NoError(t, Send(context.Background(), notify.New(true, ""), box{}, w, p))

	assert.Equal(t, "test", header.Get(EventHeader))
	assert.Equal(t, p.ID.String(), header.Get(DeliveryHeader))
	assert.Equal(t, notify.Sign("s3cret", body), header.Get(notify.SignatureHeader))
	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "Roof", got["system_name"])
	assert.Equal(t, sys.ID.String(), got["system_id"])
	assert.Equal(t, "https://solar.example.com/s/"+sys.ID.String()+"/settings/webhooks", got["url"])
	assert.Equal(t, "2026-06-15T10:00:00Z", got["created_at"])

	// Without a secret the request is unsigned; a sealed secret needs the box.
	w.SecretEnc = ""
	require.NoError(t, Send(context.Background(), notify.New(true, ""), nil, w, p))
	assert.Empty(t, header.Get(notify.SignatureHeader))
	w.SecretEnc = "sealed"
	assert.ErrorContains(t, Send(context.Background(), notify.New(true, ""), nil, w, p), "secret key")
}

func TestAdviceJSON(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	start := time.Date(2026, 6, 15, 11, 0, 0, 0, berlin)
	price := 12.5
	a := &advice.Advice{
		Available: true, Date: time.Date(2026, 6, 15, 0, 0, 0, 0, berlin), Hours: 3,
		Solar:    &advice.Window{Start: start, End: start.Add(3 * time.Hour), ExpectedKWh: 18.2, MeanW: 6067, MeanEURMWh: &price},
		Negative: []prices.MarketPrice{{Start: start, End: start.Add(time.Hour), EURMWh: -3}},
	}
	b, err := json.Marshal(AdviceJSON(a))
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"available": true, "date": "2026-06-15", "hours": 3,
		"solar_window": {"start": "2026-06-15T09:00:00Z", "end": "2026-06-15T12:00:00Z", "expected_kwh": 18.2, "mean_w": 6067, "mean_eur_mwh": 12.5},
		"negative_prices": [{"start": "2026-06-15T09:00:00Z", "end": "2026-06-15T10:00:00Z", "eur_mwh": -3}],
		"slots": []
	}`, string(b))
}
