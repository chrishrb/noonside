import { useState } from 'react';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { useParams, useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { getLiveOptions, listEventsInfiniteOptions } from '@/api/gen/@tanstack/react-query.gen';
import { useInverters, useSystem } from '@/api/queries';
import type { Event, Inverter, Live, System } from '@/api/types';
import { Content, PageHeader, QueryView, SectionHead } from '@/components/layout/Page';
import { KpiRow, KpiTile } from '@/components/data/KpiTile';
import { Button, ButtonLink } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import { Field, FieldError, Input, Select, Switch } from '@/components/ui/form';
import { Badge, Card, EmptyState, Spinner, StatusCell, Swatch } from '@/components/ui/primitives';
import { Table } from '@/components/ui/Table';
import { toast, toastError } from '@/components/ui/toast';
import { useTick } from '@/components/ui/misc';
import { AlertsSection } from '@/features/notifications/AlertsSection';
import { addDays, fmtDateTime, fmtDuration, fmtKWh, fmtPct, fmtRel, fmtTime, fmtW, num, todayIn, zonedMidnight } from '@/lib/format';
import { Freshness } from '@/components/data/Freshness';
import { AdviceSection } from '@/components/data/Advice';

const statusOpts = () => [
  { value: '', label: i18n.t('status.filters.allStatuses') }, { value: '8', label: '8 · MPP' }, { value: '0', label: `0 · ${i18n.t('status.filters.off')}` },
  { value: '9', label: `9 · ${i18n.t('common.error')}` }, { value: '12', label: `12 · ${i18n.t('status.filters.derating')}` },
];
const PAGE = 50;

export function StatusPage() {
  const { t } = useTranslation();
  const sid = useParams().sid!;
  const sys = useSystem(sid);
  const invs = useInverters(sid);
  const live = useQuery({ ...getLiveOptions({ path: { systemId: sid } }), refetchInterval: 60_000 });
  return (
    <QueryView queries={[sys, invs, live]}>
      {() => (
        <Content>
          <PageHeader eyebrow={sys.data!.name} title={t('nav.status')} sub={t('status.intro')} />
          <LiveSection sys={sys.data!} invs={invs.data!} live={live.data!} refetch={live.refetch} fetching={live.isFetching} />
          <AdviceSection sys={sys.data!} />
          <AlertsSection sys={sys.data!} />
          <EventLog sys={sys.data!} invs={invs.data!} />
        </Content>
      )}
    </QueryView>
  );
}

function LiveSection({ sys, invs, live, refetch, fetching }: { sys: System; invs: Inverter[]; live: Live; refetch: () => Promise<unknown>; fetching: boolean }) {
  const { t } = useTranslation();
  useTick();
  const tz = sys.timezone;
  const color = new Map(invs.map((i) => [i.id, i.color]));
  return (
    <section aria-labelledby="live-h">
      <SectionHead title={t('status.live')} id="live-h">
        <div className="flex items-center gap-2">
          <Freshness className="sm muted" time={live.time} stale={live.stale} tz={tz} format={(when) => t('status.updated', { when })} />
          <Button size="sm" icon="refresh" busy={fetching} onClick={() => refetch().then(() => toast(t('status.refreshed')), toastError)}>{t('status.refresh')}</Button>
        </div>
      </SectionHead>
      <KpiRow>
        <KpiTile label={t('status.acPower')} icon="bolt" value={fmtW(live.pac_w)} sub={t('kpi.pctPeak', { pct: num(sys.peak_power_kwp > 0 ? (live.pac_w / 1000 / sys.peak_power_kwp) * 100 : 0, 0) })} />
        <KpiTile label={t('status.dcPower')} icon="sun" value={fmtW(live.pdc_w)} />
        <KpiTile label={t('status.efficiency')} icon="activity" value={live.efficiency_pct != null ? fmtPct(live.efficiency_pct) : '—'} sub={live.efficiency_pct == null ? t('status.notProducing') : 'AC ÷ DC'} />
        <KpiTile label={t('kpi.energyToday')} icon="chart" value={fmtKWh(live.today_kwh)} />
      </KpiRow>
      <Card flush style={{ marginTop: 16 }}>
        <Table caption={t('status.liveCaption')}>
          <thead>
            <tr>
              <th scope="col">{t('yield.inverters.inverter')}</th><th scope="col">{t('status.connection')}</th><th scope="col">{t('common.status')}</th>
              <th scope="col" className="num">AC</th><th scope="col" className="num">DC</th><th scope="col" className="num">{t('status.efficiency')}</th><th scope="col">{t('status.lastReading')}</th>
            </tr>
          </thead>
          <tbody>
            {live.inverters.map((i) => (
              <tr key={i.inverter_id}>
                <td><span className="flex items-center gap-2"><Swatch color={color.get(i.inverter_id) ?? '#6b6b6b'} /><span className="font-medium">{i.name}</span></span></td>
                <td>{i.online ? <span className="status ok"><Icon name="dot" size="sm" />{t('status.online')}</span> : <span className="status off"><Icon name="circle" size="sm" />{t('status.offline')}</span>}</td>
                <td>
                  <StatusCell statusText={i.status_text} errorText={i.error_text} />
                  <LimitBadge pct={i.power_limit_pct} />
                  {i.error_text && <span className="xs block text-danger-text">{i.error_text}{i.error_code ? ` · ${t('status.code', { code: i.error_code })}` : ''}</span>}
                </td>
                <td className="num">{fmtW(i.pac_w)}</td>
                <td className="num">{fmtW(i.pdc_w)}</td>
                <td className="num">{i.efficiency_pct != null ? fmtPct(i.efficiency_pct) : '—'}</td>
                <td className="sm">{i.time ? <>{fmtTime(i.time, tz)} <span className="muted">· {fmtRel(i.time)}</span></> : '—'}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Card>
      <p className="xs muted" style={{ marginTop: 8 }}>{t('status.onlineHelp')}</p>
    </section>
  );
}

function EventLog({ sys, invs }: { sys: System; invs: Inverter[] }) {
  const { t } = useTranslation();
  const sid = sys.id, tz = sys.timezone;
  const [search, setSearch] = useSearchParams();
  const f = {
    inverter: search.get('inverter') ?? '', from: search.get('from') ?? '', to: search.get('to') ?? '',
    status: search.get('status') ?? '', errors: search.get('errors') === '1',
  };
  const filtered = !!(f.inverter || f.status || f.errors || f.from || f.to);
  const query = {
    inverter: f.inverter || undefined,
    status: f.status ? Number(f.status) : undefined,
    errors_only: f.errors || undefined,
    from: f.from ? zonedMidnight(f.from, tz) : undefined,
    to: f.to ? zonedMidnight(addDays(f.to, 1), tz) : undefined, // "to" is inclusive in the UI
    limit: PAGE,
  };
  const events = useInfiniteQuery({
    ...listEventsInfiniteOptions({ path: { systemId: sid }, query }),
    initialPageParam: { path: { systemId: sid } }, // the first page has no cursor
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
  const items = events.data?.pages.flatMap((p) => p.items) ?? [];
  const invById = new Map(invs.map((i) => [i.id, i]));

  return (
    <section className="section" aria-labelledby="ev-h">
      <SectionHead title={t('status.eventLog')} id="ev-h">
        {items.length > 0 && <span className="sm muted">{t(events.hasNextPage ? 'status.showingMore' : 'status.showing', { count: items.length })}</span>}
      </SectionHead>
      <EventFilters key={search.toString()} initial={f} invs={invs} today={todayIn(tz)} filtered={filtered} sid={sid}
        onApply={(v) => {
          const q = new URLSearchParams();
          if (v.inverter) q.set('inverter', v.inverter);
          if (v.from) q.set('from', v.from);
          if (v.to) q.set('to', v.to);
          if (v.status) q.set('status', v.status);
          if (v.errors) q.set('errors', '1');
          setSearch(q, { preventScrollReset: true });
        }} />
      <Card flush>
        {events.isPending ? (
          <div className="empty"><Spinner /><p>{t('status.loadingEvents')}</p></div>
        ) : events.isError ? (
          <EmptyState icon="alert" title={t('status.loadFailed')} text={events.error.message} action={<Button onClick={() => events.refetch()}>{t('common.tryAgain')}</Button>} />
        ) : items.length ? (
          <>
            <Table caption={t('status.eventsCaption')}>
              <thead><tr><th scope="col">{t('status.started')}</th><th scope="col">{t('status.duration')}</th><th scope="col">{t('yield.inverters.inverter')}</th><th scope="col">{t('common.status')}</th><th scope="col">{t('common.error')}</th></tr></thead>
              <tbody>{items.map((e) => <EventRow key={e.id} e={e} inv={invById.get(e.inverter_id)} tz={tz} />)}</tbody>
            </Table>
            {events.hasNextPage && (
              <div className="flex justify-center border-t border-border p-3">
                <Button busy={events.isFetchingNextPage} onClick={() => events.fetchNextPage().catch(toastError)}>{t('status.loadMore')}</Button>
              </div>
            )}
          </>
        ) : (
          <EmptyState icon="activity"
            title={filtered ? t('status.noMatch') : t('status.noEvents')}
            text={filtered ? t('status.noMatchText') : t('status.noEventsText')}
            action={filtered && <ButtonLink to={`/s/${sid}/status`}>{t('status.filters.reset')}</ButtonLink>} />
        )}
      </Card>
    </section>
  );
}

function EventRow({ e, inv, tz }: { e: Event; inv?: Inverter; tz: string }) {
  const { t } = useTranslation();
  const dur = e.ended_at ? fmtDuration(new Date(e.ended_at).getTime() - new Date(e.started_at).getTime()) : null;
  return (
    <tr>
      <td className="sm whitespace-nowrap">{fmtDateTime(e.started_at, tz)}</td>
      <td className="sm whitespace-nowrap">{dur ?? <Badge tone="warn" dot>{t('status.ongoing')}</Badge>}</td>
      <td><span className="flex items-center gap-2"><Swatch color={inv?.color ?? '#6b6b6b'} /><span className="sm">{inv?.name ?? t('status.deletedInverter')}</span></span></td>
      <td>
        <StatusCell statusText={e.status_text} errorText={e.error_text} />
        {e.status_code != null && <span className="xs muted block">{t('status.code', { code: e.status_code })}</span>}
        <LimitBadge pct={e.power_limit_pct} />
      </td>
      <td className="sm">
        {e.error_text ? <><span className="text-danger-text">{e.error_text}</span>{e.error_code != null && <span className="xs muted block">{t('status.code', { code: e.error_code })}</span>}</> : <span className="muted">—</span>}
      </td>
    </tr>
  );
}

/** Active power limit the device reported (curtailment); nothing at 100 % or without one. */
function LimitBadge({ pct }: { pct?: number }) {
  const { t } = useTranslation();
  if (pct == null || pct >= 100) return null;
  return <span className="block" style={{ marginTop: 4 }} title={t('status.limitHelp')}><Badge tone="warn">{t('status.limited', { pct: num(pct, 0) })}</Badge></span>;
}

interface Filters { inverter: string; from: string; to: string; status: string; errors: boolean }

function EventFilters({ initial, invs, today, filtered, sid, onApply }: {
  initial: Filters; invs: Inverter[]; today: string; filtered: boolean; sid: string; onApply: (f: Filters) => void;
}) {
  const { t } = useTranslation();
  const [v, setV] = useState(initial);
  const [toError, setToError] = useState<string>();
  const set = <K extends keyof Filters>(k: K, val: Filters[K]) => setV((o) => ({ ...o, [k]: val }));
  return (
    <form className="card" style={{ marginBottom: 16 }} noValidate onSubmit={(e) => {
      e.preventDefault();
      if (v.from && v.to && v.to < v.from) { setToError(t('status.filters.toBeforeFrom')); return; }
      onApply(v);
    }}>
      <div className="grid items-end gap-4" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <Field label={t('yield.inverters.inverter')}>
          <Select value={v.inverter} onChange={(e) => set('inverter', e.target.value)} options={[{ value: '', label: t('status.filters.allInverters') }, ...invs.map((i) => ({ value: i.id, label: i.name }))]} />
        </Field>
        <Field label={t('status.filters.from')}><Input type="date" max={today} value={v.from} onChange={(e) => set('from', e.target.value)} /></Field>
        <div className="field">
          <label htmlFor="ev-to">{t('status.filters.to')}</label>
          <Input id="ev-to" type="date" max={today} value={v.to} aria-invalid={toError ? true : undefined} onChange={(e) => { set('to', e.target.value); setToError(undefined); }} />
          {toError && <FieldError>{toError}</FieldError>}
        </div>
        <Field label={t('common.status')}><Select value={v.status} onChange={(e) => set('status', e.target.value)} options={statusOpts()} /></Field>
        <div className="field"><span className="label" aria-hidden="true">&nbsp;</span><Switch label={t('status.filters.errorsOnly')} checked={v.errors} onCheckedChange={(c) => set('errors', c)} /></div>
      </div>
      <div className="form-actions" style={{ marginTop: 16 }}>
        {filtered && <ButtonLink variant="ghost" to={`/s/${sid}/status`}>{t('status.filters.reset')}</ButtonLink>}
        <Button type="submit">{t('status.filters.apply')}</Button>
      </div>
    </form>
  );
}
