import { describe, expect, it } from 'vitest';
import { fmtCt, priceAt } from './prices';

describe('priceAt', () => {
  const points = [
    { start: '2026-06-01T10:00:00Z', end: '2026-06-01T11:00:00Z', eur_mwh: 84 },
    { start: '2026-06-01T11:00:00Z', end: '2026-06-01T11:15:00Z', eur_mwh: -12 },
  ];
  it('maps buckets to the interval that contains them, in ct/kWh', () => {
    expect(priceAt(points, ['2026-06-01T09:55:00Z', '2026-06-01T10:00:00Z', '2026-06-01T10:55:00Z', '2026-06-01T11:10:00Z', '2026-06-01T11:15:00Z']))
      .toEqual([null, 8.4, 8.4, -1.2, null]);
  });
  it('handles no prices', () => {
    expect(priceAt([], ['2026-06-01T10:00:00Z'])).toEqual([null]);
  });
});

describe('fmtCt', () => {
  it('formats EUR/MWh as ct/kWh', () => {
    expect(fmtCt(84)).toBe('8.4 ct/kWh');
    expect(fmtCt(undefined)).toBe('—');
  });
});
