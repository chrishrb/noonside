import { useSyncExternalStore } from 'react';
import { store } from './storage';

// Chromium's install prompt event (not in the DOM typings).
interface InstallPromptEvent extends Event {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

const DISMISSED = 'noonside.installDismissed';
const DISMISS_DAYS = 30;

let deferred: InstallPromptEvent | null = null;
let installed = false;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

// Registered at import (main.tsx imports this early): the event may fire
// before React renders.
if (typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault(); // we show our own notice instead of the mini-infobar
    deferred = e as InstallPromptEvent;
    emit();
  });
  window.addEventListener('appinstalled', () => { deferred = null; installed = true; emit(); });
}

/** Running as the installed app (home screen / app window). */
export const isStandalone = () =>
  window.matchMedia?.('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone === true;

// iOS has no install prompt: people add the app via Share → Add to Home Screen.
// iPadOS reports itself as a Mac, so check for touch too.
const isIOS = () => /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.userAgent.includes('Macintosh') && navigator.maxTouchPoints > 1);

export interface InstallState {
  /** Running as the installed app, or just installed from this page. */
  standalone: boolean;
  /** The browser can show its install dialog (Chromium). */
  canPrompt: boolean;
  /** iOS/iPadOS: install by hand from the share menu. */
  ios: boolean;
  /** The user closed the notice recently. */
  dismissed: boolean;
}

let snapshot: InstallState | null = null;
function read(): InstallState {
  const until = Number(store.get(DISMISSED) ?? 0);
  const next = { standalone: installed || isStandalone(), canPrompt: !!deferred, ios: isIOS(), dismissed: until > Date.now() };
  // useSyncExternalStore needs a stable snapshot between changes.
  if (!snapshot || Object.entries(next).some(([k, v]) => snapshot![k as keyof InstallState] !== v)) snapshot = next;
  return snapshot;
}

export function useInstall(): InstallState {
  return useSyncExternalStore((cb) => { listeners.add(cb); return () => listeners.delete(cb); }, read);
}

/** Opens the browser's install dialog; false if unavailable or declined. */
export async function promptInstall(): Promise<boolean> {
  if (!deferred) return false;
  const e = deferred;
  deferred = null; // the event can be used once
  emit();
  await e.prompt();
  return (await e.userChoice).outcome === 'accepted';
}

export function dismissInstall() {
  store.set(DISMISSED, String(Date.now() + DISMISS_DAYS * 24 * 60 * 60 * 1000));
  emit();
}
