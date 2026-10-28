// Per-browser conveniences; storage may be unavailable (private mode).
export const store = {
  get(key: string): string | null {
    try { return localStorage.getItem(key); } catch { return null; }
  },
  set(key: string, value: string | null) {
    try { if (value == null) localStorage.removeItem(key); else localStorage.setItem(key, value); } catch { /* ignore */ }
  },
};
export const LAST_SYSTEM = 'noonside.sys';
export const THEME = 'noonside.theme';
export const LANGUAGE = 'noonside.lang';
export const FLEET_VIEW = 'noonside.fleetView';
export const SWIPE_HINT = 'noonside.notifSwipeHint';
