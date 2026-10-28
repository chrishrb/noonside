import { describe, expect, it } from 'vitest';
import type { DayEnergyInverter } from '@/api/types';
import { dayEnergyChanges } from './EditDayDialog';

const row = (inverter_id: string, device_kwh: number | null, manual_kwh: number | null = null): DayEnergyInverter =>
  ({ inverter_id, device_kwh, manual_kwh, manual_at: manual_kwh == null ? null : '2026-10-05T10:00:00Z' });

describe('dayEnergyChanges', () => {
  it('sends only changed inverters', () => {
    const rows = [row('a', 40), row('b', 12, 11.5), row('c', null)];
    expect(dayEnergyChanges(rows, { a: '11,5', b: '11.5', c: '' })).toEqual([{ inverter_id: 'a', energy_kwh: 11.5 }]);
  });

  it('clears a correction with an empty field', () => {
    expect(dayEnergyChanges([row('a', 40, 10)], { a: '  ' })).toEqual([{ inverter_id: 'a', energy_kwh: null }]);
  });

  it('accepts zero and skips invalid input', () => {
    expect(dayEnergyChanges([row('a', 40), row('b', 3)], { a: '0', b: '-1' })).toEqual([{ inverter_id: 'a', energy_kwh: 0 }]);
    expect(dayEnergyChanges([row('a', 40)], { a: 'abc' })).toEqual([]);
  });
});
