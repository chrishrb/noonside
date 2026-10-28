package weather

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
)

// pgStore keeps the forecast cache and the expected daily energy in
// weather_forecasts, expected_daily and expected_daily_keys.
type pgStore struct{ db *db.DB }

func (s *pgStore) GetWeather(ctx context.Context, systemID uuid.UUID) (*WeatherCache, error) {
	w := WeatherCache{SystemID: systemID}
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT req_key, fetched_at, data FROM weather_forecasts WHERE system_id = $1`, systemID).
		Scan(&w.Key, &w.FetchedAt, &w.Data)
	if err != nil {
		return nil, db.MapErr(err)
	}
	return &w, nil
}

func (s *pgStore) PutWeather(ctx context.Context, w *WeatherCache) error {
	_, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO weather_forecasts (system_id, req_key, fetched_at, data) VALUES ($1, $2, $3, $4)
		ON CONFLICT (system_id) DO UPDATE SET req_key = excluded.req_key, fetched_at = excluded.fetched_at, data = excluded.data`,
		w.SystemID, w.Key, w.FetchedAt, w.Data)
	return db.MapErr(err)
}

func (s *pgStore) ExpectedDailyRange(ctx context.Context, systemID uuid.UUID) (string, *time.Time, *time.Time, error) {
	var key string
	var first, last *time.Time
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT coalesce((SELECT req_key FROM expected_daily_keys WHERE system_id = $1), ''),
			min(day), max(day)
		FROM expected_daily WHERE system_id = $1`, systemID).Scan(&key, &first, &last)
	return key, first, last, db.MapErr(err)
}

func (s *pgStore) ResetExpectedDaily(ctx context.Context, systemID uuid.UUID, key string) error {
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		if _, err := q.Exec(ctx, `DELETE FROM expected_daily WHERE system_id = $1`, systemID); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `
			INSERT INTO expected_daily_keys (system_id, req_key) VALUES ($1, $2)
			ON CONFLICT (system_id) DO UPDATE SET req_key = excluded.req_key`, systemID, key)
		return err
	})
}

func (s *pgStore) PutExpectedDaily(ctx context.Context, systemID uuid.UUID, days map[string]ExpectedDay) error {
	if len(days) == 0 {
		return nil
	}
	ds := make([]string, 0, len(days))
	vs := make([]float64, 0, len(days))
	irr := make([]float64, 0, len(days))
	for d, v := range days {
		ds = append(ds, d)
		vs = append(vs, v.KWhPerKWp)
		irr = append(irr, v.IrradiationKWhM2)
	}
	_, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO expected_daily (system_id, day, kwh_per_kwp, irradiation_kwh_m2)
		SELECT $1, d::date, v, i FROM unnest($2::text[], $3::float8[], $4::float8[]) AS t(d, v, i)
		ON CONFLICT (system_id, day) DO UPDATE SET kwh_per_kwp = excluded.kwh_per_kwp, irradiation_kwh_m2 = excluded.irradiation_kwh_m2`,
		systemID, ds, vs, irr)
	return err
}

func (s *pgStore) ExpectedDaily(ctx context.Context, systemID uuid.UUID, key string, from, to time.Time) (map[string]ExpectedDay, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT to_char(e.day, 'YYYY-MM-DD'), e.kwh_per_kwp, coalesce(e.irradiation_kwh_m2, 0) FROM expected_daily e
		JOIN expected_daily_keys k ON k.system_id = e.system_id AND k.req_key = $2
		WHERE e.system_id = $1 AND e.day >= $3::date AND e.day < $4::date`,
		systemID, key, from.Format(time.DateOnly), to.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ExpectedDay{}
	for rows.Next() {
		var d string
		var v ExpectedDay
		if err := rows.Scan(&d, &v.KWhPerKWp, &v.IrradiationKWhM2); err != nil {
			return nil, err
		}
		out[d] = v
	}
	return out, rows.Err()
}

// Store persists forecasts and expected daily energy (pgStore; tests use a
// fake).
type Store interface {
	// GetWeather returns the cached forecast, or db.ErrNotFound.
	GetWeather(ctx context.Context, systemID uuid.UUID) (*WeatherCache, error)
	PutWeather(ctx context.Context, w *WeatherCache) error

	// ExpectedDailyRange returns the key the system's expected daily energy
	// was computed for and its first and last day (nil if there is none).
	ExpectedDailyRange(ctx context.Context, systemID uuid.UUID) (key string, first, last *time.Time, err error)
	// ResetExpectedDaily deletes the system's expected daily energy and sets
	// the key for the next rows.
	ResetExpectedDaily(ctx context.Context, systemID uuid.UUID, key string) error
	// PutExpectedDaily stores the expectation by local date (YYYY-MM-DD).
	PutExpectedDaily(ctx context.Context, systemID uuid.UUID, days map[string]ExpectedDay) error
	// ExpectedDaily returns the expectation by local date in [from, to) if
	// the rows were computed for key.
	ExpectedDaily(ctx context.Context, systemID uuid.UUID, key string, from, to time.Time) (map[string]ExpectedDay, error)
}

// ExpectedDay is the weather-based expectation of one local day.
type ExpectedDay struct {
	// KWhPerKWp is the expected energy per kWp from the weather model.
	KWhPerKWp float64
	// IrradiationKWhM2 is the irradiation on the panel planes (kWp-weighted),
	// the reference yield of the performance ratio.
	IrradiationKWhM2 float64
}

// WeatherCache is a system's cached forecast. Key identifies the request
// (coordinates, orientation) and Data is the normalized forecast as JSON.
type WeatherCache struct {
	SystemID  uuid.UUID
	Key       string
	FetchedAt time.Time
	Data      []byte
}
