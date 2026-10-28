package prices

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"
)

// Awattar reads the EPEX day-ahead prices from aWATTar's free market data
// API (fair use: at most 100 requests a day), one host per zone.
type Awattar struct {
	// URLs by zone, e.g. https://api.awattar.de.
	URLs map[string]string
	http *http.Client
}

// NewAwattar returns the source; de and at are the base URLs for DE-LU and AT.
func NewAwattar(de, at string) *Awattar {
	urls := map[string]string{}
	if de != "" {
		urls[ZoneDELU] = strings.TrimRight(de, "/")
	}
	if at != "" {
		urls[ZoneAT] = strings.TrimRight(at, "/")
	}
	return &Awattar{URLs: urls, http: errtrack.HTTPClient(Timeout)}
}

func (a *Awattar) Name() string { return "aWATTar (EPEX Spot)" }

type awattarResponse struct {
	Data []struct {
		Start       int64   `json:"start_timestamp"`
		End         int64   `json:"end_timestamp"`
		MarketPrice float64 `json:"marketprice"`
		Unit        string  `json:"unit"`
	} `json:"data"`
}

func (a *Awattar) Fetch(ctx context.Context, zone string, from, to time.Time) ([]MarketPrice, error) {
	base, ok := a.URLs[zone]
	if !ok {
		return nil, fmt.Errorf("aWATTar has no prices for zone %s", zone)
	}
	body, err := get(ctx, a.http, fmt.Sprintf("%s/v1/marketdata?start=%d&end=%d", base, from.UnixMilli(), to.UnixMilli()))
	if err != nil {
		return nil, err
	}
	return parseAwattar(zone, body)
}

func parseAwattar(zone string, body []byte) ([]MarketPrice, error) {
	var r awattarResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode aWATTar response: %w", err)
	}
	out := make([]MarketPrice, 0, len(r.Data))
	for _, d := range r.Data {
		if !strings.EqualFold(d.Unit, "Eur/MWh") {
			return nil, fmt.Errorf("unexpected aWATTar unit %q", d.Unit)
		}
		out = append(out, MarketPrice{
			Zone: zone, Start: time.UnixMilli(d.Start).UTC(), End: time.UnixMilli(d.End).UTC(), EURMWh: d.MarketPrice,
		})
	}
	return normalize(out), nil
}
