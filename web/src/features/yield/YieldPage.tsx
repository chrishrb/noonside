import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import { getDailyEnergyOptions, getLiveOptions, getOverviewOptions } from '@/api/gen/@tanstack/react-query.gen';
import { useInverters, useSystem } from '@/api/queries';
import type { Inverter, Live, Overview, Period, System } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { can } from '@/auth/scopes';
import { Content, PageHeader, QueryView, SectionHead } from '@/components/layout/Page';
import { ChartLegend, EnergyChart, type ChartSeries, type CompareSeries } from '@/components/data/EnergyChart';
import { CalendarHeatmap } from '@/components/data/CalendarHeatmap';
import { LineChart } from '@/components/data/LineChart';
import { DateNav } from '@/components/data/DateNav';
import { KpiRow, KpiTile } from '@/components/data/KpiTile';
import { Button, ButtonLink } from '@/components/ui/Button';
import { Chip, Segmented } from '@/components/ui/form';
import { Icon } from '@/components/ui/Icon';
import { Card, EmptyState, RoleBadge } from '@/components/ui/primitives';
import { addDays, fmtDate, fmtKg, fmtKWh, fmtKwp, fmtMoney, fmtPct, fmtTime, fmtW, num, todayIn } from '@/lib/format';
import { FINER, PERIOD_KEYS, bestLabel, bucketLabel, bucketTick, alignTo, chartTitle, dayAxis, drillDown, isPeriod, periodLabel, periodName, periodStart } from '@/lib/period';
import { inverterLabels, isPlaneSelected, planeMembers, planeName } from '@/lib/planes';
import { commissioned } from '@/lib/system';
import { expectedAt } from '@/lib/weather';
import { priceAt } from '@/lib/prices';
import { DataTable } from './DataTable';
import { EditDayDialog } from './EditDayDialog';
import { InverterFilter } from './InverterFilter';
import { InverterTable } from './InverterTable';
import { Freshness } from '@/components/data/Freshness';

export function YieldPage() {
  const sid = useParams().sid!;
  const sys = useSystem(sid);
  const invs = useInverters(sid);
  return (
    <QueryView queries={[sys, invs]}>
      {() => <Yield sys={sys.data!} invs={[...invs.data!].sort((a, b) => a.sort_order - b.sort_order)} />}
    </QueryView>
  );
}

function Yield({ sys, invs }: { sys: System; invs: Inverter[] }) {
  const { t } = useTranslation();
  const me = useMeData();
  const sid = sys.id;
  const [search, setSearch] = useSearchParams();
  const period: Period = isPeriod(search.get('period')) ? (search.get('period') as Period) : 'day';
  const date = search.get('date') ?? undefined;
  const sel = (search.get('inv') ?? '').split(',').filter((id) => invs.some((i) => i.id === id));
  const compare = compareOf(search.get('cmp'), period);

  const overview = useQuery({ ...getOverviewOptions({
    path: { systemId: sid }, query: { period, date, inverters: sel.length ? sel : undefined, compare },
  }), enabled: invs.length > 0, placeholderData: keepPreviousData, refetchInterval: period === 'day' && !date ? 60_000 : false });
  const live = useQuery({ ...getLiveOptions({ path: { systemId: sid } }), enabled: invs.length > 0 && period === 'day', refetchInterval: 60_000 });

  const header = (
    <PageHeader
      eyebrow={t('nav.yield')}
      title={sys.name}
      sub={`${sys.location || t('common.noLocation')} · ${fmtKwp(sys.peak_power_kwp)} · ${t('yield.commissioned', { date: fmtDate(commissioned(sys)) })}`}
      // Phones have the export tab; the role is on the profile page.
      actionsClassName="max-md:hidden"
      actions={<><RoleBadge role={sys.role} />{can(me, sid, 'data:export') && <ButtonLink to={`/s/${sid}/export`} icon="download">{t('yield.exportCsv')}</ButtonLink>}</>}
    />
  );

  if (!invs.length) {
    return (
      <Content>
        {header}
        <Card>
          <EmptyState icon="bolt" title={t('yield.noInverters.title')} text={t('yield.noInverters.text')}
            action={can(me, sid, 'inverter:write') && <ButtonLink variant="primary" icon="plus" to={`/s/${sid}/settings/inverters`}>{t('inverters.add')}</ButtonLink>} />
        </Card>
      </Content>
    );
  }

  const update = (patch: Record<string, string | undefined>) => {
    const next = new URLSearchParams(search);
    for (const [k, v] of Object.entries(patch)) { if (v) next.set(k, v); else next.delete(k); }
    setSearch(next);
  };

  return (
    <Content>
      {header}
      <QueryView queries={[overview]} inline>
        {() => <YieldBody sys={sys} invs={invs} ov={overview.data!} live={live.data} sel={sel} update={update} stale={overview.isPlaceholderData} />}
      </QueryView>
    </Content>
  );
}

