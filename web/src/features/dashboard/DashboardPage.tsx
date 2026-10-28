import { useState } from 'react';
import { useQueries, useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { getDashboardOptions, getLiveOptions, getOverviewOptions, getWeatherOptions, listAlertsOptions, listDataSourcesOptions } from '@/api/gen/@tanstack/react-query.gen';
import { useSystems } from '@/api/queries';
import type { Advice, Alert, DashboardSystem, DataSource, Live, Overview, System, Weather } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { can } from '@/auth/scopes';
import { useCreateSystem } from '@/components/layout/createSystem';
import { Content, PageHeader, QueryView, SectionHead } from '@/components/layout/Page';
import { KpiRow, KpiTile, ValueUnit } from '@/components/data/KpiTile';
import { Sparkline } from '@/components/data/Sparkline';
import { WeatherBadge } from '@/components/data/Weather';
import { Button } from '@/components/ui/Button';
import { Segmented } from '@/components/ui/form';
import { Icon } from '@/components/ui/Icon';
import { Card, EmptyState, ProgressBar, RoleBadge, Stat, SysTile } from '@/components/ui/primitives';
import { useTick } from '@/components/ui/misc';
import { fmtDay, fmtKg, fmtKWh, fmtKwp, fmtRel, fmtW, num } from '@/lib/format';
import { alignTo, dayAxis } from '@/lib/period';
import { SeverityIcon } from '@/features/notifications/NotificationList';
import { FLEET_VIEW, store } from '@/lib/storage';
import { FleetMapView } from './FleetMapView';
import { FleetTable } from './FleetTable';
import { StateBadge } from './state';
import { Freshness } from '@/components/data/Freshness';
import { ADVICE_STALE, AdviceHint, adviceEnabled, adviceQuery } from '@/components/data/Advice';

const LIVE = { refetchInterval: 60_000 };

const greeting = () => {
  const h = new Date().getHours();
  return h < 5 ? 'evening' : h < 12 ? 'morning' : h < 18 ? 'afternoon' : 'evening';
};

export function DashboardPage() {
  const { t } = useTranslation();
  const me = useMeData();
  const systems = useSystems();
  const dash = useQuery({ ...getDashboardOptions(), ...LIVE });
  const first = (me.name || me.email).split(/[\s@]/)[0];
  const title = t(`dashboard.greeting.${greeting()}`, { name: first });

  return (
    <QueryView queries={[systems, dash]}>
      {() => systems.data!.length === 0 ? <NoSystems title={title} /> : <Dashboard title={title} systems={systems.data!} rows={dash.data!.systems} />}
    </QueryView>
  );
}

function NoSystems({ title }: { title: string }) {
  const { t } = useTranslation();
  const me = useMeData();
  const create = useCreateSystem();
  return (
    <Content>
      <PageHeader title={title} sub={fmtDay(new Date())} />
      <Card>
        {me.is_superadmin ? (
          <EmptyState icon="home" title={t('dashboard.noSystems.title')} text={t('dashboard.noSystems.text')}
            action={<Button variant="primary" icon="plus" onClick={create}>{t('switcher.create')}</Button>} />
        ) : (
          <EmptyState icon="users" title={t('dashboard.noMember.title')} text={t('dashboard.noMember.text')} />
        )}
      </Card>
    </Content>
  );
}

function Dashboard({ title, systems, rows }: { title: string; systems: System[]; rows: DashboardSystem[] }) {
  const { t } = useTranslation();
  const me = useMeData();
  const create = useCreateSystem();
  useTick();
  const byId = new Map(systems.map((s) => [s.id, s]));
  const ids = rows.map((r) => r.system_id);
  const lives = useQueries({ queries: ids.map((id) => ({ ...getLiveOptions({ path: { systemId: id } }), ...LIVE })) });
  const days = useQueries({
    queries: ids.map((id) => ({ ...getOverviewOptions({ path: { systemId: id }, query: { period: 'day' } }), ...LIVE })),
  });
  const weather = useQueries({
    queries: ids.map((id) => {
      const sys = byId.get(id);
      return { ...getWeatherOptions({ path: { systemId: id } }), staleTime: 10 * 60_000, refetchInterval: 15 * 60_000, enabled: sys?.latitude != null && sys?.longitude != null };
    }),
  });
  const advice = useQueries({
    queries: ids.map((id) => ({ ...adviceQuery(id), staleTime: ADVICE_STALE, refetchInterval: ADVICE_STALE, enabled: adviceEnabled(byId.get(id)) })),
  });
  const alerts = useQueries({ queries: ids.map((id) => ({ ...listAlertsOptions({ path: { systemId: id } }), ...LIVE })) });
  const sources = useQueries({
    queries: ids.map((id) => ({ ...listDataSourcesOptions({ path: { systemId: id } }), enabled: can(me, id, 'datasource:manage') })),
  });
  const [view, setView] = useState<FleetView>(() => fleetView(store.get(FLEET_VIEW)));
  const shown = rows.length > 1 ? view : 'cards';

  return (
    <Content>
      <PageHeader
        title={title}
        sub={`${fmtDay(new Date())} · ${t('counts.system', { count: rows.length })}`}
        actions={me.is_superadmin && <Button icon="plus" onClick={create}>{t('dashboard.newSystem')}</Button>}
      />
      {rows.length > 1 && <AllSystems rows={rows} byId={byId} />}
      <section className="section" aria-labelledby="fleet-h">
        {rows.length > 1 && (
          <SectionHead title={t('switcher.systems')} id="fleet-h">
            <Segmented label={t('dashboard.fleet.view')} value={view} onChange={(v) => { setView(v); store.set(FLEET_VIEW, v); }}
              options={[['cards', t('dashboard.fleet.cards'), 'grid'], ['table', t('dashboard.fleet.table'), 'list'], ['map', t('dashboard.fleet.map'), 'pin']]} />
          </SectionHead>
        )}
        {shown === 'table' ? <div className="card card-flush"><FleetTable rows={rows} /></div>
          : shown === 'map' ? <FleetMapView rows={rows} byId={byId} /> : (
          <div className="grid-2">
            {rows.map((s, k) => <SystemCard key={s.system_id} s={s} sys={byId.get(s.system_id)} day={days[k].data} wx={weather[k].data} advice={advice[k].data} />)}
          </div>
        )}
      </section>
      <NeedsAttention rows={rows} alerts={alerts.map((q) => q.data?.items)} lives={lives.map((q) => q.data)} sources={sources.map((q) => q.data)} />
    </Content>
  );
}

type FleetView = 'cards' | 'table' | 'map';
const fleetView = (v: string | null): FleetView => (v === 'table' || v === 'map' ? v : 'cards');

function AllSystems({ rows, byId }: { rows: DashboardSystem[]; byId: Map<string, System> }) {
  const { t } = useTranslation();
  const sum = (k: keyof DashboardSystem) => rows.reduce((a, s) => a + (Number(s[k]) || 0), 0);
  const kwp = sum('peak_power_kwp');
  const pac = sum('pac_w');
  const mTarget = sum('month_target_to_date_kwh');
  const co2 = rows.reduce((a, s) => a + s.lifetime_kwh * (byId.get(s.system_id)?.co2_kg_per_kwh ?? 0), 0);
  const errs = sum('inverters_with_error');
  return (
    <section className="section" aria-labelledby="agg-h" style={{ marginTop: 0 }}>
      <SectionHead title={t('nav.allSystems')} id="agg-h"><span className="sm muted">{t('counts.system', { count: rows.length })} · {fmtKwp(kwp)}</span></SectionHead>
      <KpiRow>
        <KpiTile label={t('kpi.currentPower')} icon="bolt" value={fmtW(pac)} sub={t('kpi.pctInstalled', { pct: num(kwp > 0 ? (pac / 1000 / kwp) * 100 : 0, 0) })} />
        <KpiTile label={t('period.current.day')} icon="sun" value={fmtKWh(sum('today_kwh'))} />
        <KpiTile label={t('period.current.month')} icon="target" value={fmtKWh(sum('month_kwh'))} sub={mTarget > 0 ? t('kpi.pctTargetToDate', { pct: num((sum('month_kwh') / mTarget) * 100, 0) }) : undefined} />
        <KpiTile label={t('period.current.year')} icon="chart" value={fmtKWh(sum('year_kwh'))} />
        <KpiTile label={t('kpi.lifetime')} icon="leaf" value={fmtKWh(sum('lifetime_kwh'))} sub={t('kpi.co2Avoided', { kg: fmtKg(co2) })} />
        <KpiTile label={t('kpi.invertersOnline')} icon="activity"
          value={<>{sum('inverters_online')}<span className="unit">/ {sum('inverters_total')}</span></>}
          sub={errs ? <span className="status err"><Icon name="alert" size="sm" />{t('kpi.withError', { count: errs })}</span> : t('kpi.noErrors')} />
      </KpiRow>
    </section>
  );
}

function SystemCard({ s, sys, day, wx, advice }: { s: DashboardSystem; sys?: System; day?: Overview; wx?: Weather; advice?: Advice }) {
  const { t } = useTranslation();
  const pct = s.peak_power_kwp > 0 ? (s.pac_w / 1000 / s.peak_power_kwp) * 100 : 0;
  const mPct = s.month_target_to_date_kwh ? (s.month_kwh / s.month_target_to_date_kwh) * 100 : null;
  const tz = sys?.timezone ?? 'UTC';
  const curve = day ? alignTo(dayAxis(day.range_start, tz), day.timestamps, day.total) : [];
  return (
    <article className="sys-card" aria-labelledby={`sys-${s.system_id}`}>
      <div className="sys-card-head">
        <div className="flex min-w-0 items-center gap-3">
          {s.image_url && <SysTile name={s.name} src={s.image_url} className="sys-card-tile" />}
          <div className="grid min-w-0 gap-0.5">
            {/* The name link covers the whole card (.sys-card-link), which opens the yield page. */}
            <h2 id={`sys-${s.system_id}`}><Link className="sys-card-link" to={`/s/${s.system_id}/overview`}>{s.name}</Link></h2>
            <span className="sm muted truncate">{sys?.location || t('common.noLocation')} · {fmtKwp(s.peak_power_kwp)}</span>
          </div>
        </div>
        <div className="flex flex-none flex-wrap justify-end gap-2">{wx?.available && <WeatherBadge w={wx} />}<StateBadge s={s} /><RoleBadge role={s.role} /></div>
      </div>
      <div className="sys-power">
        <div className="stat">
          <span className="sm muted">{t('kpi.currentPower')}</span>
          <span className="big"><ValueUnit value={fmtW(s.pac_w)} /></span>
          <span className="sm muted">{t('kpi.pctPeak', { pct: num(pct, 0) })}</span>
          <ProgressBar value={pct} />
        </div>
        <div>
          <Sparkline values={curve} label={t('dashboard.curveLabel', { name: s.name })} />
          <span className="xs muted">{t('dashboard.curveRange')}</span>
        </div>
      </div>
      <div className="sys-metrics">
        <Stat label={t('period.current.day')}>{fmtKWh(s.today_kwh)}</Stat>
        <div className="stat">
          <span className="l">{t('period.current.month')}</span><span className="v">{fmtKWh(s.month_kwh)}</span>
          {mPct != null && <span className="xs muted">{t('kpi.pctTarget', { pct: num(mPct, 0) })}</span>}
        </div>
        <Stat label={t('period.current.year')}>{fmtKWh(s.year_kwh)}</Stat>
        <Stat label={t('kpi.lifetime')}>{fmtKWh(s.lifetime_kwh)}</Stat>
      </div>
      <AdviceHint a={advice} tz={tz} />
      <div className="sys-foot">
        <span className="muted">{t('dashboard.online', { online: s.inverters_online, total: s.inverters_total })} · <Freshness time={s.last_update} stale={s.stale} tz={tz} format={(when) => t('dashboard.updated', { when })} /></span>
        <Icon name="right" size="sm" className="sys-card-go" />
      </div>
    </article>
  );
}

// Open alerts first, then live problems the alert engine hasn't reported yet
// (it waits a while before alerting).
function NeedsAttention({ rows, alerts, lives, sources }: { rows: DashboardSystem[]; alerts: (Alert[] | undefined)[]; lives: (Live | undefined)[]; sources: (DataSource[] | undefined)[] }) {
  const { t } = useTranslation();
  const items: React.ReactNode[] = [];
  rows.forEach((s, k) => {
    const open = alerts[k] ?? [];
    const covered = new Set(open.flatMap((a) => [a.inverter_id, a.data_source_id]).filter(Boolean));
    open.forEach((a) => items.push(
      <div className="attention-item" key={`a-${a.id}`}>
        <SeverityIcon severity={a.severity} />
        <div className="body">
          <strong>{a.title}{a.title.includes(s.name) ? '' : ` · ${s.name}`}</strong>
          <span className="muted line-clamp-2">{a.body} · {t('alerts.since', { when: fmtRel(a.opened_at) })}</span>
        </div>
        {a.kind === 'data_source_failing'
          ? <Link className="btn btn-secondary btn-sm flex-none" to={`/s/${s.system_id}/settings/sources`}>{t('settings.tabs.sources')}</Link>
          : <Link className="btn btn-secondary btn-sm flex-none" to={`/s/${s.system_id}/status`}>{t('common.status')}</Link>}
      </div>,
    ));
    lives[k]?.inverters.filter((i) => i.error_text && !covered.has(i.inverter_id)).forEach((i) => items.push(
      <div className="attention-item" key={`i-${i.inverter_id}`}>
        <span className="status err"><Icon name="alert" /></span>
        <div className="body">
          <strong>{i.name} · {s.name}</strong>
          <span className="muted">{i.error_text}{i.error_code ? ` ${t('dashboard.code', { code: i.error_code })}` : ''} · {t('dashboard.reported', { when: fmtRel(i.time) })}</span>
        </div>
        <Link className="btn btn-secondary btn-sm flex-none" to={`/s/${s.system_id}/status`}>{t('common.status')}</Link>
      </div>,
    ));
    sources[k]?.filter((d) => d.last_error && d.enabled && !covered.has(d.id)).forEach((d) => items.push(
      <div className="attention-item" key={`d-${d.id}`}>
        <span className="status warn"><Icon name="database" /></span>
        <div className="body">
          <strong>{d.name} · {s.name}</strong>
          <span className="muted line-clamp-2">{d.last_error}</span>
        </div>
        <Link className="btn btn-secondary btn-sm flex-none" to={`/s/${s.system_id}/settings/sources`}>{t('settings.tabs.sources')}</Link>
      </div>,
    ));
  });
  const anySources = sources.some((x) => x?.length);
  return (
    <section className="section" aria-labelledby="att-h">
      <SectionHead title={t('dashboard.attention')} id="att-h" />
      <Card flush>
        {items.length ? items : (
          <div className="attention-item">
            <span className="status ok"><Icon name="check" /></span>
            <div className="body"><span>{anySources ? t('dashboard.allNormalSources') : t('dashboard.allNormal')}</span></div>
          </div>
        )}
      </Card>
    </section>
  );
}
