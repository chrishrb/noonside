// The heavier Sentry integrations, in their own chunk: loaded right after
// start (lib/sentry.ts), so they don't delay the first render.
import { feedbackIntegration, replayIntegration } from '@sentry/react';
import { scrubRecordingEvent } from './sentry';

export const replay = () => replayIntegration({
  maskAllText: true,
  maskAllInputs: true,
  blockAllMedia: true,
  beforeAddRecordingEvent: scrubRecordingEvent,
});

// The bundled variant, not the lazy one from Sentry's CDN: the CSP allows
// scripts only from 'self'.
export const feedback = () => feedbackIntegration({
  autoInject: false,
  showBranding: false,
  showName: false,
  showEmail: false,
  enableScreenshot: true,
  colorScheme: 'system',
  themeLight: { background: '#ffffff', foreground: '#111111', accentBackground: '#c2410c', accentForeground: '#ffffff', successColor: '#17a34a', errorColor: '#dc2626' },
  themeDark: { background: '#17181b', foreground: '#ececec', accentBackground: '#c2410c', accentForeground: '#ffffff', successColor: '#22c55e', errorColor: '#f05252' },
});
