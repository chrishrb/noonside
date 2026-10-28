// Seed accounts (backend/db/seed.sql); override for another stack.
export const email = process.env.E2E_EMAIL ?? 'demo@noonside.local';
export const password = process.env.E2E_PASSWORD ?? 'noonside-dev-pw';
// The German run has its own account: the app saves the browser language on
// the account, and server-rendered texts (alerts) follow it.
export const emailDe = process.env.E2E_EMAIL_DE ?? 'demo-de@noonside.local';
// A superadmin, for the admin pages.
export const emailAdmin = process.env.E2E_EMAIL_ADMIN ?? 'dev@noonside.local';

// Seeded ids, so pages can be opened directly.
export const DEMO_PV = 'd0000000-0000-4000-8000-000000000010';
export const SHARE_TOKEN = 's0p_local-dev-demo-link';
export const DEMO_USER = 'd0000000-0000-4000-8000-000000000002';
