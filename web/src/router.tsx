import { useEffect, useRef } from 'react';
import { createBrowserRouter, isRouteErrorResponse, Navigate, useParams, useRouteError } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { AppShell } from '@/components/layout/AppShell';
import { Content, ErrorPanel } from '@/components/layout/Page';
import { persister } from '@/api/persist';
import { queryId } from '@/api/queries';
import { Button, ButtonLink } from '@/components/ui/Button';
import { FullScreenLoading, RequireAuth } from '@/auth/RequireAuth';
import { useFeedback } from '@/lib/feedback';
import { reportError, sentryEnabled, wrapCreateBrowserRouter } from '@/lib/sentry';
import { LoginPage } from '@/features/auth/LoginPage';
import { ForgotPasswordPage } from '@/features/auth/ForgotPasswordPage';
import { ResetPasswordPage } from '@/features/auth/ResetPasswordPage';
import { InvitationPage } from '@/features/auth/InvitationPage';
import { DashboardPage } from '@/features/dashboard/DashboardPage';

// Pages below the dashboard load on demand.
const page = (load: () => Promise<Record<string, React.ComponentType>>, name: string) => async () => ({ Component: (await load())[name] });

function RedirectSystem({ to }: { to: string }) {
  const { sid } = useParams();
  return <Navigate to={`/s/${sid}/${to}`} replace />;
}

function NotFound() {
  const { t } = useTranslation();
  return <ErrorPanel title={t('errors.pageNotFound')} text={t('errors.pageNotFoundText')} action={<ButtonLink to="/">{t('common.goToDashboard')}</ButtonLink>} />;
}

// A render error may come from data restored from the offline cache in a
// shape the page no longer expects (e.g. saved while the API was older).
// Drop it, so a reload fetches fresh data instead of crashing again.
function RouteError() {
  const { t } = useTranslation();
  const error = useRouteError();
  const qc = useQueryClient();
  const openFeedback = useFeedback();
  // The reported error, so a problem report links to it.
  const eventId = useRef<string>(undefined);
  useEffect(() => {
    console.error(error);
    if (!isRouteErrorResponse(error)) eventId.current = reportError(error);
    // Keep /me: the shell needs it, and dropping it would remount the shell.
    qc.removeQueries({ predicate: (q) => queryId(q) !== 'getMe' });
    void persister.removeClient();
  }, [error, qc]);
  return (
    <Content>
      <ErrorPanel title={t('common.somethingWrong')} text={t('errors.crashed')}
        action={<div className="flex flex-wrap gap-2">
          <Button variant="primary" icon="refresh" onClick={() => window.location.reload()}>{t('pwa.reload')}</Button>
          {sentryEnabled && <Button icon="bug" onClick={() => openFeedback(eventId.current)}>{t('feedback.reportError')}</Button>}
        </div>} />
    </Content>
  );
}

function HydrateFallback() {
  const { t } = useTranslation();
  return <FullScreenLoading text={t('common.loading')} />;
}

// Wrapped, so Sentry names transactions after the route patterns.
export const router = wrapCreateBrowserRouter(createBrowserRouter)([
  { path: '/login', element: <LoginPage /> },
  { path: '/forgot-password', element: <ForgotPasswordPage /> },
  { path: '/reset-password', element: <ResetPasswordPage /> },
  // Must match accept_url in internal/api/members.go.
  { path: '/invitations/:token', element: <InvitationPage /> },
  // Must match the share link url in internal/api/share.go.
  { path: '/p/:token', lazy: page(() => import('@/features/public/PublicPage'), 'PublicPage'), hydrateFallbackElement: <HydrateFallback /> },
  {
    element: <RequireAuth />,
    hydrateFallbackElement: <HydrateFallback />,
    errorElement: <RouteError />,
    children: [{
      element: <AppShell />,
      // Pathless, so page errors render inside the shell.
      children: [{ errorElement: <RouteError />, children: [
        { index: true, element: <DashboardPage /> },
        { path: 's/:sid', element: <RedirectSystem to="overview" /> },
        { path: 's/:sid/overview', lazy: page(() => import('@/features/yield/YieldPage'), 'YieldPage') },
        { path: 's/:sid/status', lazy: page(() => import('@/features/status/StatusPage'), 'StatusPage') },
        { path: 's/:sid/export', lazy: page(() => import('@/features/export/ExportPage'), 'ExportPage') },
        { path: 's/:sid/members', element: <RedirectSystem to="settings/members" /> },
        { path: 's/:sid/settings', lazy: page(() => import('@/features/settings/SettingsPage'), 'SettingsPage') },
        { path: 's/:sid/settings/:tab/*', lazy: page(() => import('@/features/settings/SettingsPage'), 'SettingsPage') },
        { path: 'admin/systems', lazy: page(() => import('@/features/admin/AdminSystemsPage'), 'AdminSystemsPage') },
        { path: 'admin/users', lazy: page(() => import('@/features/admin/AdminUsersPage'), 'AdminUsersPage') },
        { path: 'admin/users/:userId', lazy: page(() => import('@/features/admin/AdminUserPage'), 'AdminUserPage') },
        { path: 'notifications', lazy: page(() => import('@/features/notifications/NotificationsPage'), 'NotificationsPage') },
        { path: 'profile', lazy: page(() => import('@/features/profile/ProfilePage'), 'ProfilePage') },
        { path: 'profile/:tab/*', lazy: page(() => import('@/features/profile/ProfilePage'), 'ProfilePage') },
        { path: '*', element: <NotFound /> },
      ] }],
    }],
  },
]);
