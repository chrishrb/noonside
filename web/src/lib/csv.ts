import i18n from '@/i18n';

/** Splits one CSV line, honouring double quotes. */
export function csvSplit(line: string): string[] {
  const out: string[] = [];
  let cur = '', quoted = false;
  for (const ch of line) {
    if (ch === '"') quoted = !quoted;
    else if (ch === ',' && !quoted) { out.push(cur); cur = ''; }
    else cur += ch;
  }
  out.push(cur);
  return out;
}

export interface DailyRow { inverter: string; date: string; energy_wh: number; pac_max_w?: number }

/**
 * Parses "inverter,date,energy_wh[,pac_max_w]" rows (comma, semicolon or tab
 * separated). A first line that isn't numeric in the energy column is a header.
 */
export function parseDailyCsv(text: string): { rows: DailyRow[] } | { error: string } {
  const rows: DailyRow[] = [];
  const lines = text.split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
  for (let n = 0; n < lines.length; n++) {
    const l = lines[n];
    const p = l.split(/[;,\t]/).map((x) => x.trim());
    if (n === 0 && Number.isNaN(Number(p[2]))) continue;
    if (p.length < 3 || !p[0] || !/^\d{4}-\d{2}-\d{2}$/.test(p[1]) || p[2] === '' || Number.isNaN(Number(p[2]))) {
      return { error: i18n.t('csv.badLine', { line: n + 1, text: l.slice(0, 40) }) };
    }
    const row: DailyRow = { inverter: p[0], date: p[1], energy_wh: Number(p[2]) };
    if (p[3]) {
      if (Number.isNaN(Number(p[3]))) return { error: i18n.t('csv.badPeak', { line: n + 1 }) };
      row.pac_max_w = Number(p[3]);
    }
    rows.push(row);
  }
  if (!rows.length) return { error: i18n.t('csv.empty') };
  return { rows };
}
