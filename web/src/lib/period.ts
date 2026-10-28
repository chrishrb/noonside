import i18n from '@/i18n';
import { fmtDate, fmtDay, fmtMonth, fmtTime, monthName, pad, parseYmd, partsIn, ymd } from './format';
import type { Period } from '@/api/types';

export const PERIOD_KEYS: Period[] = ['day', 'month', 'year', 'total'];
export const isPeriod = (v: string | null): v is Period => PERIOD_KEYS.some((k) => k === v);

// Labels are looked up on each call so they follow the active language.
export const periodName = (p: Period) => i18n.t(`period.name.${p}`);
export const chartTitle = (p: Period) => i18n.t(`period.chart.${p}`);
export const currentLabel = (p: Exclude<Period, 'total'>) => i18n.t(`period.current.${p}`);
export const bestLabel = (p: Exclude<Period, 'day'>) => i18n.t(`period.best.${p}`);
export const bucketName = (p: Period) => i18n.t(`period.bucket.${p}`);

export function periodLabel(period: Period, date: string, since: string): string {
  const d = parseYmd(date);
  if (period === 'day') return fmtDay(date);
  if (period === 'month') return fmtMonth(d.year, d.month);
  if (period === 'year') return String(d.year);
  return i18n.t('period.since', { date: fmtDate(since) });
}

/** First day of the period that contains `date`. */
export function periodStart(period: Period, date: string): string {
  const d = parseYmd(date);
  if (period === 'month') return ymd({ ...d, day: 1 });
  if (period === 'year') return ymd({ year: d.year, month: 1, day: 1 });
  return date;
}

/** Tooltip / table label of one bucket, in the system's time zone. */
export function bucketLabel(period: Period, ts: string, tz: string): string {
  if (period === 'day') return fmtTime(ts, tz);
  const p = partsIn(new Date(ts), tz);
  if (period === 'month') return fmtDay(ymd(p));
  if (period === 'year') return fmtMonth(p.year, p.month);
  return String(p.year);
}

export const FINER: Record<Period, Exclude<Period, 'total'> | null> = { total: 'year', year: 'month', month: 'day', day: null };

/**
 * Drill-down target for a bucket: the next finer period and the first local
 * day of the bucket (a year's 1 Jan, a month's 1st, a day), in the system's
 * time zone. Null on day curves, which have no finer view.
 */
export function drillDown(period: Period, ts: string, tz: string): { period: Exclude<Period, 'total'>; date: string } | null {
  const next = FINER[period];
  if (!next) return null;
  const p = partsIn(new Date(ts), tz);
  return { period: next, date: periodStart(next, ymd(p)) };
}

/** X-axis tick for a bucket, or null to skip it. `band` is the bucket width in px. */
export function bucketTick(period: Period, ts: string, tz: string, band: number): string | null {
  const p = partsIn(new Date(ts), tz);
  if (period === 'day') return p.minute === 0 && p.hour % 3 === 0 ? `${pad(p.hour)}:00` : null;
  if (period === 'month') return band >= 26 || p.day === 1 || p.day % 5 === 0 ? String(p.day) : null;
  if (period === 'year') return band >= 34 ? monthName(p.month, 'short') : monthName(p.month, 'short')[0];
  return String(p.year);
}

/** Fixed day chart window, in local hours. */
export const DAY_FROM = 4, DAY_TO = 22;

/**
 * The 5-minute bucket starts of a day chart, DAY_FROM–DAY_TO local time on
 * the day that starts at `rangeStart` (local midnight), whatever data exists.
 */
export function dayAxis(rangeStart: string, tz: string, from = DAY_FROM, to = DAY_TO): string[] {
  const t0 = Date.parse(rangeStart);
  const day = ymd(partsIn(new Date(t0), tz));
  const out: string[] = [];
  // 25 h covers DST days; buckets are kept by their local date and hour.
  for (let t = t0; t < t0 + 25 * 3_600_000; t += 300_000) {
    const p = partsIn(new Date(t), tz);
    if (ymd(p) === day && p.hour >= from && p.hour < to) out.push(new Date(t).toISOString());
  }
  return out;
}

/** Values placed on `axis` by timestamp; buckets without a value are null. */
export function alignTo<T>(axis: string[], timestamps: string[], values: T[]): (T | null)[] {
  const at = new Map(timestamps.map((t, i) => [Date.parse(t), i]));
  return axis.map((t) => {
    const i = at.get(Date.parse(t));
    return i == null ? null : (values[i] ?? null);
  });
}

/** Typical monthly yield per kWp and day (Central Europe), for target estimates. */
export const SEASON = [0.95, 1.75, 2.95, 4.1, 4.9, 5.25, 5.2, 4.6, 3.5, 2.2, 1.1, 0.75];
export function estimateTargets(kwp: number): number[] {
  return SEASON.map((f, i) => Math.round((kwp * f * new Date(Date.UTC(2025, i + 1, 0)).getUTCDate() * 0.9) / 10) * 10);
}
