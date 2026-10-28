// Package pvgis estimates the monthly yield of a PV system from the EU JRC's
// PVGIS (free, no key), for the monthly targets.
package pvgis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"
)

// DefaultLossPct covers cable, inverter, soiling and mismatch losses (the
// PVGIS default).
const DefaultLossPct = 14

// StatusError is an error response of PVGIS; 400 means it has no data for
// the request (e.g. a location at sea).
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string { return fmt.Sprintf("pvgis: status %d: %s", e.Status, e.Message) }

type Client struct {
	base string
	http *http.Client
}

// NewClient calls the PVGIS API at base, e.g. https://re.jrc.ec.europa.eu/api/v5_3.
func NewClient(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: errtrack.HTTPClient(20 * time.Second)}
}

// Plane is a module field: Tilt 0 is horizontal; Azimuth 0 = south,
// -90 = east, 90 = west (PVGIS "aspect").
type Plane struct {
	KWp, Tilt, Azimuth float64
}

// Monthly returns the expected kWh for January..December, summed over the
// planes. Without planes, kwp is estimated at the optimal fixed orientation.
func (c *Client) Monthly(ctx context.Context, lat, lon float64, planes []Plane, kwp float64) ([12]float64, error) {
	var out [12]float64
	if len(planes) == 0 {
		return c.plane(ctx, lat, lon, Plane{KWp: kwp}, true)
	}
	for _, p := range planes {
		if p.KWp <= 0 {
			continue
		}
		m, err := c.plane(ctx, lat, lon, p, false)
		if err != nil {
			return out, err
		}
		for i := range out {
			out[i] += m[i]
		}
	}
	return out, nil
}

func (c *Client) plane(ctx context.Context, lat, lon float64, p Plane, optimal bool) ([12]float64, error) {
	var out [12]float64
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	q := url.Values{}
	q.Set("lat", f(lat))
	q.Set("lon", f(lon))
	q.Set("peakpower", f(p.KWp))
	q.Set("loss", strconv.Itoa(DefaultLossPct))
	if optimal {
		q.Set("optimalangles", "1")
	} else {
		q.Set("angle", f(p.Tilt))
		q.Set("aspect", f(p.Azimuth))
	}
	q.Set("outputformat", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/PVcalc?"+q.Encode(), nil)
	if err != nil {
		return out, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return out, fmt.Errorf("pvgis: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return out, fmt.Errorf("pvgis: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &e)
		return out, &StatusError{Status: resp.StatusCode, Message: e.Message}
	}
	var r struct {
		Outputs struct {
			Monthly struct {
				Fixed []struct {
					Month int     `json:"month"`
					EM    float64 `json:"E_m"`
				} `json:"fixed"`
			} `json:"monthly"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return out, fmt.Errorf("pvgis: decode: %w", err)
	}
	if len(r.Outputs.Monthly.Fixed) != 12 {
		return out, fmt.Errorf("pvgis: expected 12 months, got %d", len(r.Outputs.Monthly.Fixed))
	}
	for _, m := range r.Outputs.Monthly.Fixed {
		if m.Month >= 1 && m.Month <= 12 {
			out[m.Month-1] = m.EM
		}
	}
	return out, nil
}
