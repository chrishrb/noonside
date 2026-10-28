import { describe, expect, it } from 'vitest';
import { alignTo, bucketLabel, bucketTick, dayAxis, drillDown, estimateTargets, periodLabel, periodStart } from './period';

describe('periods', () => {
  it('labels periods', () => {
    expect(periodLabel('day', '2025-06-03', '2020-01-01')).toBe('Tue, 3 Jun 2025');
    expect(periodLabel('month', '2025-06-03', '2020-01-01')).toBe('June 2025');
    expect(periodLabel('year', '2025-06-03', '2020-01-01')).toBe('2025');
    expect(periodLabel('total', '2025-06-03', '2020-04-15')).toBe('Since 15 Apr 2020');
  });
  it('finds the period start', () => {
    expect(periodStart('month', '2025-06-17')).toBe('2025-06-01');
    expect(periodStart('year', '2025-06-17')).toBe('2025-01-01');
  });
  it('labels buckets in the system time zone', () => {
    // Local midnight of 1 Jun in Berlin is 31 May 22:00 UTC.
    expect(bucketLabel('month', '2025-05-31T22:00:00Z', 'Europe/Berlin')).toBe('Sun, 1 Jun 2025');
    expect(bucketLabel('year', '2024-12-31T23:00:00Z', 'Europe/Berlin')).toBe('January 2025');
    expect(bucketTick('day', '2025-06-03T07:00:00Z', 'Europe/Berlin', 3)).toBe('09:00');
    expect(bucketTick('day', '2025-06-03T08:00:00Z', 'Europe/Berlin', 3)).toBeNull();
  });
  it('drills down to the next finer period in the system time zone', () => {
    // Year bucket starts at local midnight of 1 Jan (31 Dec 23:00 UTC in Berlin).
    expect(drillDown('total', '2024-12-31T23:00:00Z', 'Europe/Berlin')).toEqual({ period: 'year', date: '2025-01-01' });
    expect(drillDown('year', '2025-05-31T22:00:00Z', 'Europe/Berlin')).toEqual({ period: 'month', date: '2025-06-01' });
    expect(drillDown('month', '2025-06-16T22:00:00Z', 'Europe/Berlin')).toEqual({ period: 'day', date: '2025-06-17' });
    expect(drillDown('month', '2025-06-17T04:00:00Z', 'America/New_York')).toEqual({ period: 'day', date: '2025-06-17' });
    expect(drillDown('day', '2025-06-17T10:00:00Z', 'Europe/Berlin')).toBeNull();
  });
  it('spans day charts 04:00–22:00 local on a 5-minute grid', () => {
    const ts = dayAxis('2025-06-02T22:00:00Z', 'Europe/Berlin'); // 00:00 Berlin
    expect(ts).toHaveLength(216);
    expect(ts[0]).toBe('2025-06-03T02:00:00.000Z'); // 04:00 local
    expect(ts.at(-1)).toBe('2025-06-03T19:55:00.000Z'); // 21:55 local
    // The spring-forward day has the same local hours.
    const dst = dayAxis('2025-03-29T23:00:00Z', 'Europe/Berlin');
    expect(dst).toHaveLength(216);
    expect(dst[0]).toBe('2025-03-30T02:00:00.000Z');
  });
  it('aligns sparse values to the axis', () => {
    const axis = ['2025-06-03T10:00:00.000Z', '2025-06-03T10:05:00.000Z', '2025-06-03T10:10:00.000Z'];
    expect(alignTo(axis, ['2025-06-03T10:05:00Z', '2025-06-03T10:10:00Z'], [5, 10])).toEqual([null, 5, 10]);
  });
  it('estimates targets from peak power', () => {
    const t = estimateTargets(10);
    expect(t).toHaveLength(12);
    expect(t[5]).toBeGreaterThan(t[0]);
    expect(t.every((v) => v % 10 === 0)).toBe(true);
  });
});
