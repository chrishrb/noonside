import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { DailyEnergy } from '@/api/types';
import { fmtDay, fmtKWh, monthName, num, weekdayNames } from '@/lib/format';

const CELL = 11, GAP = 2, STEP = CELL + GAP, LEFT = 22, TOP = 14;
const WEEKS = 54;
export const HEAT_LEVELS = 6;

/** Day of the week with Monday = 0, for a YYYY-MM-DD string. */
const weekday = (y: number, m: number, d: number) => (new Date(Date.UTC(y, m - 1, d)).getUTCDay() + 6) % 7;
const daysInYear = (y: number) => (new Date(Date.UTC(y, 1, 29)).getUTCMonth() === 1 ? 366 : 365);

/**
 * Daily yield as a calendar: one row of weeks per year (newest first), days
 * Monday to Sunday top to bottom, shaded by kWh/kWp relative to the best days
 * (95th percentile), so poor days stand out. Days without data stay blank.
 */
export function CalendarHeatmap({ data, kwp, first, last, onPick }: {
  data: DailyEnergy; kwp: number; first: string; last: string; onPick?: (date: string) => void;
}) {
  const { t } = useTranslation();
  const perKwp = kwp > 0;
  const { byDate, scale } = useMemo(() => {
    const byDate = new Map(data.days.map((d) => [d.date, perKwp ? d.energy_kwh / kwp : d.energy_kwh]));
    const vals = [...byDate.values()].filter((v) => v > 0).sort((a, b) => a - b);
    return { byDate, scale: vals.length ? vals[Math.min(vals.length - 1, Math.floor(vals.length * 0.95))] : 1 };
  }, [data, kwp, perKwp]);
  const level = (v: number) => (v <= 0 ? 0 : Math.min(HEAT_LEVELS - 1, 1 + Math.floor((v / scale) * (HEAT_LEVELS - 2))));
  const fmtV = (v: number) => (perKwp ? `${num(v, 2)} kWh/kWp` : fmtKWh(v));

  const firstYear = Number(first.slice(0, 4)), lastYear = Number(last.slice(0, 4));
  const years: number[] = [];
  for (let y = lastYear; y >= firstYear; y--) years.push(y);
  const W = LEFT + WEEKS * STEP, H = TOP + 7 * STEP;

  return (
    <div className="heatmap">
      {years.map((y) => {
        const off = weekday(y, 1, 1);
        const cells: React.ReactElement[] = [];
        const months: React.ReactElement[] = [];
        for (let i = 0; i < daysInYear(y); i++) {
          const d = new Date(Date.UTC(y, 0, 1 + i));
          const m = d.getUTCMonth() + 1, day = d.getUTCDate();
          const date = `${y}-${String(m).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
          const col = Math.floor((i + off) / 7), row = (i + off) % 7;
          if (day === 1) months.push(<text key={m} x={LEFT + col * STEP} y={TOP - 4}>{monthName(m, 'short')}</text>);
          if (date < first || date > last) continue;
          const v = byDate.get(date);
          cells.push(
            <rect key={date} x={LEFT + col * STEP} y={TOP + row * STEP} width={CELL} height={CELL} rx={2}
              className={v == null ? 'hm-none' : `hm-${level(v)}`} onClick={v != null && onPick ? () => onPick(date) : undefined}
              style={v != null && onPick ? { cursor: 'pointer' } : undefined}>
              <title>{`${fmtDay(date)}: ${v == null ? t('yield.heatmap.noData') : fmtV(v)}`}</title>
            </rect>,
          );
        }
        const total = [...byDate.entries()].reduce((a, [d, v]) => (d.startsWith(`${y}-`) ? a + v : a), 0);
        return (
          <figure key={y} className="heatmap-year">
            <figcaption><strong>{y}</strong><span className="xs muted">{fmtV(total)}</span></figcaption>
            <div className="heatmap-scroll">
              <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={t('yield.heatmap.aria', { year: y, total: fmtV(total) })}>
                <g className="heatmap-axis">
                  {months}
                  {[0, 2, 4].map((r) => <text key={r} x={0} y={TOP + r * STEP + CELL - 2}>{weekdayNames('narrow')[r]}</text>)}
                </g>
                {cells}
              </svg>
            </div>
          </figure>
        );
      })}
      <div className="heatmap-legend xs muted" aria-hidden>
        <span>{t('yield.heatmap.less')}</span>
        {Array.from({ length: HEAT_LEVELS }, (_, i) => <span key={i} className={`hm-key hm-${i}`} />)}
        <span>{t('yield.heatmap.more')}</span>
        <span className="hm-key hm-none" style={{ marginLeft: 12 }} /><span>{t('yield.heatmap.noData')}</span>
      </div>
    </div>
  );
}