type Compare = 'previous_year' | 'all_years';
/** The comparison chosen in ?cmp= (month and year views only). */
const compareOf = (v: string | null, period: Period): Compare | undefined =>
  period !== 'month' && period !== 'year' ? undefined : v === 'prev' ? 'previous_year' : v === 'all' ? 'all_years' : undefined;

interface BodyProps {
  sys: System; invs: Inverter[]; ov: Overview; live?: Live; sel: string[]; stale: boolean;
  update: (patch: Record<string, string | undefined>) => void;
}

function YieldBody({ sys, invs, ov, live, sel, update, stale }: BodyProps) {
  const { t } = useTranslation();
  const me = useMeData();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const [editing, setEditing] = useState(false);
  const sid = sys.id, tz = sys.timezone, period = ov.period;
  const canEdit = period === 'day' && can(me, sid, 'data:edit');
  const first = ov.first_date;
  const isCurrent = !ov.next_date;
  const label = periodLabel(period, ov.date, first);
  const invById = new Map(invs.map((i) => [i.id, i]));
  const planes = sys.planes.map((p, k) => ({ p, name: planeName(p, k, (n) => t('system.planes.fallback', { n })), ids: planeMembers(p, invs) }));
  const labels = inverterLabels(invs, new Map(planes.map((x) => [x.p.id ?? '', x.name])));
  const invRows = ov.inverters.map((r) => ({ ...r, name: labels.get(r.inverter_id) ?? r.name }));

  const hrefFor = (d: string | undefined, p: Period = period) => {
    const q = new URLSearchParams(search);
    q.set('period', p);
    if (d) q.set('date', d); else q.delete('date');
    return `/s/${sid}/overview?${q}`;
  };

  const byWeather = ov.summary.target_source === 'weather';

  // Chart data. Day curves always span 04:00–22:00 local time on a 5-minute
  // grid, whatever readings exist; month and year show all their buckets.
  const ts = period === 'day' ? dayAxis(ov.range_start, tz) : ov.timestamps;
  const align = <T,>(arr: T[]) => (period === 'day' ? alignTo(ts, ov.timestamps, arr) : arr.map((v) => v ?? null)) as (T | null)[];
  const total = align(ov.total);
  const seriesAll: ChartSeries[] = ov.series.map((s) => ({
    key: s.inverter_id, name: labels.get(s.inverter_id) ?? t('yield.inverters.inverter'), color: invById.get(s.inverter_id)?.color || '#6b6b6b', values: align(s.values),
  }));
  // "Total" sums the shown inverters into one series; "per inverter" stacks them.
  const split = invs.length > 1 && search.get('view') === 'split';
  const series = split ? seriesAll : [{ key: 'total', name: t('common.total'), color: 'var(--accent)', values: total }];
  const target = ov.target ? align(ov.target) : undefined;
  const pr = period !== 'day' && ov.performance_ratio?.some((v) => v != null) ? align(ov.performance_ratio) : undefined;
  const cmp = compareOf(search.get('cmp'), period);
  const compare: CompareSeries[] | undefined = cmp && ov.comparison?.map((c) => ({ key: String(c.year), name: String(c.year), values: c.values }));
  const fmt = period === 'day' ? fmtW : fmtKWh;
  const sm = ov.summary;
  const finer = FINER[period];
  const drill = (i: number) => {
    const to = drillDown(period, ts[i], tz);
    if (to) navigate(hrefFor(to.date, to.period));
  };
  const kwp = sel.length ? invs.filter((i) => sel.includes(i.id)).reduce((a, i) => a + i.peak_power_kwp, 0) : sys.peak_power_kwp;
  const expected = period === 'day' && ov.expected?.length ? expectedAt(ov.expected, ts) : undefined;
  // Day-ahead prices (with a price zone); ?price=off hides them.
  const hasPrices = period === 'day' && !!ov.prices?.length;
  const showPrices = hasPrices && search.get('price') !== 'off';
  const price = showPrices ? priceAt(ov.prices!, ts) : undefined;
  const negativePrice = !!price?.some((v) => v != null && v < 0);
  const negKwh = sm.negative_price_kwh, negHours = sm.negative_price_hours;
  const negNote = (negKwh != null && negKwh > 0) || (negHours != null && negHours > 0);
  // The energy tile already shows the actual value, so this one only names the reference.
  const vsTile = (
    <KpiTile label={byWeather ? t('kpi.vsExpected') : t('kpi.vsTarget')} icon="target" value={sm.actual_pct != null ? fmtPct(sm.actual_pct) : '—'}
      sub={sm.target_to_date_kwh ? t(byWeather ? 'kpi.expectedValue' : 'kpi.targetValue', { value: fmtKWh(sm.target_to_date_kwh) }) : t('kpi.noTarget')}
      progress={sm.actual_pct} progressTone={sm.actual_pct == null ? undefined : sm.actual_pct >= 100 ? 'ok' : sm.actual_pct < 90 ? 'low' : undefined} />
  );

  // With planes, the checklist lists inverters per plane, so repeated names stay apart.
  const shownPlanes = planes.filter((x) => x.ids.length);
  const onPlane = new Set(shownPlanes.flatMap((x) => x.ids));
  const sections = shownPlanes.length
    ? [...shownPlanes.map((x) => ({ key: x.p.id!, label: x.name, invs: invs.filter((i) => x.ids.includes(i.id)) })),
      { key: 'other', label: t('yield.noPlane'), invs: invs.filter((i) => !onPlane.has(i.id)) }].filter((r) => r.invs.length)
    : [{ key: 'all', label: '', invs }];

  const selPlane = sel.length ? shownPlanes.find((x) => isPlaneSelected(x.p, invs, sel)) : undefined;
  const filterSummary = !sel.length ? t('yield.allInverters') : selPlane ? selPlane.name
    : sel.length === 1 ? (invs.find((i) => i.id === sel[0])?.name ?? '') : t('counts.inverter', { count: sel.length });

  const livePower = live && (sel.length ? live.inverters.filter((i) => sel.includes(i.inverter_id)).reduce((s, i) => s + (i.pac_w || 0), 0) : live.pac_w);

  return (
    <div style={{ opacity: stale ? 0.6 : 1, transition: 'opacity 150ms' }} aria-busy={stale}>
      <div className="toolbar">
        <nav className="seg" aria-label={t('yield.period')}>
          {PERIOD_KEYS.map((k) => (
            <Link key={k} to={hrefFor(undefined, k)} aria-current={k === period ? 'page' : undefined}>{periodName(k)}</Link>
          ))}
        </nav>
        <DateNav period={period} date={ov.date} label={label} prevDate={ov.prev_date} nextDate={ov.next_date}
          prevDisabled={ov.prev_date < periodStart(period, first)} min={first} max={todayIn(tz)} hrefFor={hrefFor} onPick={(d) => navigate(hrefFor(d))} />
      </div>

      {invs.length > 1 && (
        <InverterFilter summary={filterSummary} sections={sections} invs={invs} sel={sel}
          onChange={(next) => update({ inv: next.join(',') || undefined })}>
          <Segmented label={t('yield.view.label')} value={split ? 'split' : 'total'} onChange={(v) => update({ view: v === 'split' ? v : undefined })}
            options={[['total', t('yield.view.total'), 'total'], ['split', t('yield.view.split'), 'layers']]} />
        </InverterFilter>
      )}

      <KpiRow>
        <KpiTile label={period === 'day' && isCurrent ? t('kpi.energyToday') : t('yield.inverters.energy')} icon="sun" value={fmtKWh(sm.energy_kwh)}
          sub={[sm.specific_yield_kwh_kwp != null && `${num(sm.specific_yield_kwh_kwp, 2)} kWh/kWp`, period === 'day' && sm.performance_ratio_pct != null && t('kpi.prShort', { pct: fmtPct(sm.performance_ratio_pct) })].filter(Boolean).join(' · ') || undefined} />
        {period === 'day' ? (
          <>
            {live && isCurrent && <KpiTile label={t('kpi.currentPower')} icon="bolt" value={fmtW(livePower)}
              sub={live.time ? <Freshness time={live.time} stale={live.stale} tz={tz} format={(when) => `${t('kpi.reading', { time: fmtTime(live.time, tz) })} · ${when}`} /> : t('kpi.noReading')} />}
            <KpiTile label={t('yield.inverters.peak')} icon="trophy" value={fmtW(sm.max_value ?? 0)} sub={sm.max_at ? t('kpi.at', { time: fmtTime(sm.max_at, tz) }) : t('kpi.noProduction')} />
            {byWeather && vsTile}
          </>
        ) : (
          <>
            {vsTile}
            <KpiTile label={bestLabel(period)} icon="trophy" value={fmtKWh(sm.max_value ?? 0)} sub={sm.max_at ? bucketLabel(period, sm.max_at, tz) : '—'} />
            {sm.performance_ratio_pct != null && (
              <KpiTile label={t('kpi.pr')} icon="activity" value={fmtPct(sm.performance_ratio_pct)}
                sub={sm.performance_ratio_trend_pct != null
                  ? t('kpi.prTrend', { value: `${sm.performance_ratio_trend_pct > 0 ? '+' : ''}${num(sm.performance_ratio_trend_pct, 1)}` })
                  : t('kpi.irradiation', { value: num(sm.irradiation_kwh_m2 ?? 0, 0) })} />
            )}
          </>
        )}
        <KpiTile label={t('kpi.revenue')} icon="coins" value={<>{fmtMoney(sm.revenue, sm.currency)}</>} sub={t('kpi.feedIn', { tariff: num(sys.tariff_per_kwh, 3), currency: sys.currency })} />
        <KpiTile label={t('kpi.co2')} icon="leaf" value={fmtKg(sm.co2_avoided_kg)} sub={t('kpi.lifetimeValue', { value: fmtKg(sm.lifetime_co2_avoided_kg) })} />
      </KpiRow>

      <section className="section card card-flush" aria-labelledby="chart-h" style={{ marginTop: 16 }}>
        <div className="chart-card">
          <div className="chart-head">
            <h2 id="chart-h">{chartTitle(period)}</h2>
            <ChartLegend series={series} target={!!target} targetLabel={byWeather ? t('common.expected') : undefined} expected={!!expected} compare={compare}
              price={!!price} negativePrice={negativePrice} />
          </div>
          {hasPrices && (
            <div className="chart-tools">
              <Chip pressed={showPrices} onClick={() => update({ price: showPrices ? 'off' : undefined })}>{t('yield.prices.toggle')}</Chip>
              {showPrices && ov.prices_source && <span className="xs muted">{t('yield.prices.source', { source: ov.prices_source })}</span>}
            </div>
          )}
          {(period === 'month' || period === 'year') && (
            <div className="chart-tools">
              <Segmented label={t('yield.compare.label')} value={search.get('cmp') === 'all' ? 'all' : search.get('cmp') === 'prev' ? 'prev' : 'off'}
                onChange={(v) => update({ cmp: v === 'off' ? undefined : v })}
                options={[['off', t('yield.compare.off')], ['prev', t('yield.compare.prev')], ['all', t('yield.compare.all')]]} />
              {compare && !compare.length && <span className="sm muted">{t('yield.compare.none')}</span>}
            </div>
          )}
          <EnergyChart
            kind={period === 'day' ? 'area' : 'bar'} unit={ov.unit} series={series} total={total} target={target} targetLabel={byWeather ? t('common.expected') : undefined} expected={expected} compare={compare}
            price={price} priceFmt={(v) => `${num(v, 1)} ct/kWh`}
            labels={ts.map((x) => bucketLabel(period, x, tz))} fmt={fmt}
            tick={(i, band) => bucketTick(period, ts[i], tz, band)}
            ariaLabel={t('yield.chartAria', { title: chartTitle(period), label, total: fmtKWh(sm.energy_kwh), drill: finer ? t(`yield.drillAria.${finer}`) : '' })}
            emptyText={period === 'day' ? t('yield.emptyDay') : t('yield.emptyPeriod')}
            onSelect={finer ? drill : undefined}
            selectHint={finer ? t(`yield.drillHint.${finer}`) : undefined}
          />
          {negNote && (
            <p className="sm chart-note flex flex-wrap items-center gap-x-2 gap-y-1">
              <Icon name="coins" size="sm" />
              <span>{t('yield.prices.negative', { energy: fmtKWh(negKwh ?? 0), hours: num(negHours ?? 0, 1) })}</span>
              {sm.unpaid_revenue != null && sm.unpaid_revenue > 0 && <span className="text-danger-text">· {t('yield.prices.unpaid', { value: fmtMoney(sm.unpaid_revenue, sm.currency) })}</span>}
            </p>
          )}
          {sm.corrected && (
            <p className="xs muted chart-note flex items-center gap-1"><Icon name="edit" size="sm" />{t('yield.correct.corrected')}</p>
          )}
          {byWeather && (
            <p className="xs muted chart-note">
              {sys.planes.length === 0 && <>{t('weather.notOriented')} · </>}
              <a href="https://open-meteo.com/" target="_blank" rel="noreferrer">{t('weather.attribution')}</a>
            </p>
          )}
        </div>
        <DataTable period={period} tz={tz} label={label} ts={ts} kwp={kwp} total={total} target={target} pr={pr} corrected={ov.corrected} summary={sm} inverters={invRows} invById={invById} />
      </section>

      {period === 'day' && <Readings ov={ov} ts={ts} tz={tz} labels={labels} invById={invById} />}
      {period === 'total' && <Heatmap sys={sys} first={first} onPick={(d) => navigate(hrefFor(d, 'day'))} />}

      <section className="section" aria-labelledby="inv-h">
        <SectionHead title={t('inverters.title')} id="inv-h">
          <span className="flex items-center gap-3">
            <span className="sm muted">{label}</span>
            {canEdit && <Button size="sm" icon="edit" onClick={() => setEditing(true)}>{t('yield.correct.button')}</Button>}
          </span>
        </SectionHead>
        <Card flush><InverterTable rows={invRows} invById={invById} summary={sm} label={label} /></Card>
      </section>
      {canEdit && <EditDayDialog sid={sid} date={ov.date} names={labels} open={editing} onOpenChange={setEditing} />}
      <p className="sm muted" style={{ marginTop: 16 }}>{t('yield.footer', { value: fmtKWh(sm.lifetime_energy_kwh), tz })}</p>
    </div>
  );
}

