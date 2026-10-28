package prices

import (
	"context"
	"time"

	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/jackc/pgx/v5"
)

// pgStore keeps day-ahead prices in market_prices.
type pgStore struct{ db *db.DB }

func (s *pgStore) UpsertMarketPrices(ctx context.Context, ps []MarketPrice) error {
	if len(ps) == 0 {
		return nil
	}
	zones, starts, ends := make([]string, len(ps)), make([]time.Time, len(ps)), make([]time.Time, len(ps))
	prices := make([]float64, len(ps))
	for i, p := range ps {
		zones[i], starts[i], ends[i], prices[i] = p.Zone, p.Start, p.End, p.EURMWh
	}
	_, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO market_prices (zone, starts_at, ends_at, eur_mwh)
		SELECT * FROM unnest($1::text[], $2::timestamptz[], $3::timestamptz[], $4::float8[])
		ON CONFLICT (zone, starts_at) DO UPDATE SET ends_at = EXCLUDED.ends_at, eur_mwh = EXCLUDED.eur_mwh`,
		zones, starts, ends, prices)
	return db.MapErr(err)
}

func (s *pgStore) MarketPrices(ctx context.Context, zone string, from, to time.Time) ([]MarketPrice, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT zone, starts_at, ends_at, eur_mwh::float8 FROM market_prices
		WHERE zone = $1 AND starts_at < $3 AND ends_at > $2 ORDER BY starts_at`, zone, from, to)
	return db.Collect(rows, err, func(r pgx.Row) (MarketPrice, error) {
		var p MarketPrice
		return p, r.Scan(&p.Zone, &p.Start, &p.End, &p.EURMWh)
	})
}

func (s *pgStore) NegativeMarketPrices(ctx context.Context, zone string, from, to time.Time) ([]MarketPrice, bool, error) {
	var any bool
	if err := s.db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market_prices
		WHERE zone = $1 AND starts_at < $3 AND ends_at > $2)`, zone, from, to).Scan(&any); err != nil || !any {
		return nil, false, err
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT zone, starts_at, ends_at, eur_mwh::float8 FROM market_prices
		WHERE zone = $1 AND starts_at < $3 AND ends_at > $2 AND eur_mwh < 0 ORDER BY starts_at`, zone, from, to)
	ps, err := db.Collect(rows, err, func(r pgx.Row) (MarketPrice, error) {
		var p MarketPrice
		return p, r.Scan(&p.Zone, &p.Start, &p.End, &p.EURMWh)
	})
	return ps, true, err
}

func (s *pgStore) MarketPriceRange(ctx context.Context, zone string) (*time.Time, *time.Time, error) {
	var first, last *time.Time
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT min(starts_at), max(ends_at) FROM market_prices WHERE zone = $1`, zone).Scan(&first, &last)
	return first, last, err
}
