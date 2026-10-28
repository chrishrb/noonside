import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { RouterProvider } from 'react-router';
import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query';
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client';
import { Toaster } from 'sonner';
import '@fontsource-variable/inter';
import './styles/app.css';
import '@/i18n';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import { PERSIST_MAX_AGE, persistOptions } from '@/api/persist';
import { AuthProvider } from '@/auth/AuthProvider';
import { ConfirmProvider } from '@/components/ui/Confirm';
import '@/lib/install';
import { initSentry, reactErrorHandler, reportError, sentryEnabled } from '@/lib/sentry';
import { applyTheme } from '@/lib/theme';
import { registerServiceWorker } from './pwa';
import { router } from './router';

initSentry();
applyTheme();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      // Kept as long as the persisted cache, so it can be restored offline.
      gcTime: PERSIST_MAX_AGE,
      refetchOnWindowFocus: true,
      // Client errors (403/404/…) won't change on retry.
      retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
    },
  },
  // Bugs in query functions and selects; API errors are not reported here.
  queryCache: new QueryCache({ onError: reportError }),
  mutationCache: new MutationCache({ onError: reportError }),
});

// Remounting the routes on a language switch re-renders every page, including
// labels computed outside components (periods, data source kinds, …).
function Routes() {
  const { i18n } = useTranslation();
  return <RouterProvider key={i18n.resolvedLanguage} router={router} />;
}

createRoot(document.getElementById('root')!, sentryEnabled ? {
  onUncaughtError: reactErrorHandler(),
  onCaughtError: reactErrorHandler(),
  onRecoverableError: reactErrorHandler(),
} : {}).render(
  <StrictMode>
    <PersistQueryClientProvider client={queryClient} persistOptions={persistOptions}>
      <AuthProvider>
        <ConfirmProvider>
          <Routes />
          <Toaster
            position="bottom-right"
            offset={{ bottom: 'var(--toast-bottom)', right: 'var(--toast-side)' }}
            mobileOffset={{ bottom: 'var(--toast-bottom)', right: 'var(--toast-side)', left: 'var(--toast-side)' }}
            toastOptions={{ unstyled: true }}
          />
        </ConfirmProvider>
      </AuthProvider>
    </PersistQueryClientProvider>
  </StrictMode>,
);

// The first render is on screen (the same splash while loading, or a page):
// fade out the static splash from index.html.
requestAnimationFrame(() => {
  const boot = document.getElementById('boot');
  if (!boot) return;
  boot.classList.add('gone');
  setTimeout(() => boot.remove(), 250);
});

registerServiceWorker();