/** Temperature and DC voltage per inverter on the day axis, if measured. */
function Readings({ ov, ts, tz, labels, invById }: { ov: Overview; ts: string[]; tz: string; labels: Map<string, string>; invById: Map<string, Inverter> }) {
  const { t } = useTranslation();
  const [search, setSearch] = useSearchParams();
  const has = { temperature_c: ov.series.some((s) => s.temperature_c?.some((v) => v != null)), udc_v: ov.series.some((s) => s.udc_v?.some((v) => v != null)) };
  if (!has.temperature_c && !has.udc_v) return null;
  const kind: 'temperature_c' | 'udc_v' = search.get('reading') === 'udc' || !has.temperature_c ? 'udc_v' : 'temperature_c';
  const unit = kind === 'udc_v' ? 'V' : '°C';
  const series: ChartSeries[] = ov.series.filter((s) => s[kind]?.some((v) => v != null)).map((s) => ({
    key: s.inverter_id, name: labels.get(s.inverter_id) ?? t('yield.inverters.inverter'), color: invById.get(s.inverter_id)?.color || '#6b6b6b',
    values: alignTo(ts, ov.timestamps, s[kind]!),
  }));
  const fmt = (v: number | null | undefined) => (v == null ? '—' : `${num(v, 1)} ${unit}`);
  return (
    <section className="section card card-flush" aria-labelledby="readings-h">
      <div className="chart-card">
        <div className="chart-head">
          <h2 id="readings-h">{t('yield.readings.title')}</h2>
          {has.temperature_c && has.udc_v && (
            <Segmented label={t('yield.readings.label')} value={kind === 'udc_v' ? 'udc' : 'temp'}
              onChange={(v) => { const q = new URLSearchParams(search); if (v === 'udc') q.set('reading', v); else q.delete('reading'); setSearch(q, { replace: true, preventScrollReset: true }); }}
              options={[['temp', t('yield.readings.temperature'), 'thermometer'], ['udc', t('yield.readings.udc'), 'bolt']]} />
          )}
        </div>
        <LineChart series={series} labels={ts.map((x) => bucketLabel('day', x, tz))} tick={(i) => bucketTick('day', ts[i], tz, 0)}
          fmt={fmt} axis={(v, digits) => `${num(v, digits)} ${unit}`}
          ariaLabel={t(kind === 'udc_v' ? 'yield.readings.udcAria' : 'yield.readings.temperatureAria')} />
        <p className="xs muted chart-note">{t('yield.readings.note')}</p>
      </div>
    </section>
  );
}

/** Daily yield of every year as a calendar (total view). */
function Heatmap({ sys, first, onPick }: { sys: System; first: string; onPick: (date: string) => void }) {
  const { t } = useTranslation();
  const today = todayIn(sys.timezone);
  const q = useQuery({ ...getDailyEnergyOptions({
    path: { systemId: sys.id }, query: { from: first, to: addDays(today, 1) },
  }), staleTime: 10 * 60_000 });
  return (
    <section className="section card" aria-labelledby="heat-h">
      <div className="chart-head">
        <h2 id="heat-h">{t('yield.heatmap.title')}</h2>
        <span className="sm muted">{t('yield.heatmap.sub')}</span>
      </div>
      <QueryView queries={[q]} inline>
        {() => <CalendarHeatmap data={q.data!} kwp={sys.peak_power_kwp} first={first} last={today} onPick={onPick} />}
      </QueryView>
    </section>
  );
}
