// Number, energy and date formatting. Times are shown in the system's time
// zone (pass `tz`); plain dates (YYYY-MM-DD) are calendar dates and never shifted.
// Numbers, month and weekday names follow the active UI language.

import i18n, { currentLocale } from '@/i18n';

const nfCache = new Map<string, Intl.NumberFormat>();
export function num(v: number, digits: number): string {
  const key = `${currentLocale()}|${digits}`;
  let f = nfCache.get(key);
  if (!f) {
    f = new Intl.NumberFormat(currentLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
    nfCache.set(key, f);
  }
  return f.format(v);
}

const missing = (v: number | null | undefined): v is null | undefined => v == null || Number.isNaN(v);

export function fmtW(w: number | null | undefined): string {
  if (missing(w)) return '—';
  const a = Math.abs(w);
  if (a < 1000) return `${num(w, 0)} W`;
  if (a < 1e6) return `${num(w / 1000, a < 10000 ? 2 : 1)} kW`;
  return `${num(w / 1e6, 2)} MW`;
}

export function fmtKWh(k: number | null | undefined): string {
  if (missing(k)) return '—';
  const a = Math.abs(k);
  if (a < 10) return `${num(k, 2)} kWh`;
  if (a < 10000) return `${num(k, 1)} kWh`;
  if (a < 1e6) return `${num(k / 1000, 2)} MWh`;
  return `${num(k / 1e6, 2)} GWh`;
}

export const fmtKg = (kg: number | null | undefined) =>
  missing(kg) ? '—' : kg < 1000 ? `${num(kg, 0)} kg` : `${num(kg / 1000, kg < 100000 ? 2 : 1)} t`;
export const fmtMoney = (v: number | null | undefined, cur?: string) =>
  missing(v) ? '—' : new Intl.NumberFormat(currentLocale(), { style: 'currency', currency: cur || 'EUR' }).format(v);
/** "a, b and c" in the current language. */
export const fmtList = (items: string[]) => new Intl.ListFormat(currentLocale(), { type: 'conjunction' }).format(items);
export const fmtPct = (v: number | null | undefined) => (missing(v) ? '—' : `${num(v, 1)} %`);
export const fmtKwp = (v: number | null | undefined) => (missing(v) ? '—' : `${num(v, 2)} kWp`);
/** Storage sizes in binary units (MB = 2^20 bytes, as the quota is set). */
export function fmtBytes(b: number | null | undefined): string {
  if (missing(b)) return '—';
  if (b < 1 << 20) return `${num(b / 1024, 0)} KB`;
  if (b < 1 << 30) return `${num(b / (1 << 20), b < 10 << 20 ? 1 : 0)} MB`;
  return `${num(b / (1 << 30), 1)} GB`;
}

/** Splits "12.3 kWh" into value and unit, for KPI rendering. */
export function splitUnit(s: string): [string, string | null] {
  const i = s.lastIndexOf(' ');
  return i < 0 ? [s, null] : [s.slice(0, i), s.slice(i + 1)];
}

// Calendar dates are formatted as UTC instants so Intl never shifts them.
const dfCache = new Map<string, Intl.DateTimeFormat>();
function dateFmt(opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const key = `${currentLocale()}|${JSON.stringify(opts)}`;
  let f = dfCache.get(key);
  if (!f) {
    f = new Intl.DateTimeFormat(currentLocale(), { ...opts, timeZone: 'UTC' });
    dfCache.set(key, f);
  }
  return f;
}
const utc = (year: number, month: number, day = 1) => new Date(Date.UTC(year, month - 1, day));

/** Month name in the active language (month 1–12). */
export const monthName = (month: number, width: 'long' | 'short' = 'long') => dateFmt({ month: width }).format(utc(2025, month));
/** The twelve month names in the active language. */
export const monthNames = (width: 'long' | 'short' = 'long') => Array.from({ length: 12 }, (_, i) => monthName(i + 1, width));
/** "June 2025" / "Juni 2025". */
export const fmtMonth = (year: number, month: number) => dateFmt({ month: 'long', year: 'numeric' }).format(utc(year, month));

/** Weekday names in the active language, Monday first. */
export const weekdayNames = (width: 'short' | 'narrow' = 'short') =>
  Array.from({ length: 7 }, (_, i) => dateFmt({ weekday: width }).format(utc(2025, 1, 6 + i)));
/** "Tuesday, 3 June 2025", for screen readers. */
export const fmtDayLong = (s: string) => {
  const p = parseYmd(s);
  return dateFmt({ weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' }).format(utc(p.year, p.month, p.day));
};

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export const pad = (n: number) => String(n).padStart(2, '0');

/** Calendar parts of an instant in a time zone (browser zone if omitted). */
export interface Parts { year: number; month: number; day: number; hour: number; minute: number; weekday: number }
const partsFmt = new Map<string, Intl.DateTimeFormat>();
export function partsIn(d: Date, tz?: string): Parts {
  const key = tz ?? '';
  let f = partsFmt.get(key);
  if (!f) {
    f = new Intl.DateTimeFormat('en-US', {
      timeZone: tz, hourCycle: 'h23', year: 'numeric', month: 'numeric', day: 'numeric', hour: 'numeric', minute: 'numeric', weekday: 'short',
    });
    partsFmt.set(key, f);
  }
  const p: Record<string, string> = {};
  for (const x of f.formatToParts(d)) p[x.type] = x.value;
  return {
    year: Number(p.year), month: Number(p.month), day: Number(p.day), hour: Number(p.hour) % 24, minute: Number(p.minute),
    weekday: WEEKDAYS.indexOf(p.weekday),
  };
}

// Plain calendar dates ---------------------------------------------------------
export interface YMD { year: number; month: number; day: number }
export function parseYmd(s: string): YMD {
  const [year, month, day] = s.slice(0, 10).split('-').map(Number);
  return { year, month, day };
}
export const ymd = (d: YMD) => `${d.year}-${pad(d.month)}-${pad(d.day)}`;
export const todayIn = (tz?: string) => ymd(partsIn(new Date(), tz));
export const daysInMonth = (year: number, month: number) => new Date(Date.UTC(year, month, 0)).getUTCDate();
/** Adds days/months to a calendar date (months clamp to the month's last day). */
export function addDays(s: string, n: number): string {
  const { year, month, day } = parseYmd(s);
  const d = new Date(Date.UTC(year, month - 1, day + n));
  return ymd({ year: d.getUTCFullYear(), month: d.getUTCMonth() + 1, day: d.getUTCDate() });
}

const isDateOnly = (v: string) => /^\d{4}-\d{2}-\d{2}$/.test(v);

/** "3 Jun 2025". Date-only strings are taken as is; instants are shown in tz. */
export function fmtDate(v: string | Date | null | undefined, tz?: string): string {
  if (!v) return '—';
  const p = typeof v === 'string' && isDateOnly(v) ? parseYmd(v) : partsIn(new Date(v), tz);
  return dateFmt({ day: 'numeric', month: 'short', year: 'numeric' }).format(utc(p.year, p.month, p.day));
}
/** "Tue, 3 Jun 2025". */
export function fmtDay(v: string | Date, tz?: string): string {
  const p = typeof v === 'string' && isDateOnly(v) ? parseYmd(v) : partsIn(new Date(v), tz);
  return dateFmt({ weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' }).format(utc(p.year, p.month, p.day));
}
export function fmtTime(v: string | Date | null | undefined, tz?: string): string {
  if (!v) return '—';
  const p = partsIn(new Date(v), tz);
  return `${pad(p.hour)}:${pad(p.minute)}`;
}
export const fmtDateTime = (v: string | Date | null | undefined, tz?: string) => (v ? `${fmtDate(v, tz)}, ${fmtTime(v, tz)}` : '—');

export function fmtRel(v: string | null | undefined, now = Date.now()): string {
  const t = i18n.t;
  if (!v) return t('time.never');
  const s = (now - new Date(v).getTime()) / 1000;
  if (s < 60) return t('time.justNow');
  if (s < 3600) return t('time.minAgo', { n: Math.floor(s / 60) });
  if (s < 86400) return t('time.hAgo', { n: Math.floor(s / 3600) });
  if (s < 172800) return t('time.yesterday');
  if (s < 30 * 86400) return t('time.daysAgo', { count: Math.floor(s / 86400) });
  return fmtDate(v);
}

export function fmtDuration(ms: number): string {
  const m = Math.round(ms / 60000);
  if (m < 60) return i18n.t('time.min', { n: m });
  const h = Math.floor(m / 60), r = m % 60;
  return r ? i18n.t('time.hMin', { h, m: r }) : i18n.t('time.h', { n: h });
}

export const every = (s: number) =>
  s % 3600 === 0 ? i18n.t('time.everyH', { n: s / 3600 }) : s % 60 === 0 ? i18n.t('time.everyMin', { n: s / 60 }) : i18n.t('time.everyS', { n: s });

export const initials = (name: string | null | undefined) =>
  String(name || '?').split(/\s+/).filter(Boolean).slice(0, 2).map((p) => p[0].toUpperCase()).join('') || '?';

export const cap = (s: string | null | undefined) => {
  const t = String(s || '');
  return t.charAt(0).toUpperCase() + t.slice(1);
};

/** The instant of local midnight at the start of `date` in `tz`, as ISO string. */
export function zonedMidnight(date: string, tz: string): string {
  const { year, month, day } = parseYmd(date);
  const want = Date.UTC(year, month - 1, day);
  let t = want;
  // Two rounds settle DST transitions.
  for (let i = 0; i < 2; i++) {
    const p = partsIn(new Date(t), tz);
    const seen = Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute);
    t += want - seen;
  }
  return new Date(t).toISOString();
}
