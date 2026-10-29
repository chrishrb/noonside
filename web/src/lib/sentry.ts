// Error reporting, tracing, error replays and the "report a problem" form,
// all through Sentry. Off without a DSN in the runtime config
// (lib/runtimeConfig.ts; local dev, tests, installations without Sentry). Events carry only the user ID: tokens in URLs,
// query strings and fragments are removed, and replays mask all text, inputs
// and media. API errors (ApiError) are never reported here: 5xx are reported
// by the API itself, the rest are expected (offline, permissions).
import * as Sentry from '@sentry/react';
import { reactRouterBrowserTracingIntegration } from '@sentry/react/react-router';
import type { Event, EventHint } from '@sentry/react';
import { config } from './runtimeConfig';

const DSN = config.sentryDsn;

export const sentryEnabled = Boolean(DSN);

const TOKEN_PATH = /(\/(?:p|public|invitations)\/)[^/?#\s]+/g;
const QUERY_OR_FRAGMENT = /[?#][^\s]*/g;

/** Removes tokens in paths (share links, invitations), query strings and fragments. */
export function scrubUrl(s: string): string {
  return s.replace(TOKEN_PATH, '$1[token]').replace(QUERY_OR_FRAGMENT, '');
}

function scrubEvent<T extends Event>(event: T): T {
  if (event.request) {
    if (event.request.url) event.request.url = scrubUrl(event.request.url);
    delete event.request.query_string;
    if (event.request.headers) delete event.request.headers.Referer;
  }
  if (event.transaction) event.transaction = scrubUrl(event.transaction);
  for (const b of event.breadcrumbs ?? []) scrubData(b.data);
  return event;
}

// Breadcrumb and span data with URLs: fetch/xhr (url), navigation (from, to).
function scrubData(data: Record<string, unknown> | undefined) {
  if (!data) return;
  for (const k of ['url', 'from', 'to', 'http.url', 'url.full', 'http.query', 'http.fragment']) {
    if (typeof data[k] === 'string') data[k] = scrubUrl(data[k]);
  }
}

/**
 * Scrubs a replay recording event: navigation and request spans
 * (payload.description) and page meta (href). beforeSend doesn't see replays.
 */
export function scrubRecordingEvent<T extends { data?: unknown }>(event: T): T {
  const d = event.data as { href?: unknown; payload?: { description?: unknown } } | undefined;
  if (typeof d?.href === 'string') d.href = scrubUrl(d.href);
  if (typeof d?.payload?.description === 'string') d.payload.description = scrubUrl(d.payload.description);
  return event;
}

/** Errors thrown for API responses (ApiError) are named so; see api/client.ts. */
function isApiError(hint: EventHint): boolean {
  const e = hint.originalException;
  return e instanceof Error && e.name === 'ApiError';
}

export function initSentry() {
  if (!DSN) return;
  Sentry.init({
    dsn: DSN,
    // Through the API (errtrack.TunnelPath in Go): ad blockers block
    // Sentry's ingest host, and the CSP doesn't allow it.
    tunnel: '/api/v1/diag',
    release: __APP_VERSION__,
    environment: config.sentryEnvironment,
    // Nothing about the user but the ID we set (setSentryUser).
    dataCollection: { userInfo: false, cookies: false, httpHeaders: false, httpBodies: [], urlQueryParams: false },
    // Replay and feedback follow in loadLazy.
    integrations: [reactRouterBrowserTracingIntegration()],
    tracesSampleRate: config.sentryTracesSampleRate,
    // Continue traces into the Go API (same origin).
    tracePropagationTargets: [/^\/api\/v1\//],
    replaysSessionSampleRate: 0,
    replaysOnErrorSampleRate: config.sentryReplaysOnErrorSampleRate,
    beforeSend: (event, hint) => (isApiError(hint) ? null : scrubEvent(event)),
    beforeSendSpan: (span) => {
      span.name = scrubUrl(span.name);
      scrubData(span.attributes);
      return span;
    },
    beforeBreadcrumb: (b) => {
      scrubData(b.data);
      if (b.message) b.message = scrubUrl(b.message);
      return b;
    },
  });
  // Replay events (their page URLs) skip beforeSend.
  Sentry.addEventProcessor((event) => {
    if (event.type === 'replay_event') {
      const e = event as Event & { urls?: string[] };
      e.urls = e.urls?.map(scrubUrl);
      scrubEvent(e);
    }
    return event;
  });
  void loadLazy();
}

let lazy: Promise<void> | undefined;

/**
 * Pages opened with a secret in the URL: share links, invitations and the
 * password recovery link (#access_token=…). The replay's page meta (href) is
 * recorded before beforeAddRecordingEvent could scrub it, so these sessions
 * get no replay.
 */
export const tokenPage = (path: string, hash: string) => /^\/(?:p|invitations|reset-password)(?:\/|$)/.test(path) || hash !== '';

/** Adds the replay and feedback integrations (own chunk) once. */
function loadLazy(): Promise<void> {
  lazy ??= import('./sentryLazy').then((m) => {
    if (!tokenPage(location.pathname, location.hash)) Sentry.addIntegration(m.replay());
    Sentry.addIntegration(m.feedback());
  });
  return lazy;
}

/** Sets the signed-in user (ID only) of later events, or clears it. */
export function setSentryUser(id: string | null) {
  if (DSN) Sentry.setUser(id ? { id } : null);
}

/** Reports an error that the app caught (e.g. by a route's error element). */
export function reportError(error: unknown): string | undefined {
  if (!DSN) return undefined;
  if (error instanceof Error && error.name === 'ApiError') return undefined;
  return Sentry.captureException(error);
}

/** Labels of the feedback form, in the current language. */
export interface FeedbackLabels {
  formTitle: string;
  messageLabel: string;
  messagePlaceholder: string;
  submitButtonLabel: string;
  cancelButtonLabel: string;
  confirmButtonLabel: string;
  successMessageText: string;
  isRequiredLabel: string;
  addScreenshotButtonLabel: string;
  removeScreenshotButtonLabel: string;
  highlightToolText: string;
  hideToolText: string;
  removeHighlightText: string;
  errorEmptyMessageText: string;
  errorGenericText: string;
  errorTimeoutText: string;
  errorForbiddenText: string;
  errorNoClientText: string;
}

/**
 * Opens the "report a problem" form. eventId links the report to an error
 * the user just saw; onSent runs once the report is sent.
 */
export async function openFeedback(labels: FeedbackLabels, { eventId, onSent }: { eventId?: string; onSent?: () => void } = {}) {
  if (!DSN) return;
  await loadLazy();
  const feedback = Sentry.getFeedback();
  if (!feedback) return;
  // The app's own theme choice (lib/theme.ts), else the system's.
  const theme = document.documentElement.getAttribute('data-theme');
  feedback.setTheme(theme === 'dark' ? 'dark' : theme === 'pink' || theme === 'light' ? 'light' : 'system');
  const form = await feedback.createForm({
    ...labels,
    tags: eventId ? { error_event_id: eventId } : undefined,
    onFormClose: () => form.removeFromDom(),
    onFormSubmitted: () => {
      form.removeFromDom();
      onSent?.();
    },
  });
  form.appendToDom();
  form.open();
}

export const wrapCreateBrowserRouter = Sentry.wrapCreateBrowserRouter;
export const reactErrorHandler = Sentry.reactErrorHandler;
