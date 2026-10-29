import { useSyncExternalStore } from 'react';
import { THEME, store } from './storage';

export type Theme = 'system' | 'light' | 'dark' | 'pink';

const THEMES: Theme[] = ['system', 'light', 'dark', 'pink'];

export const getTheme = (): Theme => {
  const t = store.get(THEME);
  return THEMES.includes(t as Theme) ? (t as Theme) : 'system';
};

/** The pink-heart emoji, used as the mark of pink mode. */
export const PINK_HEART = '🩷';

// --surface from tokens.css: the colour of the top bar, used for the
// browser/status bar of the installed app.
const SURFACE = { light: '#ffffff', dark: '#17181b', pink: '#ffcfe3' };
const darkQuery = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;

function applyThemeColor(t: Theme) {
  const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  if (!meta) return;
  if (t === 'pink') {
    meta.content = SURFACE.pink;
    return;
  }
  const dark = t === 'dark' || (t === 'system' && !!darkQuery?.matches);
  meta.content = dark ? SURFACE.dark : SURFACE.light;
}

darkQuery?.addEventListener?.('change', () => applyThemeColor(getTheme()));

export function applyTheme(t: Theme = getTheme()) {
  const root = document.documentElement;
  if (t === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', t);
  applyThemeColor(t);
}

const listeners = new Set<() => void>();

export function setTheme(t: Theme) {
  store.set(THEME, t === 'system' ? null : t);
  applyTheme(t);
  listeners.forEach((l) => l());
}

/** The chosen theme; re-renders when it changes (menu and profile stay in sync). */
export function useTheme(): Theme {
  return useSyncExternalStore((l) => { listeners.add(l); return () => { listeners.delete(l); }; }, getTheme);
}
