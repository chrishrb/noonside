import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import type { Inverter, Overview, Period } from '@/api/types';
import { Icon } from '@/components/ui/Icon';
import { Disclosure } from '@/components/ui/misc';
import { Table } from '@/components/ui/Table';
import { fmtKWh, fmtPct, num } from '@/lib/format';
import { bucketLabel, bucketName, chartTitle } from '@/lib/period';

interface Props {
  period: Period; tz: string; label: string; ts: string[]; kwp: number;
  total: (number | null)[]; target?: (number | null)[]; pr?: (number | null)[]; corrected?: boolean[]; summary: Overview['summary'];
  inverters: Overview['inverters']; invById: Map<string, Inverter>;
}

const sum = (vs: (number | null | undefined)[]) => vs.reduce<number>((a, v) => a + (v || 0), 0);
const spec = (kwh: number, kwp: number) => (kwp > 0 ? `${num(kwh / kwp, 1)} kWh/kWp` : '—');
const vsTarget = (actual: number, target: number) => {
  if (!(target > 0)) return <>—</>;
  const pct = (actual / target) * 100;
  const color = pct >= 100 ? 'var(--success-text)' : pct < 90 ? 'var(--danger-text)' : undefined;
  return <span style={{ color }}>{fmtPct(pct)}</span>;
};

/** The period's values as a table (collapsible, remembered in ?table=1): per inverter for a day, per bucket otherwise. */
export function DataTable(props: Props) {
  const { t } = useTranslation();
  const { period } = props;
  const [search, setSearch] = useSearchParams();
  const open = search.get('table') === '1';
  const count = period === 'day' ? props.inverters.length : bucketRows(props).length;

  return (
    <Disclosure
      open={open}
      onToggle={(o) => {
        if (o === open) return;
        const q = new URLSearchParams(search);
        if (o) q.set('table', '1'); else q.delete('table');
        setSearch(q, { replace: true, preventScrollReset: true });
      }}
      summary={<>{t('yield.table.title')}<span className="muted font-normal">· {period === 'day' ? t('yield.table.inverters', { count }) : t('yield.table.rows', { count })}</span></>}
    >
      {!count ? <p className="sm muted" style={{ padding: '0 20px 16px' }}>{t('yield.table.none')}</p>
        : period === 'day' ? <InverterRows {...props} /> : <BucketRows {...props} />}
    </Disclosure>
  );
}

/** Buckets that have started, i.e. not in the future. */
function bucketRows({ ts, total }: Props) {
  const now = Date.now();
  return ts.map((_, i) => i).filter((i) => total[i] != null && Date.parse(ts[i]) <= now);
}

function InverterRows({ label, inverters, invById, summary }: Props) {
  const { t } = useTranslation();
  const ac = sum(inverters.filter((r) => r.dc_energy_kwh != null).map((r) => r.energy_kwh));
  const dc = sum(inverters.map((r) => r.dc_energy_kwh));
  return (
    <Table scroll stickyFirst caption={t('yield.inverters.caption', { label })}>
      <thead>
        <tr>
          <th scope="col">{t('yield.inverters.inverter')}</th><th scope="col" className="num">{t('yield.table.acEnergy')}</th>
          <th scope="col" className="num">{t('yield.table.dcEnergy')}</th><th scope="col" className="num">{t('yield.table.efficiency')}</th>
        </tr>
      </thead>
      <tbody>
        {inverters.map((r) => (
          <tr key={r.inverter_id}>
            <td><span className="cell-main"><span className="font-medium whitespace-nowrap">{r.name}</span><span className="xs muted">{invById.get(r.inverter_id)?.serial_number}</span></span></td>
            <td className="num"><span className="cell-main"><span>{fmtKWh(r.energy_kwh)}</span><span className="xs muted">{fmtPct(r.share_pct)}</span></span></td>
            <td className="num">{fmtKWh(r.dc_energy_kwh)}</td>
            <td className="num">{fmtPct(r.efficiency_pct)}</td>
          </tr>
        ))}
      </tbody>
      {inverters.length > 1 && (
        <tfoot>
          <tr>
            <td>{t('common.total')}</td>
            <td className="num">{fmtKWh(summary.energy_kwh)}</td>
            <td className="num">{dc > 0 ? fmtKWh(dc) : '—'}</td>
            <td className="num">{dc > 0 ? fmtPct((ac / dc) * 100) : '—'}</td>
          </tr>
        </tfoot>
      )}
    </Table>
  );
}

function BucketRows(props: Props) {
  const { t } = useTranslation();
  const { period, tz, label, ts, kwp, total, target, pr, corrected } = props;
  const rows = bucketRows(props);
  const energy = sum(rows.map((i) => total[i]));
  const targetSum = target ? sum(rows.map((i) => target[i])) : 0;
  const weather = props.summary.target_source === 'weather';
  return (
    <Table scroll stickyFirst caption={t('yield.table.caption', { title: chartTitle(period), label })}>
      <thead>
        <tr>
          <th scope="col">{bucketName(period)}</th><th scope="col" className="num">{t('yield.inverters.energy')}</th>
          <th scope="col" className="num">{t('yield.inverters.specific')}</th>
          {target && <><th scope="col" className="num">{t(weather ? 'common.expected' : 'common.target')}</th><th scope="col" className="num">{t(weather ? 'yield.table.vsExpected' : 'yield.table.vsTarget')}</th></>}
          {pr && <th scope="col" className="num"><abbr title={t('kpi.pr')}>{t('yield.table.pr')}</abbr></th>}
        </tr>
      </thead>
      <tbody>
        {rows.map((i) => (
          <tr key={ts[i]}>
            <td className="whitespace-nowrap">{bucketLabel(period, ts[i], tz)}</td>
            <td className="num font-medium whitespace-nowrap">
              {corrected?.[i] && (
                <span className="muted mr-1 inline-flex align-[-2px]" title={t('yield.correct.correctedDay')}>
                  <Icon name="edit" size="sm" /><span className="sr-only">{t('yield.correct.correctedDay')}</span>
                </span>
              )}
              {fmtKWh(total[i])}
            </td>
            <td className="num">{spec(total[i]!, kwp)}</td>
            {target && <><td className="num">{fmtKWh(target[i])}</td><td className="num">{vsTarget(total[i]!, target[i] ?? 0)}</td></>}
            {pr && <td className="num">{fmtPct(pr[i])}</td>}
          </tr>
        ))}
      </tbody>
      <tfoot>
        <tr>
          <td>{t('common.total')}</td>
          <td className="num">{fmtKWh(energy)}</td>
          <td className="num">{spec(energy, kwp)}</td>
          {target && <><td className="num">{fmtKWh(targetSum)}</td><td className="num">{vsTarget(energy, targetSum)}</td></>}
          {pr && <td className="num">{fmtPct(props.summary.performance_ratio_pct)}</td>}
        </tr>
      </tfoot>
    </Table>
  );
}
