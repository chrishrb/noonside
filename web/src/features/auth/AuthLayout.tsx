import type { ReactNode } from 'react';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';

import { LogoMark } from '@/components/ui/Logo';
import { usePageTitle } from '@/components/layout/Page';

export const Brand = ({ to = '/login' }: { to?: string }) => (
  <Link className="brand" to={to} style={{ padding: 0 }}><LogoMark />noonside</Link>
);

/** Two-column public screen: form on the left, dark product panel on the right. */
export function AuthLayout({ title, children, side }: { title: string; children: ReactNode; side?: ReactNode }) {
  const { t } = useTranslation();
  usePageTitle(title);
  return (
    <div className="auth">
      <main className="auth-main" id="main-content">
        <div className="auth-panel">
          <Brand />
          {children}
        </div>
      </main>
      <aside className="auth-side" aria-label={t('auth.side.about')}>
        {side ?? (
          <>
            <span className="brand"><LogoMark />noonside</span>
            <p className="quote">{t('auth.side.quote')}</p>
            <div className="facts">
              <div className="stat"><span className="v">{t('auth.side.curves')}</span><span className="l">{t('auth.side.curvesText')}</span></div>
              <div className="stat"><span className="v">{t('auth.side.roles')}</span><span className="l">{t('auth.side.rolesText')}</span></div>
              <div className="stat"><span className="v">{t('auth.side.import')}</span><span className="l">{t('auth.side.importText')}</span></div>
              <div className="stat"><span className="v">{t('auth.side.push')}</span><span className="l">{t('auth.side.pushText')}</span></div>
            </div>
            <p className="muted-inv">{t('auth.side.invitationOnly')}</p>
          </>
        )}
      </aside>
    </div>
  );
}

export const AuthTitles = ({ eyebrow, title, children }: { eyebrow?: string; title: string; children?: ReactNode }) => (
  <div className="titles">
    {eyebrow && <span className="eyebrow">{eyebrow}</span>}
    <h1>{title}</h1>
    {children && <p className="muted sm">{children}</p>}
  </div>
);
