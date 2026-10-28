import type { Page } from '@playwright/test';

// API fixtures for the screenshot specs (landing-shots, readme-shots): parts
// that depend on the time of day or on things the seed doesn't have.

export const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

/** An ISO instant for hh:mm today in Europe/Berlin (the demo system's zone). */
function berlin(h: number, m = 0) {
  const now = new Date();
  const day = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Berlin' }).format(now);
  const guess = new Date(`${day}T${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:00Z`);
  const local = new Date(guess.toLocaleString('en-US', { timeZone: 'Europe/Berlin' }));
  const utc = new Date(guess.toLocaleString('en-US', { timeZone: 'UTC' }));
  return { day, iso: new Date(guess.getTime() - (local.getTime() - utc.getTime())).toISOString() };
}

// A sunny day with a negative-price dip at noon, as seen in the morning.
export async function withAdvice(page: Page) {
  const slots = Array.from({ length: 12 }, (_, i) => {
    const h = 8 + i;
    const w = Math.max(0, 14_000 * Math.exp(-((h + 0.5 - 13) ** 2) / 8));
    return { start: berlin(h).iso, end: berlin(h + 1).iso, expected_w: Math.round(w), eur_mwh: Math.round(95 - 110 * Math.exp(-((h + 0.5 - 13) ** 2) / 8)) };
  });
  await page.route(/\/api\/v1\/systems\/[^/]+\/advice(\?|$)/, (route) => route.fulfill({ json: {
    available: true, date: berlin(0).day, hours: 3, expected_kwh: 84.6, remaining_kwh: 79.2,
    solar_window: { start: berlin(11, 30).iso, end: berlin(14, 30).iso, expected_kwh: 38.4, mean_w: 12_800, mean_eur_mwh: -6 },
    cheap_window: { start: berlin(12).iso, end: berlin(15).iso, expected_kwh: 37.9, mean_w: 12_630, mean_eur_mwh: -11 },
    negative_prices: [{ start: berlin(12).iso, end: berlin(14, 30).iso, eur_mwh: -15 }],
    slots,
  } }));
}

const hostedSource = {
  id: '00000000-0000-4000-8000-0000000000b1', kind: 'solarlog_hosted', name: 'Solar-Log 1200', enabled: true,
  poll_interval_s: 300, config: { backfill_history: true }, last_run_at: minutesAgo(3), last_success_at: minutesAgo(3), last_error: '',
  upload: { host: 'ftp.example.com', port: 21, username: 'pvk3m7q2xa', directory: '/',
    quota_bytes: 200 * 1024 ** 2, used_bytes: 23 * 1024 ** 2, locked: false, retention_days: 30 },
};

const pvoutputSource = {
  id: '00000000-0000-4000-8000-0000000000b2', kind: 'pvoutput_export', name: 'PVOutput', enabled: true,
  poll_interval_s: 300, config: { pvoutput_system_id: '104233' }, last_run_at: minutesAgo(4), last_success_at: minutesAgo(4), last_error: '',
};

// A Solar-Log that uploads to the FTP account noonside provides, and with
// `pvoutput` an upload to PVOutput.
export async function withHostedSource(page: Page, { pvoutput = false } = {}) {
  await page.route(/\/api\/v1\/systems\/[^/]+\/data-sources$/, async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    await route.fulfill({ json: pvoutput ? [hostedSource, pvoutputSource] : [hostedSource] });
  });
}

export type Member = { name: string; email: string; role: 'owner' | 'admin' | 'viewer'; days: number };

// Members of the system. The first stands for the signed-in user (the one
// whose email starts with `me`), so it stays marked as "you".
export async function withMembers(page: Page, me: string, members: Member[]) {
  await page.route(/\/api\/v1\/systems\/[^/]+\/members$/, async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    const res = await route.fetch();
    const self = (await res.json() as { user_id: string; email: string }[]).find((m) => m.email.startsWith(me));
    await route.fulfill({ response: res, json: members.map((m, i) => ({
      user_id: i === 0 ? self?.user_id : `00000000-0000-4000-8000-0000000000a${i}`,
      name: m.name, email: m.email, role: m.role, created_at: minutesAgo(m.days * 1440),
    })) });
  });
}

// A Home Assistant webhook and a read key for Grafana.
export async function withIntegrations(page: Page) {
  await page.route(/\/api\/v1\/systems\/[^/]+\/webhooks$/, async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    await route.fulfill({ json: [{
      id: '00000000-0000-4000-8000-0000000000c1', name: 'Home Assistant', url: 'https://ha.example.net/api/webhook/noonside',
      events: ['alert.opened', 'alert.resolved', 'daily.summary', 'surplus.forecast'], enabled: true, has_secret: true, locale: 'en',
      created_at: minutesAgo(40 * 1440), last_delivery_at: minutesAgo(600), last_status: 'sent',
    }] });
  });
  await page.route(/\/api\/v1\/systems\/[^/]+\/api-keys$/, async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    await route.fulfill({ json: [{
      id: '00000000-0000-4000-8000-0000000000c2', name: 'Grafana', prefix: 's0k_7f3a', scopes: ['data:read'],
      created_at: minutesAgo(90 * 1440), last_used_at: minutesAgo(2),
    }] });
  });
}
