import { getNotificationTargetsOptions, sendTestPushMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useMutation, useQuery } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';
import { Link, Navigate, useParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { Button } from '@/components/ui/Button';
import { BackLink, NavList, NavRow, RadioRow, ToggleRow } from '@/components/ui/NavList';
import { Banner, Card, Skeleton } from '@/components/ui/primitives';
import { toast, toastError } from '@/components/ui/toast';
import { subscribePush, unsubscribePush, usePushState, type PushState } from '@/lib/push';
import { TargetCard } from './NotificationTargets';
import { BASE, CHANNELS, TYPES, typeSummary, useUnavailable, usePrefs, type Channel, type Pref, type Scope, type TypeKey } from './prefs';

const SEVERITIES = ['info', 'warning', 'error'] as const;
const DIGESTS = ['daily', 'weekly', 'monthly'] as const;

/**
 * Notification settings, LinkedIn style: lists of groups that open a page
 * each. The part of the URL after /profile/notifications picks the page.
 */
export function NotificationSettings() {
  const { t } = useTranslation();
  const { '*': rest = '' } = useParams();
  const prefs = usePrefs();
  if (prefs.q.isError) return <Banner tone="danger" title={t('notifications.prefs.loadFailed')}>{prefs.q.error.message}</Banner>;
  if (!prefs.data) return <Skeleton style={{ height: 280 }} />;

  const [a, b, c] = rest.split('/').filter(Boolean);
  if (!a) return <Overview prefs={prefs} />;
  if (TYPES.includes(a as TypeKey) && !b) return <TypeSettings prefs={prefs} scope="default" type={a as TypeKey} back={BASE} />;
  if (a === 'push' && !b) return <PushSettings available={prefs.data.push_available} />;
  if ((a === 'ntfy' || a === 'webhook') && !b) return <TargetCard channel={a} back={BASE} />;
  if (a === 'systems' && b && prefs.data.member_systems.some((m) => m.system_id === b)) {
    if (!c) return <SystemSettings prefs={prefs} id={b} />;
    if (TYPES.includes(c as TypeKey)) return <TypeSettings prefs={prefs} scope={b} type={c as TypeKey} back={`${BASE}/systems/${b}`} />;
  }
  return <Navigate to={BASE} replace />;
}

type Prefs = ReturnType<typeof usePrefs>;

function Overview({ prefs }: { prefs: Prefs }) {
  const { t } = useTranslation();
  const data = prefs.data!;
  const { state } = usePushState();
  const targets = useQuery(getNotificationTargetsOptions());
  const setUp = (on: boolean | undefined) => (on === undefined ? undefined : on ? t('notifications.prefs.setUp') : t('notifications.prefs.notSetUp'));
  return (
    <>
      <TypeRows prefs={prefs} scope="default" base={BASE} title={t('notifications.prefs.receive')} intro={t('notifications.prefs.intro')} />
      <NavList title={t('notifications.prefs.delivery')} intro={t('notifications.prefs.deliveryIntro')}>
        <NavRow to={`${BASE}/push`} title={t('notifications.push.title')} sub={pushSummary(t, data.push_available, state)} />
        <NavRow to={`${BASE}/ntfy`} title="ntfy" sub={setUp(targets.data && !!targets.data.ntfy_url)} />
        <NavRow to={`${BASE}/webhook`} title="Webhook" sub={setUp(targets.data && !!targets.data.webhook_url)} />
      </NavList>
      {data.member_systems.length > 0 && (
        <NavList title={t('notifications.prefs.perSystem')} intro={t('notifications.prefs.perSystemHelp')}>
          {data.member_systems.map((m) => (
            <NavRow key={m.system_id} to={`${BASE}/systems/${m.system_id}`} title={m.name} sub={t(`notifications.prefs.modes.${prefs.modeOf(m.system_id)}`)} />
          ))}
        </NavList>
      )}
    </>
  );
}

/** The three notification types as rows, for the default or one system. */
function TypeRows({ prefs, scope, base, title, intro }: { prefs: Prefs; scope: Scope; base: string; title: string; intro?: string }) {
  const { t } = useTranslation();
  const p = prefs.prefOf(scope);
  const unavailable = useUnavailable();
  return (
    <NavList title={title} intro={intro}>
      {TYPES.map((type) => (
        <NavRow key={type} to={`${base}/${type}`} title={t(`notifications.prefs.types.${type}.title`)} sub={typeSummary(t, type, p, unavailable)} />
      ))}
    </NavList>
  );
}

function pushSummary(t: TFunction, available: boolean, state: PushState): string {
  if (!available) return t('notifications.push.unavailableShort');
  return t(`notifications.push.states.${state}`);
}

function TypeSettings({ prefs, scope, type, back }: { prefs: Prefs; scope: Scope; type: TypeKey; back: string }) {
  const { t } = useTranslation();
  const data = prefs.data!;
  const p = prefs.prefOf(scope);
  const v = p[type];
  const set = <K extends TypeKey>(k: K, patch: Partial<Pref[K]>) => prefs.setPref(scope, { ...p, [k]: { ...p[k], ...patch } });
  const on = type === 'alerts' || (type === 'reports' ? p.reports.digest !== 'none' : p.forecast.enabled);
  const system = scope === 'default' ? undefined : data.member_systems.find((m) => m.system_id === scope)?.name;
  const toggle = (c: Channel, checked: boolean) => set(type, { channels: checked ? [...v.channels, c] : v.channels.filter((x) => x !== c) });
  const title = t(`notifications.prefs.types.${type}.title`);
  const { state: pushState } = usePushState();
  const targets = useQuery(getNotificationTargetsOptions());
  const unavailable = useUnavailable();
  const helpOf = (c: Channel) => channelHelp(t, c, type, { email: data.email_available, push: data.push_available, pushState, targets: targets.data });

  return (
    <>
      <NavList head={<BackLink to={back}>{system ?? t('notifications.prefs.title')}</BackLink>} title={title} intro={t(`notifications.prefs.types.${type}.intro`)}>
        {type === 'reports' && (
          <ToggleRow strong title={t('notifications.prefs.types.reports.allow')} checked={on}
            onCheckedChange={(x) => set('reports', { digest: x ? 'weekly' : 'none' })} />
        )}
        {type === 'forecast' && (
          <ToggleRow strong title={t('notifications.prefs.types.forecast.allow')} checked={on}
            onCheckedChange={(x) => set('forecast', { enabled: x })} />
        )}
      </NavList>
      {on && type === 'reports' && (
        <NavList section title={t('notifications.prefs.frequency')}>
          {DIGESTS.map((d) => (
            <RadioRow key={d} name="digest" title={t(`notifications.prefs.digests.${d}`)} checked={p.reports.digest === d} onChange={() => set('reports', { digest: d })} />
          ))}
        </NavList>
      )}
      {on && (
        <NavList section title={t('notifications.prefs.channelsTitle')} intro={t('notifications.prefs.channelsIntro')}>
          {CHANNELS.map((c) => (
            <ToggleRow key={c} title={t(`notifications.prefs.channels.${c}.title`)} help={helpOf(c)}
              checked={v.channels.includes(c) && !unavailable.includes(c)} disabled={unavailable.includes(c)} onCheckedChange={(x) => toggle(c, x)} />
          ))}
        </NavList>
      )}
      {type === 'alerts' && (
        <NavList section title={t('notifications.prefs.minSeverity')}>
          {SEVERITIES.map((s) => (
            <RadioRow key={s} name="severity" title={t(`notifications.prefs.severities.${s}`)} checked={p.alerts.min_severity === s} onChange={() => set('alerts', { min_severity: s })} />
          ))}
          <ToggleRow title={t('notifications.prefs.notifyResolved')} checked={p.alerts.notify_resolved} onCheckedChange={(x) => set('alerts', { notify_resolved: x })} />
        </NavList>
      )}
    </>
  );
}

/** What a channel does, or what is missing before it works. */
function channelHelp(t: TFunction, c: Channel, type: TypeKey, x: { email: boolean; push: boolean; pushState: PushState; targets?: { ntfy_url: string; webhook_url: string } }): ReactNode {
  const k = 'notifications.prefs.channels';
  switch (c) {
    case 'in_app': return t(`${k}.in_app.help`);
    case 'email':
      if (!x.email) return t(`${k}.email.unavailable`);
      return type === 'reports' ? t(`${k}.email.report`) : null;
    case 'push':
      if (!x.push) return t('notifications.push.unavailable');
      if (x.pushState !== 'on') return <Link to={`${BASE}/push`}>{t(`${k}.push.setUp`)}</Link>;
      return t(`${k}.push.help`);
    case 'ntfy':
    case 'webhook':
      if (x.targets && !(c === 'ntfy' ? x.targets.ntfy_url : x.targets.webhook_url)) return <Link to={`${BASE}/${c}`}>{t(`${k}.${c}.setUp`)}</Link>;
      return null;
  }
}

function SystemSettings({ prefs, id }: { prefs: Prefs; id: string }) {
  const { t } = useTranslation();
  const m = prefs.data!.member_systems.find((x) => x.system_id === id)!;
  const mode = prefs.modeOf(id);
  const base = `${BASE}/systems/${id}`;
  return (
    <>
      <NavList head={<BackLink to={BASE}>{t('notifications.prefs.title')}</BackLink>} title={m.name} intro={t('notifications.prefs.perSystemHelp')}>
        <ToggleRow strong title={t('notifications.prefs.systemOn')} help={mode === 'muted' ? t('notifications.prefs.mutedHelp') : undefined}
          checked={mode !== 'muted'} onCheckedChange={(x) => prefs.setMode(id, x ? 'on' : 'muted')} />
        {mode !== 'muted' && (
          <ToggleRow title={t('notifications.prefs.useDefault')} help={t('notifications.prefs.useDefaultHelp')}
            checked={mode === 'default'} onCheckedChange={(x) => prefs.setMode(id, x ? 'default' : 'custom')} />
        )}
      </NavList>
      {mode === 'custom' && <TypeRows prefs={prefs} scope={id} base={base} title={t('notifications.prefs.receiveSystem')} />}
    </>
  );
}

function PushSettings({ available }: { available: boolean }) {
  const { t } = useTranslation();
  const { state, refresh } = usePushState();
  const [busy, setBusy] = useState(false);
  const test = useMutation(sendTestPushMutation());
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try { await fn(); } catch (e) { toastError(e); } finally { setBusy(false); await refresh(); }
  };
  const note = !available ? t('notifications.push.unavailable')
    : state === 'unsupported' ? t('notifications.push.unsupported')
    : state === 'install' ? t('notifications.push.iosInstall')
    : state === 'blocked' ? t('notifications.push.blocked')
    : state === 'on' ? t('notifications.push.on') : null;
  return (
    <Card as="section" aria-labelledby="push-h">
      <div className="grid gap-4">
        <BackLink to={BASE}>{t('notifications.prefs.title')}</BackLink>
        <div className="grid max-w-[60ch] gap-1">
          <h2 id="push-h">{t('notifications.push.title')}</h2>
          <p className="sm muted">{t('notifications.push.text')}</p>
          {note && <p className="sm">{note}</p>}
        </div>
        {available && state === 'off' && (
          <div><Button icon="bell" busy={busy} onClick={() => run(async () => {
            if (await subscribePush() === 'granted') toast(t('notifications.push.enabled'));
          })}>{t('notifications.push.enable')}</Button></div>
        )}
        {available && state === 'on' && (
          <div className="flex flex-wrap gap-2">
            <Button busy={test.isPending} onClick={() => test.mutateAsync({}).then((r) => toast(t('notifications.push.testSent', { count: r.devices })), toastError)}>{t('notifications.push.test')}</Button>
            <Button variant="ghost" busy={busy} onClick={() => run(async () => { await unsubscribePush(); toast(t('notifications.push.disabled')); })}>{t('notifications.push.disable')}</Button>
          </div>
        )}
      </div>
    </Card>
  );
}
