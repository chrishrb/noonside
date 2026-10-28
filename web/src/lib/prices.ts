import type { PricePoint } from '@/api/types';
import { num } from './format';

/** EUR/MWh → ct/kWh. */
export const ctPerKwh = (eurMwh: number) => eurMwh / 10;

/** A day-ahead price in ct/kWh, e.g. "8.4 ct/kWh" or "−1.2 ct/kWh". */
export const fmtCt = (eurMwh: number | null | undefined) => (eurMwh == null ? '—' : `${num(ctPerKwh(eurMwh), 1)} ct/kWh`);

/**
 * Price in ct/kWh at each bucket start (the interval that contains it), null
 * outside the price data. Both `points` and `timestamps` are sorted.
 */
export function priceAt(points: PricePoint[], timestamps: string[]): (number | null)[] {
  const iv = points.map((p) => [Date.parse(p.start), Date.parse(p.end), ctPerKwh(p.eur_mwh)] as const);
  let j = 0;
  return timestamps.map((ts) => {
    const t = Date.parse(ts);
    while (j < iv.length && iv[j][1] <= t) j++;
    const p = iv[j];
    return p && p[0] <= t && t < p[1] ? p[2] : null;
  });
}
