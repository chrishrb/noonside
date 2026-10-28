import { useCallback, useEffect, useState } from 'react';
import { deletePushSubscription, getPushConfig, savePushSubscription } from '@/api/gen/sdk.gen';
import { isStandalone } from './install';
import { store } from './storage';

// Web Push: the service worker (src/sw.ts) shows the notifications. On iOS
// this only works in the app added to the home screen (iOS 16.4+), and
// permission must be requested from a tap.

const ENABLED = 'noonside.push';
// Tells every usePushState on the page that this device's state changed.
const CHANGED = 'noonside:push';
const changed = () => window.dispatchEvent(new Event(CHANGED));

export const pushSupported = () =>
  typeof window !== 'undefined' && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;

// ready never settles without a service worker (e.g. the Vite dev server).
const registration = () => Promise.race([
  navigator.serviceWorker.ready,
  new Promise<never>((_, reject) => setTimeout(() => reject(new Error('no service worker')), 10_000)),
]);

function keyBytes(base64url: string): Uint8Array<ArrayBuffer> {
  const b64 = (base64url + '='.repeat((4 - (base64url.length % 4)) % 4)).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(b64);
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

async function save(sub: PushSubscription) {
  const json = sub.toJSON();
  await savePushSubscription({
    body: {
      endpoint: sub.endpoint,
      keys: { p256dh: json.keys?.p256dh ?? '', auth: json.keys?.auth ?? '' },
      user_agent: navigator.userAgent.slice(0, 500),
    },
  });
}

async function publicKey(): Promise<string | null> {
  const { data: cfg } = await getPushConfig();
  return cfg.enabled && cfg.public_key ? cfg.public_key : null;
}

/** Asks for permission (call from a tap) and registers this device. */
export async function subscribePush(): Promise<NotificationPermission> {
  const key = await publicKey();
  if (!key) throw new Error('push is not configured on this server');
  const permission = await Notification.requestPermission();
  if (permission !== 'granted') return permission;
  const reg = await registration();
  const sub = (await reg.pushManager.getSubscription()) ?? await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(key) });
  await save(sub);
  store.set(ENABLED, '1');
  changed();
  return permission;
}

/** Stops push on this device (also on sign-out). */
export async function unsubscribePush() {
  store.set(ENABLED, null);
  if (!pushSupported()) return;
  const reg = await navigator.serviceWorker.getRegistration();
  const sub = await reg?.pushManager.getSubscription();
  if (!sub) return;
  await deletePushSubscription({ query: { endpoint: sub.endpoint } }).catch(() => {});
  await sub.unsubscribe();
  changed();
}

/**
 * Re-registers the subscription on start: browsers (Safari in particular)
 * may rotate or drop it, and it moves with the signed-in user.
 */
export function usePushResync() {
  useEffect(() => {
    if (!pushSupported() || Notification.permission !== 'granted' || !store.get(ENABLED)) return;
    void (async () => {
      const reg = await registration();
      let sub = await reg.pushManager.getSubscription();
      if (!sub) {
        const key = await publicKey();
        if (!key) return;
        sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(key) });
      }
      await save(sub);
    })().catch(() => {});
  }, []);
}

export type PushState = 'unsupported' | 'install' | 'blocked' | 'off' | 'on';

async function readPushState(): Promise<PushState> {
  // iOS Safari only has push in the home screen app.
  if (!pushSupported()) return /iPhone|iPad|iPod/.test(navigator.userAgent) && !isStandalone() ? 'install' : 'unsupported';
  if (Notification.permission === 'denied') return 'blocked';
  const reg = await navigator.serviceWorker.getRegistration();
  const sub = await reg?.pushManager.getSubscription();
  return sub && Notification.permission === 'granted' ? 'on' : 'off';
}

/** This device's push state for the settings UI; `loaded` is false until it is known. */
export function usePushState() {
  const [state, setState] = useState<PushState>('off');
  const [loaded, setLoaded] = useState(false);
  const refresh = useCallback(() => readPushState().then((s) => { setState(s); setLoaded(true); }), []);
  useEffect(() => {
    void refresh();
    window.addEventListener(CHANGED, refresh);
    return () => window.removeEventListener(CHANGED, refresh);
  }, [refresh]);
  return { state, loaded, refresh };
}
