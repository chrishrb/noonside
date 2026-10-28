package prices

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"
)

// EntsoE reads the day-ahead prices (document A44) from the ENTSO-E
// Transparency Platform; it needs a security token (free on registration).
type EntsoE struct {
	BaseURL string
	token   string
	http    *http.Client
}

func NewEntsoE(baseURL, token string) *EntsoE {
	return &EntsoE{BaseURL: strings.TrimRight(baseURL, "/"), token: token, http: errtrack.HTTPClient(Timeout)}
}

func (e *EntsoE) Name() string { return "ENTSO-E Transparency Platform" }

// entsoeDomains are the EIC codes of the bidding zones.
var entsoeDomains = map[string]string{
	ZoneDELU: "10Y1001A1001A82H",
	ZoneAT:   "10YAT-APG------L",
}

type entsoeDoc struct {
	XMLName    xml.Name `xml:"Publication_MarketDocument"`
	TimeSeries []struct {
		Period []struct {
			Interval struct {
				Start string `xml:"start"`
				End   string `xml:"end"`
			} `xml:"timeInterval"`
			Resolution string `xml:"resolution"`
			Points     []struct {
				Position int     `xml:"position"`
				Price    float64 `xml:"price.amount"`
			} `xml:"Point"`
		} `xml:"Period"`
	} `xml:"TimeSeries"`
}

type entsoeAck struct {
	XMLName xml.Name `xml:"Acknowledgement_MarketDocument"`
	Reason  struct {
		Code string `xml:"code"`
		Text string `xml:"text"`
	} `xml:"Reason"`
}

func (e *EntsoE) Fetch(ctx context.Context, zone string, from, to time.Time) ([]MarketPrice, error) {
	domain, ok := entsoeDomains[zone]
	if !ok {
		return nil, fmt.Errorf("no ENTSO-E domain for zone %s", zone)
	}
	q := url.Values{}
	q.Set("securityToken", e.token)
	q.Set("documentType", "A44")
	q.Set("in_Domain", domain)
	q.Set("out_Domain", domain)
	q.Set("periodStart", from.UTC().Format("200601021504"))
	q.Set("periodEnd", to.UTC().Format("200601021504"))
	body, err := get(ctx, e.http, e.BaseURL+"?"+q.Encode())
	if ack, ok := parseAck(body); ok {
		// 999 "No matching data found" is an empty answer, not an error.
		if strings.Contains(strings.ToLower(ack.Reason.Text), "no matching data") {
			return nil, nil
		}
		return nil, fmt.Errorf("ENTSO-E: %s %s", ack.Reason.Code, ack.Reason.Text)
	}
	if err != nil {
		return nil, err
	}
	return parseEntsoE(zone, body)
}

func parseAck(body []byte) (*entsoeAck, bool) {
	if !bytes.Contains(body, []byte("Acknowledgement_MarketDocument")) {
		return nil, false
	}
	var a entsoeAck
	if err := xml.Unmarshal(body, &a); err != nil {
		return nil, false
	}
	return &a, true
}

func parseEntsoE(zone string, body []byte) ([]MarketPrice, error) {
	var doc entsoeDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode ENTSO-E response: %w", err)
	}
	var out []MarketPrice
	for _, ts := range doc.TimeSeries {
		for _, p := range ts.Period {
			start, err := parseEntsoETime(p.Interval.Start)
			if err != nil {
				return nil, err
			}
			end, err := parseEntsoETime(p.Interval.End)
			if err != nil {
				return nil, err
			}
			step, err := parseResolution(p.Resolution)
			if err != nil {
				return nil, err
			}
			pts := p.Points
			sort.Slice(pts, func(i, j int) bool { return pts[i].Position < pts[j].Position })
			// Curve type A03 leaves out points whose price equals the one
			// before, so each point lasts until the next position.
			for i, pt := range pts {
				s := start.Add(time.Duration(pt.Position-1) * step)
				e := end
				if i+1 < len(pts) {
					e = start.Add(time.Duration(pts[i+1].Position-1) * step)
				}
				for t := s; t.Before(e) && t.Before(end); t = t.Add(step) {
					out = append(out, MarketPrice{Zone: zone, Start: t, End: t.Add(step), EURMWh: pt.Price})
				}
			}
		}
	}
	return normalize(out), nil
}

func parseEntsoETime(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02T15:04Z", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("ENTSO-E time %q: %w", s, err)
	}
	return t, nil
}

func parseResolution(r string) (time.Duration, error) {
	switch r {
	case "PT15M":
		return 15 * time.Minute, nil
	case "PT30M":
		return 30 * time.Minute, nil
	case "PT60M", "PT1H":
		return time.Hour, nil
	}
	return 0, fmt.Errorf("unsupported ENTSO-E resolution %q", r)
}
