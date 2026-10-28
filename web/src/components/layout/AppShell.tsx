import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Link, NavLink, Outlet, useLocation, useMatch } from 'react-router';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import { useMeData } from '@/auth/useMe';
import { useSystems } from '@/api/queries';
import { Icon, type IconName } from '@/components/ui/Icon';
import { LogoMark } from '@/components/ui/Logo';
import { Avatar } from '@/components/ui/primitives';
import { LAST_SYSTEM, store } from '@/lib/storage';
import { CreateSystemDialog } from '@/features/systems/CreateSystemDialog';
import { SystemSwitcher } from './SystemSwitcher';
import { CreateSystemContext } from './createSystem';
import { systemNavItems } from './navItems';
import { InstallBar } from './InstallBar';
import { NotificationBell } from './NotificationBell';
import { UserMenu } from './UserMenu';
import { useAppBadge, useLocaleSync, useMarkReadFromUrl, useUnreadCount } from '@/features/notifications/hooks';
import { usePushResync } from '@/lib/push';
import { OfflineBar } from './OfflineBar';
import { PullToRefresh } from './PullToRefresh';
import { TabBar } from './TabBar';

function NavItem({ to, icon, children, end }: { to: string; icon: IconName; children: ReactNode; end?: boolean }) {
  return (
    <NavLink to={to} end={end}>
      <Icon name={icon} />
      <span className="flex-1 truncate">{children}</span>
    </NavLink>
  );
}

/**
 * Sidebar navigation from 1024px; below that a top bar with the system
 * switcher and a bottom tab bar, with only the page in between scrolling.
 */
export function AppShell() {
  const { t } = useTranslation();
  const me = useMeData();
  const { data: systems = [] } = useSystems();
  const { pathname } = useLocation();
  const routeSid = useMatch('/s/:sid/*')?.params.sid;
  const [createOpen, setCreateOpen] = useState(false);
  const [title, setTitle] = useState('noonside');
  const mainRef = useRef<HTMLElement>(null);
  const unread = useUnreadCount();
  useAppBadge(unread);
  useLocaleSync(me);
  useMarkReadFromUrl();
  usePushResync();

  // The nav follows the system in the URL, else the last one used.
  if (routeSid) store.set(LAST_SYSTEM, routeSid);
  const last = store.get(LAST_SYSTEM);
  const sys = systems.find((s) => s.id === routeSid) ?? systems.find((s) => s.id === last) ?? systems[0];
  const sid = sys?.id;

  useEffect(() => {
    const on = (e: Event) => setTitle((e as CustomEvent<string>).detail);
    window.addEventListener('noonside:title', on);
    return () => window.removeEventListener('noonside:title', on);
  }, []);

  // New page: scroll up (the window from 1024px, the page area below) and
  // move focus to the heading. Not on the first page: focusing before any
  // interaction makes browsers draw the keyboard focus ring (issue #48), and
  // screen readers start at the top anyway.
  const shownPath = useRef<string | null>(null);
  useEffect(() => {
    window.scrollTo(0, 0);
    mainRef.current?.scrollTo(0, 0);
    const first = shownPath.current === null || shownPath.current === pathname;
    shownPath.current = pathname;
    if (first) return;
    const t = setTimeout(() => mainRef.current?.querySelector<HTMLElement>('[data-page-title]')?.focus({ preventScroll: true }), 0);
    return () => clearTimeout(t);
  }, [pathname]);

  return (
    <CreateSystemContext.Provider value={() => setCreateOpen(true)}>
    <div className="app">
      <a className="skip-link" href="#main-content" onClick={(e) => { e.preventDefault(); mainRef.current?.focus(); }}>{t('nav.skipToContent')}</a>
      <aside className="sidebar" aria-label={t('nav.main')}>
        <div className="brand-row">
          <Link className="brand" to="/"><LogoMark />noonside</Link>
          <NotificationBell unread={unread} />
        </div>
        <nav className="nav" aria-label={t('nav.general')}><NavItem to="/" icon="dashboard" end>{t('nav.dashboard')}</NavItem></nav>
        {sys && (
          <>
            <SystemSwitcher me={me} systems={systems} current={sys} onCreate={() => setCreateOpen(true)} />
            <nav className="nav" aria-label={sys.name}>
              {systemNavItems(me, sys.id).map((it) => <NavItem key={it.to} to={it.to} icon={it.icon}>{t(it.label)}</NavItem>)}
            </nav>
          </>
        )}
        {me.is_superadmin && (
          <nav className="nav" aria-label={t('nav.administration')}>
            <span className="nav-label eyebrow">{t('nav.administration')}</span>
            <NavItem to="/admin/systems" icon="home">{t('nav.allSystems')}</NavItem>
            <NavItem to="/admin/users" icon="user">{t('nav.users')}</NavItem>
          </nav>
        )}
        <div className="sidebar-foot">
          <UserMenu id="sidebar-menu" me={me} side="top" align="start" trigger={
            <button type="button" className={clsx('user-chip', pathname.startsWith('/profile') && 'active')}>
              <Avatar name={me.name || me.email} src={me.avatar_url} />
              <span className="who">
                <span className="sm truncate font-medium">{me.name || me.email}</span>
                <span className="xs muted truncate">{me.is_superadmin ? t('common.superadmin') : me.email}</span>
              </span>
              <Icon name="up" size="sm" className="ms-auto flex-none text-muted" />
            </button>
          } />
        </div>
      </aside>
      <div className="main">
        <header className="topbar">
          {sys
            ? <SystemSwitcher compact me={me} systems={systems} current={sys} onCreate={() => setCreateOpen(true)} />
            : <span className="title">{title}</span>}
          <NotificationBell unread={unread} />
          <UserMenu id="topbar-menu" me={me} trigger={
            <button type="button" className="btn btn-ghost btn-icon" aria-label={t('nav.account')}><Avatar name={me.name || me.email} src={me.avatar_url} /></button>
          } />
        </header>
        <OfflineBar />
        <main id="main-content" ref={mainRef} tabIndex={-1} className="main">
          <PullToRefresh scrollRef={mainRef} />
          <Outlet />
        </main>
      </div>
      <InstallBar />
      <TabBar me={me} sid={sid} />
      {me.is_superadmin && <CreateSystemDialog open={createOpen} onOpenChange={setCreateOpen} />}
    </div>
    </CreateSystemContext.Provider>
  );
}
