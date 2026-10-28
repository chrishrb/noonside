import { useState } from 'react';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import type { DashboardSystem } from '@/api/types';
import { Icon } from '@/components/ui/Icon';
import { Table } from '@/components/ui/Table';
import { fmtKWh, fmtPct, fmtW, num } from '@/lib/format';
import { Freshness } from '@/components/data/Freshness';

type Key = 'name' | 'now' | 'today' | 'month' | 'year' | 'target' | 'pr' | 'alerts' | 'updated';
type Row = { s: DashboardSystem; values: Record<Exclude<Key, 'name'>, number | null> };

const perKwp = (kwh: number, kwp: number) => (kwp > 0 ? kwh / kwp : null);

function rowOf(s: DashboardSystem): Row {
  const kwp = s.peak_power_kwp;
  return {
    s,
    values: {
      now: kwp > 0 ? (s.pac_w / 1000 / kwp) * 100 : null,
      today: perKwp(s.today_kwh, kwp),
      month: perKwp(s.month_kwh, kwp),
      year: perKwp(s.year_kwh, kwp),
      target: s.month_target_to_date_kwh ? (s.month_kwh / s.month_target_to_date_kwh) * 100 : null,
      pr: s.month_performance_ratio_pct ?? null,
      alerts: s.open_alerts,
      updated: s.last_update ? Date.parse(s.last_update) : null,
    },
  };
}

/**
 * Systems side by side, normalized by peak power so different sizes compare.
 * Sorted by a column header; missing values sort last.
 */
export function FleetTable({ rows }: { rows: DashboardSystem[] }) {
  const { t } = useTranslation();
  const [sort, setSort] = useState<{ key: Key; desc: boolean }>({ key: 'month', desc: true });
  const data = rows.map(rowOf).sort((a, b) => {
    if (sort.key === 'name') return (sort.desc ? -1 : 1) * a.s.name.localeCompare(b.s.name);
    const x = a.values[sort.key], y = b.values[sort.key];
    if (x == null || y == null) return x == null ? (y == null ? 0 : 1) : -1;
    return (sort.desc ? y - x : x - y) || a.s.name.localeCompare(b.s.name);
  });
  const th = (key: Key, label: string, numeric = true) => {
    const active = sort.key === key;
    return (
      <th scope="col" className={numeric ? 'num' : undefined} aria-sort={active ? (sort.desc ? 'descending' : 'ascending') : undefined}>
        <button type="button" className="th-sort" onClick={() => setSort({ key, desc: active ? !sort.desc : key !== 'name' })}>
          {label}{active && <Icon name={sort.desc ? 'down' : 'up'} size="sm" />}
        </button>
      </th>
    );
  };
  const kwhKwp = (v: number | null) => (v == null ? '—' : num(v, 2));
  return (
    <Table caption={t('dashboard.fleet.caption')} stickyFirst>
      <thead>
        <tr>
          {th('name', t('common.name'), false)}
          {th('now', t('dashboard.fleet.now'))}
          {th('today', t('dashboard.fleet.today'))}
          {th('month', t('dashboard.fleet.month'))}
          {th('year', t('dashboard.fleet.year'))}
          {th('target', t('dashboard.fleet.target'))}
          {th('pr', t('dashboard.fleet.pr'))}
          {th('alerts', t('dashboard.fleet.alerts'))}
          {th('updated', t('dashboard.fleet.updated'))}
        </tr>
      </thead>
      <tbody>
        {data.map(({ s, values: v }) => (
          <tr key={s.system_id}>
            <td className="whitespace-nowrap"><Link className="font-medium" to={`/s/${s.system_id}/overview`}>{s.name}</Link></td>
            <td className="num" title={fmtW(s.pac_w)}>{v.now == null ? '—' : `${num(v.now, 0)} %`}</td>
            <td className="num" title={fmtKWh(s.today_kwh)}>{kwhKwp(v.today)}</td>
            <td className="num" title={fmtKWh(s.month_kwh)}>{kwhKwp(v.month)}</td>
            <td className="num" title={fmtKWh(s.year_kwh)}>{kwhKwp(v.year)}</td>
            <td className="num">{v.target == null ? '—' : `${num(v.target, 0)} %`}</td>
            <td className="num">{v.pr == null ? '—' : fmtPct(v.pr)}</td>
            <td className="num">
              {s.open_alerts ? <Link to={`/s/${s.system_id}/status`} className="status warn">{s.open_alerts}</Link> : <span className="muted">0</span>}
            </td>
            <td className="num sm muted whitespace-nowrap"><Freshness time={s.last_update} stale={s.stale} format={(when) => when} /></td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
