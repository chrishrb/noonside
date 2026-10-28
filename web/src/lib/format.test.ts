import { describe, expect, it } from 'vitest';
import { addDays, fmtDate, fmtDay, fmtDuration, fmtKWh, fmtKg, fmtRel, fmtTime, fmtW, initials, splitUnit, zonedMidnight } from './format';

describe('energy formatting', () => {
  it('scales power', () => {
    expect(fmtW(512)).toBe('512 W');
    expect(fmtW(6120)).toBe('6.12 kW');
    expect(fmtW(12345)).toBe('12.3 kW');
    expect(fmtW(2_500_000)).toBe('2.50 MW');
    expect(fmtW(undefined)).toBe('—');
  });
  it('scales energy', () => {
    expect(fmtKWh(3.456)).toBe('3.46 kWh');
    expect(fmtKWh(123.45)).toBe('123.5 kWh');
    expect(fmtKWh(45678)).toBe('45.68 MWh');
    expect(fmtKg(850)).toBe('850 kg');
    expect(fmtKg(12500)).toBe('12.50 t');
  });
  it('splits units for KPI tiles', () => {
    expect(splitUnit('12.3 kWh')).toEqual(['12.3', 'kWh']);
    expect(splitUnit('—')).toEqual(['—', null]);
  });
});

describe('dates in the system time zone', () => {
  it('keeps plain dates as calendar dates', () => {
    expect(fmtDate('2025-06-03')).toBe('3 Jun 2025');
    expect(fmtDay('2025-06-03')).toBe('Tue, 3 Jun 2025');
  });
  it('shows instants in the given zone', () => {
    expect(fmtTime('2025-06-03T10:05:00Z', 'Europe/Berlin')).toBe('12:05');
    expect(fmtTime('2025-01-03T10:05:00Z', 'Europe/Berlin')).toBe('11:05');
    expect(fmtDate('2025-06-03T23:30:00Z', 'Europe/Berlin')).toBe('4 Jun 2025');
  });
  it('finds local midnight across DST', () => {
    expect(zonedMidnight('2025-06-03', 'Europe/Berlin')).toBe('2025-06-02T22:00:00.000Z');
    expect(zonedMidnight('2025-01-03', 'Europe/Berlin')).toBe('2025-01-02T23:00:00.000Z');
    expect(zonedMidnight('2025-03-30', 'Europe/Berlin')).toBe('2025-03-29T23:00:00.000Z');
    expect(zonedMidnight('2025-03-31', 'Europe/Berlin')).toBe('2025-03-30T22:00:00.000Z');
  });
  it('adds days', () => {
    expect(addDays('2024-02-28', 1)).toBe('2024-02-29');
    expect(addDays('2024-12-31', 1)).toBe('2025-01-01');
    expect(addDays('2025-03-01', -1)).toBe('2025-02-28');
  });
});

describe('misc', () => {
  it('relative time', () => {
    const now = Date.parse('2025-06-03T12:00:00Z');
    expect(fmtRel('2025-06-03T11:59:30Z', now)).toBe('just now');
    expect(fmtRel('2025-06-03T11:30:00Z', now)).toBe('30 min ago');
    expect(fmtRel('2025-06-02T11:00:00Z', now)).toBe('yesterday');
    expect(fmtRel(undefined, now)).toBe('never');
  });
  it('durations and initials', () => {
    expect(fmtDuration(45 * 60000)).toBe('45 min');
    expect(fmtDuration(135 * 60000)).toBe('2 h 15 min');
    expect(initials('Ada Lovelace')).toBe('AL');
    expect(initials('')).toBe('?');
  });
});
