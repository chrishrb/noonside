import { defineConfig, devices, type Project } from '@playwright/test';

// Runs against a running stack (`mise run dev:stack`) with the seed accounts
// from backend/db/seed.sql. `auth.setup.ts` signs in once; the other projects
// reuse the session.
export const AUTH = 'e2e/.auth/demo.json';
export const AUTH_DE = 'e2e/.auth/demo-de.json';
// Superadmin (dev@), for the admin pages.
export const AUTH_ADMIN = 'e2e/.auth/admin.json';
const signedIn = { storageState: AUTH };

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  use: { baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:5173', trace: 'retain-on-failure' },
  projects: [
    { name: 'setup', testMatch: /auth\.setup\.ts/ },
    // The smoke test signs in itself, on desktop only.
    { name: 'desktop', testIgnore: /(landing|readme)-shots\.spec\.ts/, use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 }, ...signedIn }, dependencies: ['setup'] },
    { name: 'desktop-dark', testMatch: /layout\.spec\.ts/, use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 }, colorScheme: 'dark', ...signedIn }, dependencies: ['setup'] },
    { name: 'tablet', testMatch: /layout\.spec\.ts/, use: { ...devices['Desktop Chrome'], viewport: { width: 768, height: 1024 }, isMobile: true, hasTouch: true, ...signedIn }, dependencies: ['setup'] },
    { name: 'phone', testMatch: /layout\.spec\.ts/, use: { ...devices['iPhone 13'], browserName: 'chromium', ...signedIn }, dependencies: ['setup'] },
    // German labels are longer; the app picks the language from the browser.
    { name: 'phone-de', testMatch: /layout\.spec\.ts/, use: { ...devices['iPhone 13'], browserName: 'chromium', locale: 'de-DE', storageState: AUTH_DE }, dependencies: ['setup'] },
    // Screenshots for the landing site (`mise run landing:screens` sets
    // LANDING_SHOTS), German; not part of the normal runs.
    ...(process.env.LANDING_SHOTS ? landingProjects() : []),
    // Screenshots for the README (`mise run docs:screens` sets README_SHOTS),
    // English; not part of the normal runs.
    ...(process.env.README_SHOTS ? readmeProjects() : []),
  ],
});

function landingProjects(): Project[] {
  const common = { testMatch: /landing-shots\.spec\.ts/, dependencies: ['setup'] };
  const de = { locale: 'de-DE', storageState: AUTH_DE };
  return (['light', 'dark'] as const).flatMap((colorScheme): Project[] => [
    // 2x, so the screenshots stay sharp on high-density screens. Desktop shots
    // are card crops; 1100 px gives cards about 800 px wide, the width they
    // are shown at on the landing site.
    { name: `landing-desktop-${colorScheme}`, ...common, use: { ...devices['Desktop Chrome'], viewport: { width: 1100, height: 1000 }, deviceScaleFactor: 2, colorScheme, ...de } },
    // The iPhone 13 screen (844) without the status bar (47), which the
    // landing site's phone frame draws.
    { name: `landing-phone-${colorScheme}`, ...common, use: { ...devices['iPhone 13'], browserName: 'chromium', viewport: { width: 390, height: 797 }, colorScheme, ...de } },
  ]);
}

function readmeProjects(): Project[] {
  const common = { testMatch: /readme-shots\.spec\.ts/, dependencies: ['setup'] };
  return [
    { name: 'readme-desktop', ...common, use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, locale: 'en-US', ...signedIn } },
    { name: 'readme-phone', ...common, use: { ...devices['iPhone 13'], browserName: 'chromium', colorScheme: 'dark', locale: 'en-US', ...signedIn } },
  ];
}
