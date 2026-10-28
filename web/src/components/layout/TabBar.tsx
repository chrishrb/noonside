import { NavLink } from 'react-router';
import { useTranslation } from 'react-i18next';
import type { Me } from '@/api/types';
import { Icon } from '@/components/ui/Icon';
import { systemNavItems } from './navItems';

/** Bottom navigation for phones and tablets (hidden from 1024px, where the sidebar shows). */
export function TabBar({ me, sid }: { me: Me; sid?: string }) {
  const { t } = useTranslation();
  return (
    <nav className="tabbar" aria-label={t('nav.tabs')}>
      <NavLink to="/" end>
        <Icon name="dashboard" />
        <span>{t('nav.dashboard')}</span>
      </NavLink>
      {sid && systemNavItems(me, sid).map((it) => (
        <NavLink key={it.to} to={it.to}>
          <Icon name={it.icon} />
          <span>{t(it.short ?? it.label)}</span>
        </NavLink>
      ))}
    </nav>
  );
}
