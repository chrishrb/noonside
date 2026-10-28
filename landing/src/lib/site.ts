// Site-wide settings. Operator details are required for the Impressum
// (§ 5 DDG) and the Datenschutzerklärung. They come from PUBLIC_* env vars at
// build time (landing/.env.production, never committed); a public build
// (https LANDING_URL) fails without them (astro.config.mjs).

const env = import.meta.env;

/** The app (SPA + API), e.g. https://noonside.example.com. */
export const APP_URL: string = (env.PUBLIC_APP_URL || 'http://localhost:5173').replace(/\/$/, '');
/** Base of the waitlist endpoints the forms post to. */
export const API_URL = `${APP_URL}/api/v1`;
/** Share link of the demo system in kiosk mode, opened on the app host. */
export const DEMO_URL: string = env.PUBLIC_DEMO_URL || `${APP_URL}/p/s0p_local-dev-demo-link?kiosk=1`;

export const operator = {
  name: env.PUBLIC_OPERATOR_NAME || 'Vorname Nachname',
  street: env.PUBLIC_OPERATOR_STREET || 'Straße Hausnummer',
  city: env.PUBLIC_OPERATOR_CITY || 'PLZ Ort',
  email: env.PUBLIC_OPERATOR_EMAIL || 'kontakt@example.com',
  phone: env.PUBLIC_OPERATOR_PHONE || '',
};

/** Provider of the server the app and its database run on (Datenschutzerklärung, Auftragsverarbeiter). */
export const hosting = {
  name: env.PUBLIC_HOSTING_NAME || 'Anbieter des Servers',
  location: env.PUBLIC_HOSTING_LOCATION || 'Standort des Rechenzentrums',
};

/** Provider that sends the emails (Datenschutzerklärung, Auftragsverarbeiter). */
export const mailProvider = {
  name: env.PUBLIC_MAIL_PROVIDER || 'E-Mail-Anbieter',
  location: env.PUBLIC_MAIL_PROVIDER_LOCATION || 'Rechenzentrum in der EU',
};

/** Pricing shown on the fake-door cards (the monthly price leads, it reads cheaper). */
export const plans = {
  free: { name: 'Free', price: '0 €', period: 'für immer' },
  pro: { name: 'Pro', price: '4,90 €', period: 'pro Monat, oder 49 € im Jahr' },
  installer: { name: 'Installateure', price: 'auf Anfrage', period: 'pro Kundenanlage' },
} as const;

export type Plan = keyof typeof plans;
