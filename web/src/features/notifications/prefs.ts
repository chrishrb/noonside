import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { TFunction } from 'i18next';
import i18n from '@/i18n';
import { getNotificationPreferencesOptions, getNotificationTargetsOptions, setNotificationPreferencesMutation } from '@/api/gen/@tanstack/react-query.gen';
import type { NotificationChannel, NotificationPreference, NotificationPreferences } from '@/api/types';
import { toast, toastError } from '@/components/ui/toast';
import { fmtList } from '@/lib/format';

export type Pref = Omit<NotificationPreference, 'system_id' | 'muted_until'>;
export type Channel = NotificationChannel;
export type TypeKey = 'alerts' | 'reports' | 'forecast';
/** "default" is the user's default; otherwise a system id. */
export type Scope = 'default' | string;
export type Mode = 'default' | 'custom' | 'muted';

export const TYPES: TypeKey[] = ['alerts', 'reports', 'forecast'];
export const CHANNELS: Channel[] = ['in_app', 'email', 'push', 'ntfy', 'webhook'];
export const BASE = '/profile/notifications';
// A mute without end: the API stores an instant.
const FOREVER = '9999-12-31T00:00:00Z';

const strip = ({ alerts, reports, forecast }: NotificationPreference): Pref => ({ alerts, reports, forecast });
// Channels in display order, so equal settings compare equal.
const norm = (p: Pref): Pref => ({
  alerts: { ...p.alerts, channels: CHANNELS.filter((c) => p.alerts.channels.includes(c)) },
  reports: { ...p.reports, channels: CHANNELS.filter((c) => p.reports.channels.includes(c)) },
  forecast: { ...p.forecast, channels: CHANNELS.filter((c) => p.forecast.channels.includes(c)) },
});
// Pending saves, shared by every usePrefs (see put).
const saves = { seq: 0, queue: Promise.resolve() };

const same = (a: Pref, b: Pref) => JSON.stringify(norm(a)) === JSON.stringify(norm(b));

/**
 * The notification preferences with helpers to change the default or one
 * system. Every change is saved right away; the cache is updated first so
 * switches don't wait for the server.
 */
export function usePrefs() {
  const q = useQuery(getNotificationPreferencesOptions());
  const qc = useQueryClient();
  const { queryKey } = getNotificationPreferencesOptions();
  const save = useMutation(setNotificationPreferencesMutation());
  const data = q.data;

  const system = (id: string) => data?.systems.find((s) => s.system_id === id);
  const modeOf = (id: string): Mode => { const p = system(id); return !p ? 'default' : p.muted_until ? 'muted' : 'custom'; };
  const prefOf = (scope: Scope): Pref => {
    const p = scope === 'default' ? undefined : system(scope);
    return strip(p && !p.muted_until ? p : data!.default);
  };

  const put = (def: Pref, systems: NotificationPreference[]) => {
    const prev = qc.getQueryData<NotificationPreferences>(queryKey);
    if (prev) qc.setQueryData<NotificationPreferences>(queryKey, { ...prev, default: norm(def), systems });
    const my = ++saves.seq;
    // One request at a time, in order; each sends the whole state, so the
    // last one wins, and only its answer replaces the cache.
    saves.queue = saves.queue.then(async () => {
      try {
        const next = await save.mutateAsync({ body: { default: norm(def), systems } });
        if (my === saves.seq) { qc.setQueryData(queryKey, next); toast(i18n.t('notifications.prefs.saved')); }
      } catch (err) {
        toastError(err);
        if (my === saves.seq) await qc.invalidateQueries({ queryKey });
      }
    });
    return saves.queue;
  };
  const others = (id: string) => data!.systems.filter((s) => s.system_id !== id);

  const setPref = (scope: Scope, p: Pref) => {
    if (scope === 'default') return put(p, data!.systems);
    return put(data!.default, [...others(scope), { ...norm(p), system_id: scope }]);
  };
  /** Mode of one system. Unmuting keeps custom settings, if any. */
  const setMode = (id: string, mode: Mode | 'on') => {
    const stored = system(id);
    const own = stored ? strip(stored) : strip(data!.default);
    if (mode === 'on') mode = stored && !same(own, strip(data!.default)) ? 'custom' : 'default';
    if (mode === 'default') return put(data!.default, others(id));
    return put(data!.default, [...others(id), { ...own, system_id: id, ...(mode === 'muted' && { muted_until: FOREVER }) }]);
  };

  return { q, data, modeOf, prefOf, setPref, setMode, saving: save.isPending };
}

/**
 * Channels that can't deliver: email or push when the server doesn't send
 * them, ntfy or webhook until the user set up that target. They show as off
 * and can't be switched on.
 */
export function useUnavailable(): Channel[] {
  const prefs = useQuery(getNotificationPreferencesOptions());
  const targets = useQuery(getNotificationTargetsOptions());
  const out: Channel[] = [];
  if (prefs.data && !prefs.data.email_available) out.push('email');
  if (prefs.data && !prefs.data.push_available) out.push('push');
  if (targets.data && !targets.data.ntfy_url) out.push('ntfy');
  if (targets.data && !targets.data.webhook_url) out.push('webhook');
  return out;
}

/** The channels that actually deliver, in display order. */
export const effective = (channels: Channel[], unavailable: Channel[]) => CHANNELS.filter((c) => channels.includes(c) && !unavailable.includes(c));

/** "In-app, Email and Push", or "No channels". */
export function channelSummary(t: TFunction, channels: Channel[]): string {
  if (!channels.length) return t('notifications.prefs.noChannels');
  return fmtList(channels.map((c) => t(`notifications.prefs.channels.${c}.short`)));
}

/** The summary line of a notification type in the list. */
export function typeSummary(t: TFunction, type: TypeKey, p: Pref, unavailable: Channel[]): string {
  const off = t('notifications.prefs.off');
  const chans = effective(p[type].channels, unavailable);
  switch (type) {
    case 'alerts':
      return chans.length ? `${channelSummary(t, chans)} · ${t(`notifications.prefs.severities.${p.alerts.min_severity}`)}` : off;
    case 'reports':
      return p.reports.digest === 'none' ? off : `${t(`notifications.prefs.digestsShort.${p.reports.digest}`)} · ${channelSummary(t, chans)}`;
    case 'forecast':
      return p.forecast.enabled ? channelSummary(t, chans) : off;
  }
}
