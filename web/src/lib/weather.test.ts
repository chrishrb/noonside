import { describe, expect, it } from 'vitest';
import { expectedAt, weatherIcon, weatherKind } from './weather';

describe('weather', () => {
  it('maps WMO codes to conditions and icons', () => {
    expect(weatherKind(0)).toBe('clear');
    expect(weatherKind(2)).toBe('partlyCloudy');
    expect(weatherKind(45)).toBe('fog');
    expect(weatherKind(63)).toBe('rain');
    expect(weatherKind(81)).toBe('showers');
    expect(weatherKind(86)).toBe('snow');
    expect(weatherKind(96)).toBe('thunder');
    expect(weatherIcon(0, true)).toBe('sun');
    expect(weatherIcon(0, false)).toBe('moon');
    expect(weatherIcon(3, false)).toBe('cloud');
  });

  it('interpolates 15-minute points at the middle of 5-minute buckets', () => {
    const points = [
      { time: '2026-06-01T10:07:30Z', w: 0 },
      { time: '2026-06-01T10:22:30Z', w: 900 },
    ];
    const ts = ['2026-06-01T09:00:00Z', '2026-06-01T10:05:00Z', '2026-06-01T10:10:00Z', '2026-06-01T10:15:00Z', '2026-06-01T10:25:00Z', '2026-06-01T11:00:00Z'];
    // Bucket middles: 10:07:30 → 0, 10:12:30 → 300, 10:17:30 → 600, 10:27:30 → last value.
    expect(expectedAt(points, ts)).toEqual([null, 0, 300, 600, 900, null]);
    expect(expectedAt(points, ts, 0.5)).toEqual([null, 0, 150, 300, 450, null]);
    expect(expectedAt([], ts)).toEqual(ts.map(() => null));
  });
});
