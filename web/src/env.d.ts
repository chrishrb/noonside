/** Build id, set in vite.config.ts; changes the persisted query cache key. */
declare const __APP_VERSION__: string;

interface ImportMetaEnv {
  /** Sentry DSN for `vite` dev; deployed apps read it from /config.js (lib/runtimeConfig.ts). */
  readonly VITE_SENTRY_DSN?: string;
  readonly VITE_SENTRY_ENVIRONMENT?: string;
}
