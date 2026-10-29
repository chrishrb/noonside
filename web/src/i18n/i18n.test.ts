import { afterEach, describe, expect, it } from 'vitest';
import i18n, { detectLanguage, setLanguage } from '@/i18n';
import { LANGUAGE, store } from '@/lib/storage';
import { fmtDate, fmtDay, fmtRel, fmtW } from '@/lib/format';
import { periodLabel } from '@/lib/period';
import { en } from './en';
import { de } from './de';
import { es } from './es';

const keys = (o: object, p = ''): string[] =>
  Object.entries(o).flatMap(([k, v]) => (typeof v === 'object' ? keys(v, `${p}${k}.`) : [`${p}${k}`]));

afterEach(() => {
  store.set(LANGUAGE, null);
  return i18n.changeLanguage('en');
});

describe('i18n', () => {
  it('has the same keys in every language', () => {
    expect(keys(de).sort()).toEqual(keys(en).sort());
    expect(keys(es).sort()).toEqual(keys(en).sort());
  });

  it('prefers the stored choice, else the browser language', () => {
    store.set(LANGUAGE, 'de');
    expect(detectLanguage()).toBe('de');
    store.set(LANGUAGE, 'fr');
    expect(detectLanguage()).toBe('en'); // jsdom's navigator is en-US
  });

  it('switches language and formats numbers and dates for it', async () => {
    setLanguage('de');
    expect(store.get(LANGUAGE)).toBe('de');
    expect(i18n.language).toBe('de');
    expect(document.documentElement.lang).toBe('de');
    expect(fmtW(6120)).toBe('6,12 kW');
    expect(fmtDate('2025-06-03')).toBe('3. Juni 2025');
    expect(fmtDay('2025-06-03')).toMatch(/^Di\.?, 3\. Juni 2025$/);
    expect(periodLabel('month', '2025-06-03', '2020-01-01')).toBe('Juni 2025');
    expect(fmtRel('2025-06-01T12:00:00Z', Date.parse('2025-06-03T12:30:00Z'))).toBe('vor 2 Tagen');
    expect(i18n.t('counts.system', { count: 1 })).toBe('1 Anlage');
  });

  it('switches to Spanish and formats numbers and dates for it', async () => {
    setLanguage('es');
    expect(i18n.language).toBe('es');
    expect(document.documentElement.lang).toBe('es');
    expect(fmtW(6120)).toBe('6,12 kW');
    expect(fmtRel('2025-06-01T12:00:00Z', Date.parse('2025-06-03T12:30:00Z'))).toBe('hace 2 días');
    expect(i18n.t('counts.system', { count: 1 })).toBe('1 instalación');
    expect(fmtDate('2025-06-03')).toBe('3 jun 2025');
    expect(fmtDay('2025-06-03')).toBe('mar, 3 jun 2025');
    expect(periodLabel('month', '2025-06-03', '2020-01-01')).toBe('junio de 2025');
  });
});
