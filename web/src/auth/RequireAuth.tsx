import { useEffect } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router';
import { ApiError } from '@/api/client';
import { ApiErrorPanel } from '@/components/layout/Page';
import { LogoMark } from '@/components/ui/Logo';
import { Spinner } from '@/components/ui/primitives';
import { useTranslation } from 'react-i18next';
import { useAuth } from './AuthProvider';
import { useMe } from './useMe';

/** The splash from index.html, with a spinner and what is loading. */
export function FullScreenLoading({ text }: { text: string }) {
  return (
    <div className="splash" role="status">
      <LogoMark size={72} />
      noonside
      <span className="splash-text"><Spinner />{text}</span>
    </div>
  );
}

/** Signed-in area: waits for the session and /me, else sends to /login. */
export function RequireAuth() {
  const { t } = useTranslation();
  const { session, ready, signOut } = useAuth();
  const location = useLocation();
  const me = useMe();
  const unauthorized = me.error instanceof ApiError && me.error.status === 401;

  useEffect(() => { if (unauthorized) void signOut(); }, [unauthorized, signOut]);

  if (!ready) return <FullScreenLoading text={t('common.loading')} />;
  if (!session || unauthorized) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  if (me.isError) return <ApiErrorPanel error={me.error} retry={() => me.refetch()} />;
  if (!me.data) return <FullScreenLoading text={t('auth.loadingSystems')} />;
  return <Outlet />;
}
