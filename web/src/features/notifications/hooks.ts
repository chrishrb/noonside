import { useEffect, useSyncExternalStore } from 'react';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import { type InfiniteData, type QueryClient, type QueryFilters, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast as sonner } from 'sonner';
import { deleteNotifications } from '@/api/gen/sdk.gen';
import { deleteNotificationsMutation, getUnreadNotificationCountOptions, markNotificationsReadMutation, updateMeMutation } from '@/api/gen/@tanstack/react-query.gen';
import { queryId, useInvalidate } from '@/api/queries';
import type { Me, Notification, NotificationPage } from '@/api/types';
import { toastError } from '@/components/ui/toast';
import i18n, { currentLanguage } from '@/i18n';

// Filters for every cached variant (any params, infinite or not).
const LIST: QueryFilters = { predicate: (q) => queryId(q) === 'listNotifications' };
const COUNT: QueryFilters = { predicate: (q) => queryId(q) === 'getUnreadNotificationCount' };
const UNDO_MS = 5000;

// --- removed, waiting for undo ----------------------------------------------

// Removed notifications stay hidden here until the delete went through, so a
// refetch in between can't bring them back.
const pending = new Map<string, { n: Notification; timer: ReturnType<typeof setTimeout>; qc: QueryClient }>();
const listeners = new Set<() => void>();
let hidden: ReadonlySet<string> = new Set();
let hiddenUnread = 0;

function changed() {
  hidden = new Set(pending.keys());
  hiddenUnread = [...pending.values()].filter((p) => !p.n.read_at).length;
  listeners.forEach((l) => l());
}

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => { listeners.delete(l); };
};

/** Ids of notifications removed but not yet deleted (hide them in lists). */
export const useHiddenNotifications = () => useSyncExternalStore(subscribe, () => hidden);
const useHiddenUnread = () => useSyncExternalStore(subscribe, () => hiddenUnread);

async function commit(ids: string[], keepalive = false) {
  const entries = ids.map((id) => pending.get(id)).filter((p) => !!p);
  entries.forEach((p) => { clearTimeout(p.timer); sonner.dismiss(`notif-removed-${p.n.id}`); });
  if (!entries.length) return;
  const qc = entries[0].qc;
  try {
    await deleteNotifications({ body: { ids: entries.map((p) => p.n.id) }, keepalive });
  } catch (err) {
    toastError(err);
  }
  await Promise.all([qc.invalidateQueries(LIST), qc.invalidateQueries(COUNT)]);
  entries.forEach((p) => pending.delete(p.n.id));
  changed();
}

// Leaving or backgrounding the app deletes right away (undo is gone then).
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden' && pending.size) void commit([...pending.keys()], true);
  });
}

/** Removes a notification after a few seconds, with an undo toast. */
export function useDismiss() {
  const qc = useQueryClient();
  return (n: Notification) => {
    if (pending.has(n.id)) return;
    const timer = setTimeout(() => void commit([n.id]), UNDO_MS);
    pending.set(n.id, { n, timer, qc });
    changed();
    sonner(i18n.t('notifications.removed'), {
      id: `notif-removed-${n.id}`,
      className: 'toast',
      duration: UNDO_MS,
      action: {
        label: i18n.t('notifications.undo'),
        onClick: () => {
          const p = pending.get(n.id);
          if (!p) return;
          clearTimeout(p.timer);
          pending.delete(n.id);
          changed();
        },
      },
    });
  };
}

// --- read state ---------------------------------------------------------------

type ListData = NotificationPage | InfiniteData<NotificationPage>;

/** Applies fn to every cached page of notifications. */
function updatePages(qc: QueryClient, fn: (p: NotificationPage) => NotificationPage) {
  qc.setQueriesData<ListData>(LIST, (d) => {
    if (!d) return d;
    return 'pages' in d ? { ...d, pages: d.pages.map(fn) } : fn(d);
  });
}

/** Unread notifications, polled while the app is open. */
export function useUnreadCount() {
  const q = useQuery({ ...getUnreadNotificationCountOptions(), refetchInterval: 60_000 });
  return Math.max(0, (q.data?.count ?? 0) - useHiddenUnread());
}

/** Shows the unread count on the installed app's icon (iOS 16.4+, Chrome, Edge). */
export function useAppBadge(count: number) {
  useEffect(() => {
    if (!('setAppBadge' in navigator)) return;
    // Rejects without notification permission (iOS); nothing to do then.
    const p = count > 0 ? navigator.setAppBadge(count) : navigator.clearAppBadge();
    p.catch(() => {});
  }, [count]);
}

/** Marks notifications read (shown at once) and refreshes the inbox and the badge. */
export function useMarkRead() {
  const mark = useMutation(markNotificationsReadMutation());
  const qc = useQueryClient();
  const invalidate = useInvalidate();
  return {
    pending: mark.isPending,
    mark: async (body: { ids?: string[]; all?: boolean }) => {
      const ids = new Set(body.ids);
      const hit = (n: Notification) => !n.read_at && (body.all || ids.has(n.id));
      // Unread ones among them that are on screen; the rest shows after the refetch.
      const seen = new Set<string>();
      for (const [, d] of qc.getQueriesData<ListData>(LIST)) {
        const pages = !d ? [] : 'pages' in d ? d.pages : [d];
        pages.forEach((p) => p.items.filter(hit).forEach((n) => seen.add(n.id)));
      }
      const now = new Date().toISOString();
      const fewer = (c: number) => (body.all ? 0 : Math.max(0, c - seen.size));
      updatePages(qc, (p) => ({ ...p, unread: fewer(p.unread), items: p.items.map((n) => (hit(n) ? { ...n, read_at: now } : n)) }));
      qc.setQueriesData<{ count: number }>(COUNT, (d) => d && { count: fewer(d.count) });
      try {
        await mark.mutateAsync({ body });
      } finally {
        await invalidate('listNotifications', 'getUnreadNotificationCount');
      }
    },
  };
}

/** Deletes all read notifications. */
export function useClearRead() {
  const del = useMutation(deleteNotificationsMutation());
  const invalidate = useInvalidate();
  return async () => {
    try {
      await del.mutateAsync({ body: { read: true } });
    } finally {
      await invalidate('listNotifications', 'getUnreadNotificationCount');
    }
  };
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Opening a push adds `?notification=<id>` (src/sw.ts): mark it read and drop the parameter. */
export function useMarkReadFromUrl() {
  const [search, setSearch] = useSearchParams();
  const { mark } = useMarkRead();
  const id = search.get('notification');
  useEffect(() => {
    if (id === null) return;
    if (UUID.test(id)) mark({ ids: [id] }).catch(() => {}); // not essential; it stays unread
    setSearch((s) => { s.delete('notification'); return s; }, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);
}

/** Tells the server the UI language, used for emails and push notifications. */
export function useLocaleSync(me: Me) {
  const { i18n } = useTranslation();
  const update = useMutation(updateMeMutation());
  const invalidate = useInvalidate();
  const lang = currentLanguage();
  useEffect(() => {
    if (me.locale === lang || update.isPending) return;
    update.mutateAsync({ body: { locale: lang } })
      .then(() => invalidate('getMe', 'listNotifications', 'listAlerts'))
      .catch(() => {}); // retried on the next render with a stale locale
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [me.locale, lang, i18n.language]);
}
