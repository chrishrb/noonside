// Settings of an installation, read at runtime from /config.js (index.html),
// which the web container writes from its environment (web/nginx). The same
// image runs anywhere; VITE_* env vars stand in for `vite` in development.

interface RuntimeConfig {
  sentryDsn?: string;
  sentryEnvironment?: string;
  sentryTracesSampleRate?: number;
  sentryReplaysOnErrorSampleRate?: number;
}

declare global {
  interface Window {
    __NOONSIDE__?: RuntimeConfig;
  }
}

const runtime: RuntimeConfig = (typeof window !== 'undefined' && window.__NOONSIDE__) || {};

/** A sample rate from the config: a number in [0, 1], else the fallback. */
function rate(v: unknown, fallback: number): number {
  const n = typeof v === 'string' && v !== '' ? Number(v) : v;
  return typeof n === 'number' && n >= 0 && n <= 1 ? n : fallback;
}

export const config = {
  sentryDsn: runtime.sentryDsn || import.meta.env.VITE_SENTRY_DSN || undefined,
  sentryEnvironment: runtime.sentryEnvironment || import.meta.env.VITE_SENTRY_ENVIRONMENT || undefined,
  sentryTracesSampleRate: rate(runtime.sentryTracesSampleRate, 0.1),
  sentryReplaysOnErrorSampleRate: rate(runtime.sentryReplaysOnErrorSampleRate, 1),
};
