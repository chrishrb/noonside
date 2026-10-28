/// <reference lib="webworker" />
// Service worker (vite-plugin-pwa injectManifest): precaches the app shell
// and shows Web Push notifications sent by internal/alerts.
import { cleanupOutdatedCaches, createHandlerBoundToURL, precacheAndRoute } from 'workbox-precaching';
import { NavigationRoute, registerRoute } from 'workbox-routing';

declare const self: ServiceWorkerGlobalScope;

precacheAndRoute(self.__WB_MANIFEST);
cleanupOutdatedCaches();
// API responses are never cached here; offline data comes from the persisted
// query cache (src/api/persist.ts).
registerRoute(new NavigationRoute(createHandlerBoundToURL('/index.html'), {
  // /auth/v1 is the Auth server on the same origin: its email links (/verify)
  // must reach it, not the app shell.
  denylist: [/^\/api\//, /^\/auth\//, /^\/auth-templates\//, /^\/media\//, /^\/healthz/, /^\/readyz/, /^\/openapi\.yaml/],
}));

// src/pwa.ts asks before activating a new version.
self.addEventListener('message', (e) => {
  if (e.data?.type === 'SKIP_WAITING') void self.skipWaiting();
});

interface PushPayload { id?: string; title: string; body: string; url: string; tag?: string; badge?: number }

self.addEventListener('push', (e) => {
  let p: PushPayload;
  try { p = e.data?.json() as PushPayload; } catch { p = { title: 'noonside', body: e.data?.text() ?? '', url: '/notifications' }; }
  const nav = self.navigator as WorkerNavigator & { setAppBadge?: (n?: number) => Promise<void>; clearAppBadge?: () => Promise<void> };
  // Every push must show a notification (iOS revokes the subscription otherwise).
  e.waitUntil(Promise.all([
    self.registration.showNotification(p.title, {
      body: p.body,
      tag: p.tag,
      data: { url: p.url, id: p.id },
      icon: '/pwa-192x192.png',
      badge: '/pwa-64x64.png',
    }),
    p.badge ? nav.setAppBadge?.(p.badge).catch(() => {}) : nav.clearAppBadge?.().catch(() => {}),
  ]));
});

self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const data = e.notification.data as { url?: string; id?: string } | null;
  const url = new URL(data?.url ?? '/notifications', self.location.origin);
  if (url.origin !== self.location.origin) return;
  // The app marks it read (useMarkReadFromUrl); the worker has no access token.
  if (data?.id) url.searchParams.set('notification', data.id);
  e.waitUntil((async () => {
    const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    const win = wins.find((w) => new URL(w.url).origin === url.origin);
    if (win) {
      await win.focus();
      await win.navigate(url.href).catch(() => {});
      return;
    }
    await self.clients.openWindow(url.href);
  })());
});

// The server learns the new subscription when the app opens next
// (usePushResync in src/lib/push.ts); the worker has no access token.
self.addEventListener('pushsubscriptionchange', (e) => {
  const ev = e as Event & { oldSubscription?: PushSubscription; waitUntil(p: Promise<unknown>): void };
  const key = ev.oldSubscription?.options.applicationServerKey;
  if (!key) return;
  ev.waitUntil(self.registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key }));
});
