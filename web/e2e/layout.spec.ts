import { expect, test, type Page } from '@playwright/test';
import { AUTH_ADMIN } from '../playwright.config';
import { DEMO_PV, DEMO_USER, SHARE_TOKEN } from './accounts';
import { layoutProblems, settle, shoot, watchErrors } from './layout';

// Every important page on every viewport project (see playwright.config.ts):
// nothing may stick out of the screen, no errors, and a screenshot to look
// at (`mise run web:shots`). Add new pages here.
const sys = `/s/${DEMO_PV}`;
// `open` runs after loading, e.g. to open a menu for the screenshot.
const pages: { name: string; path: string; public?: boolean; admin?: boolean; before?: (page: Page) => Promise<unknown>; open?: (page: Page) => Promise<unknown> }[] = [
  { name: 'dashboard', path: '/' },
  { name: 'dashboard-table', path: '/', before: (p) => p.addInitScript(() => localStorage.setItem('noonside.fleetView', 'table')) },
  { name: 'dashboard-map', path: '/', before: (p) => p.addInitScript(() => localStorage.setItem('noonside.fleetView', 'map')) },
  { name: 'yield-day', path: `${sys}/overview?period=day` },
  { name: 'yield-day-split', path: `${sys}/overview?period=day&view=split&table=1` },
  { name: 'yield-month-compare', path: `${sys}/overview?period=month&cmp=all` },
  { name: 'yield-year', path: `${sys}/overview?period=year&cmp=prev&table=1` },
  { name: 'yield-total', path: `${sys}/overview?period=total` },
  { name: 'status', path: `${sys}/status` },
  { name: 'export', path: `${sys}/export` },
  { name: 'notifications', path: '/notifications' },
  ...['account', 'notifications', 'appearance', 'systems'].map((tab) => ({ name: `profile-${tab}`, path: `/profile/${tab}` })),
  ...['alerts', 'reports', 'push', 'ntfy', 'webhook', `systems/${DEMO_PV}`].map((sub) => ({ name: `profile-notifications-${sub.split('/')[0]}`, path: `/profile/notifications/${sub}` })),
  // The sidebar chip from 1024px, the top bar avatar below.
  { name: 'user-menu', path: '/', open: (p) => p.locator('.user-chip:visible, .topbar button:has(.avatar):visible').first().click() },
  ...['general', 'inverters', 'sources', 'alerts', 'integrations', 'access'].map((tab) => ({ name: `settings-${tab}`, path: `${sys}/settings/${tab}` })),
  // Section links (emails, webhooks) open their tab and scroll to the section.
  { name: 'settings-keys-link', path: `${sys}/settings/keys` },
  { name: 'settings-push-link', path: `${sys}/settings/push` },
  { name: 'settings-pvoutput-link', path: `${sys}/settings/pvoutput` },
  { name: 'settings-alerts-group', path: `${sys}/settings/alerts/availability` },
  // As a superadmin.
  { name: 'admin-users', path: '/admin/users', admin: true },
  { name: 'admin-user', path: `/admin/users/${DEMO_USER}`, admin: true },
  { name: 'admin-systems', path: '/admin/systems', admin: true },
  { name: 'public', path: `/p/${SHARE_TOKEN}`, public: true },
  { name: 'public-month', path: `/p/${SHARE_TOKEN}?period=month`, public: true },
  { name: 'kiosk', path: `/p/${SHARE_TOKEN}?kiosk=1`, public: true },
];

for (const pg of pages) {
  test.describe(pg.name, () => {
    if (pg.public) test.use({ storageState: { cookies: [], origins: [] } });
    if (pg.admin) test.use({ storageState: AUTH_ADMIN });
    test(`layout of ${pg.name}`, async ({ page }, info) => {
      const errors = watchErrors(page);
      await pg.before?.(page);
      await page.goto(pg.path);
      await settle(page);
      if (pg.open) { await pg.open(page); await page.waitForTimeout(300); }
      const problems = await layoutProblems(page);
      await shoot(page, info, pg.name);
      expect(problems, 'elements outside the screen').toEqual([]);
      expect(errors, 'errors while loading').toEqual([]);
    });
  });
}
