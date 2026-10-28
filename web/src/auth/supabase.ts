import { createClient } from '@supabase/supabase-js';

// The Auth server (GoTrue) is served under /auth/v1 of the app's own origin
// (the gateway in production, the Vite proxy in dev), so one build works on
// any host. VITE_SUPABASE_URL overrides it. GoTrue doesn't check the
// publishable key, but supabase-js requires one.
const url = import.meta.env.VITE_SUPABASE_URL || window.location.origin;
const key = import.meta.env.VITE_SUPABASE_PUBLISHABLE_KEY || 'noonside';

export const supabase = createClient(url, key, {
  auth: { persistSession: true, autoRefreshToken: true, detectSessionInUrl: true },
});
