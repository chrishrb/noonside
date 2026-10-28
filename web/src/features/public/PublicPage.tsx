import { useEffect, useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useParams, useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import { getPublicLiveOptions, getPublicOverviewOptions, getPublicSystemOptions } from '@/api/gen/@tanstack/react-query.gen';
import type { Live, Overview, Period, PublicSystem } from '@/api/types';
import { FullScreenLoading } from '@/auth/RequireAuth';
import { ChartLegend, EnergyChart, type ChartSeries } from '@/components/data/EnergyChart';
import { KpiRow, KpiTile, ValueUnit } from '@/components/data/KpiTile';
import { usePageTitle } from '@/components/layout/Page';
import { Icon } from '@/components/ui/Icon';
import { LogoMark } from '@/components/ui/Logo';
import { SysTile } from '@/components/ui/primitives';
import { Segmented } from '@/components/ui/form';
import { fmtDate, fmtKg, fmtKWh, fmtKwp, fmtMoney, fmtTime, fmtW, num } from '@/lib/format';
import { alignTo, bucketLabel, bucketTick, chartTitle, dayAxis, drillDown, periodLabel, periodName, periodStart } from '@/lib/period';
import { expectedAt } from '@/lib/weather';
import { Freshness } from '@/components/data/Freshness';

const PERIODS: Period[] = ['day', 'month', 'year', 'total'];
// Kiosk: the views it cycles through and how long each stays.
const KIOSK_VIEWS: Period[] = ['day', 'month', 'year'];
const KIOSK_STEP = 20_000;
// Few requests per screen: the public endpoints are rate-limited per IP.
const LIVE_REFRESH = 60_000;
const refreshOf = (p: Period) => (p === 'day' ? 60_000 : 5 * 60_000);

/** Read-only page of a share link (`/p/:token`); `?kiosk=1` is the large-screen mode. */
export function PublicPage() {
  const { t } = useTranslation();
  const token = useParams().token!;
  const [search] = useSearchParams();
  const kiosk = search.get('kiosk') === '1';
  const path = { path: { token } };
  const sys = useQuery({ ...getPublicSystemOptions(path), retry: false, refetchInterval: 30 * 60_000 });
  const live = useQuery({ ...getPublicLiveOptions(path), enabled: sys.isSuccess, refetchInterval: LIVE_REFRESH });

  // Share links are not for search engines.
  useEffect(() => {
    const m = document.createElement('meta');
    m.name = 'robots';
    m.content = 'noindex';
    document.head.appendChild(m);
    return () => m.remove();
  }, []);

  if (sys.isPending) return <FullScreenLoading text={t('common.loading')} />;
  if (sys.isError) {
    return (
      <div className="public-page">
        <div className="public-gone">
          <Icon name="link" />
          <h1>{t('public.gone.title')}</h1>
          <p className="muted">{t('public.gone.text')}</p>
        </div>
      </div>
    );
  }
  return kiosk
    ? <Kiosk token={token} sys={sys.data} live={live.data} />
    : <Public token={token} sys={sys.data} live={live.data} />;
}

function Brand({ sys }: { sys: PublicSystem }) {
  const { t } = useTranslation();
  return (
    <header className="public-head">
      <span className="brand"><LogoMark />noonside</span>
      <div className="flex min-w-0 items-center gap-3">
        {sys.image_url && <SysTile name={sys.name} src={sys.image_url} className="sys-card-tile" />}
        <div className="grid min-w-0 gap-0.5">
          <h1 className="truncate">{sys.name}</h1>
          <span className="sm muted truncate">{fmtKwp(sys.peak_power_kwp)}{sys.commissioned_at ? ` · ${t('yield.commissioned', { date: fmtDate(sys.commissioned_at) })}` : ''}</span>
        </div>
      </div>
    </header>
  );
}

function useOverview(token: string, period: Period, date?: string) {
  return useQuery({ ...getPublicOverviewOptions({ path: { token }, query: { period, date } }), placeholderData: keepPreviousData, refetchInterval: date ? false : refreshOf(period) });
}

function Public({ token, sys, live }: { token: string; sys: PublicSystem; live?: Live }) {
  const { t } = useTranslation();
  usePageTitle(sys.name);
  const [search, setSearch] = useSearchParams();
  const period = PERIODS.find((p) => p === search.get('period')) ?? 'day';
  const date = search.get('date') ?? undefined;
  const ov = useOverview(token, period, date);
  const go = (p: Period, d?: string) => {
    const q = new URLSearchParams(search);
    q.set('period', p);
    if (d) q.set('date', d); else q.delete('date');
    setSearch(q);
  };
  const o = ov.data;
  return (
    <div className="public-page">
      <Brand sys={sys} />
      <main className="content public-content">
        <div className="toolbar">
          <Segmented label={t('yield.period')} value={period} onChange={(p) => go(p)} options={PERIODS.map((p) => [p, periodName(p)])} />
          {o && period !== 'total' && (
            <div className="flex items-center gap-2">
              <button type="button" className="btn btn-ghost btn-sm" aria-label={t('dateNav.previous')} disabled={o.prev_date < periodStart(period, o.first_date)} onClick={() => go(period, o.prev_date)}><Icon name="left" size="sm" /></button>
              <span className="font-medium">{periodLabel(period, o.date, o.first_date)}</span>
              <button type="button" className="btn btn-ghost btn-sm" aria-label={t('dateNav.next')} disabled={!o.next_date} onClick={() => o.next_date && go(period, o.next_date)}><Icon name="right" size="sm" /></button>
            </div>
          )}
        </div>
        {o && <Summary sys={sys} ov={o} live={live} current={!o.next_date} />}
        {o && <PublicChart sys={sys} ov={o} onDrill={period === 'day' ? undefined : go} />}
        <p className="xs muted" style={{ marginTop: 16 }}>{t('public.footer', { tz: sys.timezone })}</p>
      </main>
    </div>
  );
}

function Summary({ sys, ov, live, current }: { sys: PublicSystem; ov: Overview; live?: Live; current: boolean }) {
  const { t } = useTranslation();
  const sm = ov.summary;
  return (
    <KpiRow>
      {ov.period === 'day' && current && live && (
        <KpiTile label={t('kpi.currentPower')} icon="bolt" value={fmtW(live.pac_w)} sub={live.time ? <Freshness time={live.time} stale={live.stale} tz={sys.timezone} format={(when) => `${t('kpi.reading', { time: fmtTime(live.time, sys.timezone) })} · ${when}`} /> : t('kpi.noReading')} />
      )}
      <KpiTile label={ov.period === 'day' && current ? t('kpi.energyToday') : t('yield.inverters.energy')} icon="sun" value={fmtKWh(sm.energy_kwh)}
        sub={sm.specific_yield_kwh_kwp != null ? `${num(sm.specific_yield_kwh_kwp, 2)} kWh/kWp` : undefined} />
      {sys.show_financials && <KpiTile label={t('kpi.revenue')} icon="coins" value={fmtMoney(sm.revenue, sm.currency)} />}
      <KpiTile label={t('kpi.co2')} icon="leaf" value={fmtKg(sm.co2_avoided_kg)} sub={t('kpi.lifetimeValue', { value: fmtKg(sm.lifetime_co2_avoided_kg) })} />
      <KpiTile label={t('kpi.lifetime')} icon="trophy" value={fmtKWh(sm.lifetime_energy_kwh)} />
    </KpiRow>
  );
}

function PublicChart({ sys, ov, onDrill, big }: { sys: PublicSystem; ov: Overview; onDrill?: (p: Period, d: string) => void; big?: boolean }) {
  const { t } = useTranslation();
  const tz = sys.timezone, period = ov.period;
  const ts = period === 'day' ? dayAxis(ov.range_start, tz) : ov.timestamps;
  const total = period === 'day' ? alignTo(ts, ov.timestamps, ov.total) : ov.total.map((v) => v ?? null);
  const series: ChartSeries[] = [{ key: 'total', name: t('common.total'), color: 'var(--accent)', values: total }];
  const expected = period === 'day' && ov.expected?.length ? expectedAt(ov.expected, ts) : undefined;
  const byWeather = ov.summary.target_source === 'weather';
  const target = period !== 'day' && ov.target ? ov.target : undefined;
  return (
    <section className={big ? 'kiosk-chart' : 'section card card-flush'} aria-labelledby="pchart-h">
      <div className="chart-card">
        <div className="chart-head">
          <h2 id="pchart-h">{big ? `${chartTitle(period)} · ${periodLabel(period, ov.date, ov.first_date)}` : chartTitle(period)}</h2>
          <ChartLegend series={series} target={!!target} targetLabel={byWeather ? t('common.expected') : undefined} expected={!!expected} />
        </div>
        <EnergyChart kind={period === 'day' ? 'area' : 'bar'} unit={ov.unit} series={series} total={total} target={target}
          targetLabel={byWeather ? t('common.expected') : undefined} expected={expected}
          labels={ts.map((x) => bucketLabel(period, x, tz))} fmt={period === 'day' ? fmtW : fmtKWh}
          tick={(i, band) => bucketTick(period, ts[i], tz, band)}
          ariaLabel={t('public.chartAria', { title: chartTitle(period), total: fmtKWh(ov.summary.energy_kwh) })}
          emptyText={period === 'day' ? t('yield.emptyDay') : t('yield.emptyPeriod')}
          onSelect={onDrill && period !== 'day' ? (i) => {
            const to = drillDown(period, ts[i], tz);
            if (to) onDrill(to.period, to.date);
          } : undefined} />
        {byWeather && <p className="xs muted chart-note"><a href="https://open-meteo.com/" target="_blank" rel="noreferrer">{t('weather.attribution')}</a></p>}
      </div>
    </section>
  );
}

/** Large-screen mode: live power and today's yield, charts cycling day → month → year. */
function Kiosk({ token, sys, live }: { token: string; sys: PublicSystem; live?: Live }) {
  const { t } = useTranslation();
  usePageTitle(sys.name);
  const [step, setStep] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setStep((s) => (s + 1) % KIOSK_VIEWS.length), KIOSK_STEP);
    return () => clearInterval(id);
  }, []);
  // All three views stay loaded, so switching needs no request.
  const day = useOverview(token, 'day');
  const month = useOverview(token, 'month');
  const year = useOverview(token, 'year');
  const views = [day.data, month.data, year.data];
  const shown = views[step] ?? day.data;
  const d = day.data?.summary;
  return (
    <div className="public-page kiosk">
      <Brand sys={sys} />
      <main className="kiosk-main">
        <div className="kiosk-kpis">
          <div className="kiosk-kpi">
            <span className="label"><Icon name="bolt" />{t('kpi.currentPower')}</span>
            <span className="value"><ValueUnit value={fmtW(live?.pac_w ?? 0)} /></span>
            <span className="sub">{live?.time ? <Freshness time={live.time} stale={live.stale} tz={sys.timezone} format={(when) => `${t('kpi.reading', { time: fmtTime(live.time, sys.timezone) })} · ${when}`} /> : t('kpi.noReading')}</span>
          </div>
          <div className="kiosk-kpi">
            <span className="label"><Icon name="sun" />{t('kpi.energyToday')}</span>
            <span className="value"><ValueUnit value={fmtKWh(d?.energy_kwh)} /></span>
            <span className="sub">{d?.specific_yield_kwh_kwp != null ? `${num(d.specific_yield_kwh_kwp, 2)} kWh/kWp` : ' '}</span>
          </div>
          <div className="kiosk-kpi">
            <span className="label"><Icon name="leaf" />{t('kpi.co2')}</span>
            <span className="value"><ValueUnit value={fmtKg(d?.lifetime_co2_avoided_kg)} /></span>
            <span className="sub">{t('kpi.lifetimeValue', { value: fmtKWh(d?.lifetime_energy_kwh) })}</span>
          </div>
        </div>
        {shown && <PublicChart sys={sys} ov={shown} big />}
        <div className="kiosk-steps" aria-hidden>{KIOSK_VIEWS.map((p, i) => <span key={p} className={i === step ? 'on' : undefined}>{periodName(p)}</span>)}</div>
      </main>
    </div>
  );
}
