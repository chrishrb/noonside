// Package geocode turns addresses into coordinates with Photon
// (https://photon.komoot.io, OpenStreetMap data, ODbL). Photon's public
// instance is free for fair use and made for search-as-you-type; for heavy
// or commercial use point the base URL at a self-hosted instance.
package geocode

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

// Place is one geocoding result.
type Place struct {
	// Label is a one-line address, e.g. "Hauptstraße 1, 86199 Augsburg, Deutschland".
	Label string
	Lat   float64
	Lon   float64
	// Kind is how precise the match is: house, street, locality, district,
	// city, county, state, country or other.
	Kind        string
	CountryCode string
}

// Client calls the Photon search API.
type Client struct {
	baseURL   string
	userAgent string
	http      *http.Client
}

// NewClient returns a client for baseURL (e.g. https://photon.komoot.io).
// userAgent identifies the app, as the OpenStreetMap usage policies ask.
func NewClient(baseURL, userAgent string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		userAgent: userAgent,
		http:      errtrack.HTTPClient(5 * time.Second),
	}
}

// Languages Photon supports for labels; others fall back to local names.
var languages = map[string]bool{"de": true, "en": true, "fr": true}

// Search returns up to limit places matching q; lang ("de", "en") selects
// the language of the labels.
func (c *Client) Search(ctx context.Context, q, lang string, limit int) ([]Place, error) {
	v := url.Values{}
	v.Set("q", q)
	// Ask for more to make up for duplicates dropped below.
	v.Set("limit", strconv.Itoa(limit+3))
	if languages[lang] {
		v.Set("lang", lang)
	}
	body, err := c.get(ctx, "/api/", v)
	if err != nil {
		return nil, err
	}
	return parse(body, limit)
}

// Reverse returns the address nearest to the coordinates (nil if none is
// near): a building with a house number, else a street, else whatever is
// nearest. The label leaves out names of buildings and shops, since it is
// used as an address.
func (c *Client) Reverse(ctx context.Context, lat, lon float64, lang string) (*Place, error) {
	v := url.Values{}
	v.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
	v.Set("lon", strconv.FormatFloat(lon, 'f', -1, 64))
	v.Set("limit", "5")
	if languages[lang] {
		v.Set("lang", lang)
	}
	body, err := c.get(ctx, "/reverse", v)
	if err != nil {
		return nil, err
	}
	var fc featureCollection
	if err := json.Unmarshal(body, &fc); err != nil {
		return nil, fmt.Errorf("photon: decode: %w", err)
	}
	// Photon sorts by distance. Points of interest come as houses without a number.
	rank := func(f feature) int {
		switch {
		case f.Properties.Type == "house" && f.Properties.HouseNumber != "":
			return 0
		case f.Properties.Type == "street":
			return 1
		}
		return 2
	}
	var best *Place
	bestRank := 3
	for _, f := range fc.Features {
		if f.Properties.Type == "house" && f.Properties.Street != "" {
			f.Properties.Name = ""
		}
		p, ok := toPlace(f)
		if r := rank(f); ok && r < bestRank {
			best, bestRank = &p, r
		}
	}
	return best, nil
}

func (c *Client) get(ctx context.Context, path string, v url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("photon: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("photon: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("photon: status %d", resp.StatusCode)
	}
	return body, nil
}

type featureCollection struct {
	Features []feature `json:"features"`
}

type feature struct {
	Properties struct {
		Type        string `json:"type"`
		Name        string `json:"name"`
		Street      string `json:"street"`
		HouseNumber string `json:"housenumber"`
		Postcode    string `json:"postcode"`
		City        string `json:"city"`
		District    string `json:"district"`
		State       string `json:"state"`
		Country     string `json:"country"`
		CountryCode string `json:"countrycode"`
	} `json:"properties"`
	Geometry struct {
		Coordinates []float64 `json:"coordinates"` // lon, lat
	} `json:"geometry"`
}

func parse(body []byte, limit int) ([]Place, error) {
	var fc featureCollection
	if err := json.Unmarshal(body, &fc); err != nil {
		return nil, fmt.Errorf("photon: decode: %w", err)
	}
	out := []Place{}
	seen := map[string]bool{}
	for _, f := range fc.Features {
		p, ok := toPlace(f)
		if !ok || seen[p.Label] {
			continue
		}
		seen[p.Label] = true
		out = append(out, p)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// toPlace converts a feature; ok is false without coordinates or label.
func toPlace(f feature) (Place, bool) {
	l := label(f)
	if len(f.Geometry.Coordinates) != 2 || l == "" {
		return Place{}, false
	}
	return Place{
		Label: l, Lon: f.Geometry.Coordinates[0], Lat: f.Geometry.Coordinates[1],
		Kind: kind(f.Properties.Type), CountryCode: f.Properties.CountryCode,
	}, true
}

// label builds "Name, Street 1, 12345 City, Country", skipping empty parts.
func label(f feature) string {
	p := f.Properties
	var parts []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" && (len(parts) == 0 || parts[len(parts)-1] != s) {
			parts = append(parts, s)
		}
	}
	street := strings.TrimSpace(p.Street + " " + p.HouseNumber)
	switch p.Type {
	case "house":
		if p.Name != "" && p.Name != street {
			add(p.Name)
		}
		add(street)
	case "street":
		add(p.Name)
	default:
		if p.Name != p.City {
			add(p.Name)
		}
	}
	add(strings.TrimSpace(p.Postcode + " " + p.City))
	if p.City == "" && p.Type != "state" && p.Type != "country" {
		add(p.State)
	}
	add(p.Country)
	return strings.Join(parts, ", ")
}

func kind(t string) string {
	switch t {
	case "house", "street", "locality", "district", "city", "county", "state", "country":
		return t
	}
	return "other"
}
