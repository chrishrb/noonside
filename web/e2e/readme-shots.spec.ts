import { test, type Page } from '@playwright/test';
import { DEMO_PV, SHARE_TOKEN } from './accounts';
import { withAdvice, withHostedSource, withIntegrations, withMembers, type Member } from './fixtures';
import { settle } from './layout';

// Screenshots for the README's product tour (docs/screenshots/), English.
// `mise run docs:screens` re-seeds the dev database and runs the readme-*
// projects: desktop shots are the viewport at 1440×900 (2x), phone shots
// dark. Day views show yesterday: the seed's minute data ends with the last
// full day. Settings that the seed leaves empty come from route fixtures.
const sys = `/s/${DEMO_PV}`;
const yesterday = new Date(Date.now() - 86_400_000).toISOString().slice(0, 10);
const lastYear = `${new Date().getFullYear() - 1}-06-01`;

const MEMBERS: Member[] = [
  { name: 'Demo', email: 'demo@noonside.local', role: 'owner', days: 1200 },
  { name: 'Alex Berger', email: 'alex@example.com', role: 'owner', days: 1190 },
  { name: 'Huber Solar (installer)', email: 'service@huber-solar.example', role: 'admin', days: 900 },
  { name: 'Ground floor flat', email: 'tenant@example.com', role: 'viewer', days: 300 },
];

type Shot = { name: string; path: string; phone?: boolean; public?: boolean; before?: (page: Page) => Promise<unknown>; open?: (page: Page) => Promise<unknown> };

/** Scrolls the card with this heading to the top (the phone scrolls in .main). */
const scrollTo = (heading: string) => (page: Page) =>
  page.locator('.card', { has: page.getByRole('heading', { name: heading }) }).first().evaluate((el) => el.scrollIntoView({ block: 'start' }));

const shots: Shot[] = [
  { name: 'dashboard', path: '/' },
  { name: 'yield-day', path: `${sys}/overview?period=day&date=${yesterday}` },
  { name: 'yield-year', path: `${sys}/overview?period=year&date=${lastYear}&cmp=prev` },
  { name: 'yield-total', path: `${sys}/overview?period=total`, open: scrollTo('Daily yield calendar') },
  { name: 'status', path: `${sys}/status`, before: withAdvice },
  { name: 'notifications', path: '/notifications' },
  { name: 'alerts', path: `${sys}/settings/alerts/availability` },
  { name: 'data-sources', path: `${sys}/settings/sources`, before: (p) => withHostedSource(p) },
  { name: 'integrations', path: `${sys}/settings/integrations`, before: async (p) => { await withHostedSource(p, { pvoutput: true }); await withIntegrations(p); } },
  { name: 'members', path: `${sys}/settings/access`, before: (p) => withMembers(p, 'demo@', MEMBERS) },
  { name: 'kiosk', path: `/p/${SHARE_TOKEN}?kiosk=1`, public: true },
  { name: 'mobile-dark', path: '/', phone: true },
  { name: 'mobile-yield-dark', path: `${sys}/overview?period=day&date=${yesterday}`, phone: true, open: scrollTo('Power') },
];

// The app's service worker would make the API requests, past page.route.
test.use({ serviceWorkers: 'block' });

for (const shot of shots) {
  test.describe(shot.name, () => {
    if (shot.public) test.use({ storageState: { cookies: [], origins: [] } });
    test(shot.name, async ({ page }, info) => {
      const phone = info.project.name === 'readme-phone';
      test.skip(phone !== !!shot.phone, 'other viewport');
      // No swipe hint; the system picker shows Demo PV, which the shots are about.
      await page.addInitScript((sysId) => {
        localStorage.setItem('noonside.notifSwipeHint', '1');
        localStorage.setItem('noonside.sys', sysId);
      }, DEMO_PV);
      await shot.before?.(page);
      await page.goto(shot.path);
      await settle(page);
      await shot.open?.(page);
      // No focus ring on the heading the app focuses after navigating, no
      // install prompt or toasts, no hover state under the parked mouse.
      await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
      await page.addStyleTag({ content: `
        *, *::before, *::after { animation: none !important; transition: none !important; caret-color: transparent !important; }
        [data-sonner-toaster], .install-bar { display: none !important; }` });
      await page.mouse.move(0, 0);
      await page.screenshot({ path: `../docs/screenshots/${shot.name}.png` });
    });
  });
}
