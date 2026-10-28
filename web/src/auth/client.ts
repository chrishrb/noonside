import { AuthClient } from '@supabase/auth-js';

// The Auth server (GoTrue) is served under /auth/v1 of the app's own origin
// (the gateway in production, the Vite proxy in dev), so one build works on
// any host.
export const authClient = new AuthClient({
  url: `${window.location.origin}/auth/v1`,
  storageKey: 'noonside.auth',
  persistSession: true,
  autoRefreshToken: true,
  detectSessionInUrl: true,
});
