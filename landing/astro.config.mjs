// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// The landing site: static, German, no client framework. LANDING_URL is its own origin (canonical URLs, sitemap);
// PUBLIC_APP_URL and PUBLIC_DEMO_URL point to the app (src/lib/site.ts).
const site = process.env.LANDING_URL || 'http://localhost:4321';

// A public build must not go out with the placeholders of the Impressum or
// the local demo link: the operator comes from PUBLIC_OPERATOR_* and
// PUBLIC_HOSTING_* (src/lib/site.ts, landing/.env.production).
const required = ['PUBLIC_OPERATOR_NAME', 'PUBLIC_OPERATOR_STREET', 'PUBLIC_OPERATOR_CITY', 'PUBLIC_OPERATOR_EMAIL', 'PUBLIC_HOSTING_NAME', 'PUBLIC_HOSTING_LOCATION', 'PUBLIC_MAIL_PROVIDER'];
if (site.startsWith('https://')) {
  const missing = required.filter((k) => !process.env[k]);
  if (missing.length) {
    throw new Error(`set ${missing.join(', ')} for a public build (operator for Impressum/Datenschutz)`);
  }
  if (!process.env.PUBLIC_APP_URL || !process.env.PUBLIC_DEMO_URL) {
    throw new Error('set PUBLIC_APP_URL and PUBLIC_DEMO_URL (share link of the demo system, ?kiosk=1) for a public build');
  }
}

export default defineConfig({
  site,
  trailingSlash: 'always',
  // The CSP allows only 'self' for scripts and styles, so nothing inline:
  // stylesheets are files, and the only script is public/scripts/token.js.
  build: { format: 'directory', inlineStylesheets: 'never' },
  devToolbar: { enabled: false },
  server: { port: 4321 },
  integrations: [
    sitemap({ filter: (page) => !/\/(warteliste|bald)\//.test(page) }),
  ],
  vite: {
    // tokens.css comes from the app (web/src/styles).
    server: { fs: { allow: ['..'] } },
    build: { assetsInlineLimit: 0 },
  },
});
