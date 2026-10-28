import { test, type Locator, type Page } from '@playwright/test';
import { DEMO_PV } from './accounts';
import { minutesAgo, withAdvice, withHostedSource, withMembers, type Member } from './fixtures';
import { settle } from './layout';

// Screenshots for the landing site (landing/): German, in light and dark.
// `mise run landing:screens` re-seeds the dev database, runs the landing-*
// projects and writes them to landing/src/assets/screens/<project>/<name>.png.
//
// Desktop shots are crops of single cards (`crop`), so the landing site shows
// one idea per picture instead of a whole page with sidebar and account.
// Day views show yesterday: the seed's minute data ends with the last full
// day, so today is still empty in the morning. Parts that depend on the time
// of day or on things the seed doesn't have (advice, a hosted FTP source,
// realistic members, a tidy inbox) come from route fixtures.
const sys = `/s/${DEMO_PV}`;
const yesterday = new Date(Date.now() - 86_400_000).toISOString().slice(0, 10);
const lastYear = `${new Date().getFullYear() - 1}-06-01`;

type Shot = {
  name: string;
  path: string;
  phone?: boolean;
  desktop?: boolean;
  before?: (page: Page) => Promise<unknown>;
  tidy?: (page: Page) => Promise<unknown>;
  /** The element to capture; without it the viewport. */
  crop?: (page: Page) => Locator;
};

const card = (page: Page, heading: string | RegExp) =>
  page.locator('.card', { has: page.getByRole('heading', { name: heading }) }).first();

// A household with its installer: two owners, the installer as admin, a
// tenant who may only look. The signed-in user stays the first row ("Sie").
const MEMBERS: Member[] = [
  { name: 'Martina Berger', email: 'martina@beispiel.de', role: 'owner', days: 1200 },
  { name: 'Jonas Berger', email: 'jonas@beispiel.de', role: 'owner', days: 1190 },
  { name: 'Solartechnik Huber', email: 'service@solartechnik-huber.de', role: 'admin', days: 900 },
  { name: 'Wohnung EG', email: 'eg@beispiel.de', role: 'viewer', days: 300 },
];

const shots: Shot[] = [
  { name: 'history-calendar', path: `${sys}/overview?period=total`, crop: (p) => card(p, 'Kalender der Tageserträge') },
  { name: 'history-years', path: `${sys}/overview?period=total`, tidy: chartOnly, crop: (p) => card(p, 'Energie pro Jahr') },
  { name: 'yield-year', path: `${sys}/overview?period=year&date=${lastYear}&cmp=prev`, tidy: chartOnly, crop: (p) => card(p, 'Energie pro Monat') },
  { name: 'yield-day', path: `${sys}/overview?period=day&date=${yesterday}&price=off`, tidy: chartOnly, crop: (p) => card(p, 'Leistung') },
  { name: 'advice', path: `${sys}/status`, before: withAdvice, crop: (p) => card(p, 'Große Verbraucher heute') },
  { name: 'hosted-ftp', path: `${sys}/settings/sources`, before: (p) => withHostedSource(p), crop: (p) => card(p, 'Solar-Log 1200') },
  { name: 'members', path: `${sys}/settings/members`, before: (p) => withMembers(p, 'demo-de', MEMBERS), crop: (p) => p.locator('.card', { has: p.locator('table') }).first() },
  { name: 'notifications', path: '/notifications', phone: true, desktop: false, before: withCuratedInbox, tidy: listOnly },
];

// One of each: an open fault, the morning forecast, a fixed error and the
// monthly report, with fresh times. Texts are the server's own.
async function withCuratedInbox(page: Page) {
  await page.route(/\/api\/v1\/me\/notifications(\?|$)/, async (route) => {
    const res = await route.fetch();
    type N = { type: string; severity: string; title: string; created_at: string; read_at?: string };
    const items = ((await res.json()) as { items: N[] }).items;
    const picks: [(n: N) => boolean, number][] = [
      [(n) => n.type === 'alert_opened' && /^WR 3 liefert weniger, als das Wetter erlaubt/.test(n.title), 25],
      [(n) => n.type === 'forecast', 190],
      [(n) => n.type === 'alert_resolved' && n.severity === 'error', 1440 + 200],
      [(n) => n.type === 'report', 2 * 1440 + 300],
    ];
    const list = picks.flatMap(([match, age], i) => {
      const n = items.find(match);
      return n ? [{ ...n, created_at: minutesAgo(age), read_at: i === 0 ? undefined : minutesAgo(age - 5) }] : [];
    });
    await route.fulfill({ response: res, json: { items: list, unread: 1 } });
  });
  await page.route(/\/api\/v1\/me\/notifications\/unread-count/, (route) => route.fulfill({ json: { count: 1 } }));
}

// Inside a chart card only the chart and its weather credit (Open-Meteo,
// CC BY 4.0): no price toggle, no negative-price line, no data table.
async function chartOnly(page: Page) {
  await page.evaluate(() => {
    for (const el of document.querySelectorAll('.card button, .card p, .card a, .card details, .card summary')) {
      const text = el.textContent ?? '';
      if (/^Day-Ahead-Preis$|Bei negativen Preisen/.test(text.trim())) el.remove();
    }
    for (const el of document.querySelectorAll('.card *')) {
      if (el.children.length < 4 && /^Datentabelle/.test(el.textContent?.trim() ?? '') && el.closest('.card') !== el) {
        const row = el.closest('details, .card-foot, .disclosure') ?? el;
        row.remove();
      }
    }
  });
}

// Only the title and the list: the toolbar would fill half the phone.
async function listOnly(page: Page) {
  await page.evaluate(() => {
    const list = document.querySelector('.notif-page');
    list?.previousElementSibling?.remove(); // "Nur ungelesene"
    for (const b of document.querySelectorAll('button')) {
      if (/entfernen|gelesen markieren/.test(b.textContent ?? '') && !b.closest('.notif-page')) b.parentElement?.remove();
    }
  });
}

// The app's service worker would make the API requests, past page.route.
test.use({ serviceWorkers: 'block' });

for (const s of shots) {
  test.describe(s.name, () => {
    test(`landing shot ${s.name}`, async ({ page }, info) => {
      const phone = info.project.name.startsWith('landing-phone');
      test.skip(phone ? !s.phone : s.desktop === false, 'not shown on the landing site');
      // The swipe hint would take the top of the notifications; the header
      // shows Demo PV, which all shots are about.
      await page.addInitScript((sysId) => {
        localStorage.setItem('noonside.notifSwipeHint', '1');
        localStorage.setItem('noonside.sys', sysId);
      }, DEMO_PV);
      await s.before?.(page);
      await page.goto(s.path);
      await settle(page);
      // No focus ring on the heading the app focuses after navigating, no
      // PWA install prompt, no hover state under the parked mouse.
      await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
      await page.addStyleTag({ content: '.install-bar { display: none !important; }' });
      await page.mouse.move(0, 0);
      // Chromium has no safe areas; on an iPhone the tab bar sits above the
      // home indicator (34 px), which the landing site's frame draws.
      if (phone) await page.addStyleTag({ content: '.tabbar { height: calc(var(--tabbar-h) + 34px) !important; padding-bottom: 34px !important; }' });
      await s.tidy?.(page);
      const path = `../landing/src/assets/screens/${info.project.name.replace(/^landing-/, '')}/${s.name}.png`;
      if (s.crop) await s.crop(page).screenshot({ path, animations: 'disabled' });
      else await page.screenshot({ path });
    });
  });
}
