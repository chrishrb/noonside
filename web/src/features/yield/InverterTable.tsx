import { useTranslation } from 'react-i18next';
import type { Inverter, Overview } from '@/api/types';
import { ShareBar, StatusCell, Swatch } from '@/components/ui/primitives';
import { Table } from '@/components/ui/Table';
import { fmtKWh, fmtKwp, fmtW, num } from '@/lib/format';

/** Yield per inverter for the selected period, with a totals row. */
export function InverterTable({ rows, invById, summary, label }: {
  rows: Overview['inverters']; invById: Map<string, Inverter>; summary: Overview['summary']; label: string;
}) {
  const { t } = useTranslation();
  const spec = (v?: number) => (v != null ? `${num(v, 2)} kWh/kWp` : '—');
  return (
    <Table stickyFirst caption={t('yield.inverters.caption', { label })}>
      <thead>
        <tr>
          <th scope="col">{t('yield.inverters.inverter')}</th><th scope="col" className="num">{t('yield.inverters.peak')}</th><th scope="col" className="num">{t('yield.inverters.energy')}</th>
          <th scope="col" className="num">{t('yield.inverters.specific')}</th><th scope="col" className="num">{t('yield.inverters.maxAc')}</th><th scope="col" className="num">{t('yield.inverters.share')}</th>
          <th scope="col">{t('yield.inverters.statusNow')}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.inverter_id}>
            <td>
              <span className="flex items-center gap-2">
                <Swatch color={r.color} />
                <span className="cell-main"><span className="font-medium whitespace-nowrap">{r.name}</span><span className="xs muted">{invById.get(r.inverter_id)?.model}</span></span>
              </span>
            </td>
            <td className="num">{fmtKwp(r.peak_power_kwp)}</td>
            <td className="num">{fmtKWh(r.energy_kwh)}</td>
            <td className="num">{spec(r.specific_yield_kwh_kwp)}</td>
            <td className="num">{fmtW(r.max_pac_w)}</td>
            <td className="num"><ShareBar pct={r.share_pct} color={r.color}>{num(r.share_pct, 1)} %</ShareBar></td>
            <td>
              <StatusCell statusText={r.status_text} errorText={r.error_text} />
              {r.error_text && <span className="xs muted block">{r.error_text}</span>}
            </td>
          </tr>
        ))}
      </tbody>
      {rows.length > 1 && (
        <tfoot>
          <tr>
            <td>{t('common.total')}</td>
            <td className="num">{fmtKwp(rows.reduce((a, r) => a + r.peak_power_kwp, 0))}</td>
            <td className="num">{fmtKWh(summary.energy_kwh)}</td>
            <td className="num">{spec(summary.specific_yield_kwh_kwp)}</td>
            <td /><td className="num">100 %</td><td />
          </tr>
        </tfoot>
      )}
    </Table>
  );
}
