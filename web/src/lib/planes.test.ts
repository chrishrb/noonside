import { describe, expect, it } from 'vitest';
import type { Inverter, Plane } from '@/api/types';
import { inverterLabels, isPlaneSelected, planeMembers, planeName } from './planes';

const inv = (id: string, plane_id?: string): Inverter => ({ id, external_ref: id, name: id, model: '', serial_number: '', peak_power_kwp: 0, color: '', sort_order: 0, plane_id });
const plane = (id?: string, name?: string): Plane => ({ id, name, kwp: 1, tilt_deg: 30, azimuth_deg: 0 });

describe('module planes', () => {
  it('lists the plane’s inverters in inverter order', () => {
    const invs = [inv('a', 'p'), inv('b', 'q'), inv('c', 'p')];
    expect(planeMembers(plane('p'), invs)).toEqual(['a', 'c']);
    expect(planeMembers(plane(), [inv('a')])).toEqual([]);
  });
  it('matches the current selection', () => {
    const invs = [inv('a', 'p'), inv('b', 'p'), inv('c')];
    expect(isPlaneSelected(plane('p'), invs, ['b', 'a'])).toBe(true);
    expect(isPlaneSelected(plane('p'), invs, ['a'])).toBe(false);
    expect(isPlaneSelected(plane('p'), invs, [])).toBe(false);
    expect(isPlaneSelected(plane('p'), [inv('a', 'p')], [])).toBe(true);
    expect(isPlaneSelected(plane('x'), invs, [])).toBe(false);
  });
  it('falls back to a numbered name', () => {
    expect(planeName(plane('p', ' '), 1, (n) => `Plane ${n}`)).toBe('Plane 2');
    expect(planeName(plane('p', 'East'), 1, (n) => `Plane ${n}`)).toBe('East');
  });
  it('prefixes every inverter on a plane when there are several planes', () => {
    const named = [{ ...inv('a', 'p'), name: 'WR 1' }, { ...inv('b', 'q'), name: 'WR 1' }, { ...inv('c', 'p'), name: 'WR 2' }, { ...inv('d'), name: 'WR 3' }];
    const labels = inverterLabels(named, new Map([['p', 'Süd'], ['q', 'Nord']]));
    expect([...labels.values()]).toEqual(['Süd · WR 1', 'Nord · WR 1', 'Süd · WR 2', 'WR 3']);
    expect(inverterLabels(named, new Map()).get('a')).toBe('WR 1');
  });
  it('prefixes only repeated names on a single plane', () => {
    const named = [{ ...inv('a', 'p'), name: 'WR 1' }, { ...inv('b'), name: 'WR 1' }, { ...inv('c', 'p'), name: 'WR 2' }];
    const labels = inverterLabels(named, new Map([['p', 'Süd']]));
    expect([...labels.values()]).toEqual(['Süd · WR 1', 'WR 1', 'WR 2']);
  });
});
