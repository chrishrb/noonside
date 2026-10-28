import { Link, Navigate, useLocation, useParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { useMeData } from '@/auth/useMe';
import { Content, PageHeader, usePageTitle } from '@/components/layout/Page';
import { NotificationSettings } from '@/features/notifications/NotificationSettings';
import { AccountTab } from './AccountTab';
import { AppearanceTab } from './AppearanceTab';
import { SystemsTab } from './SystemsTab';

type TabKey = 'account' | 'notifications' | 'appearance' | 'systems';
const TABS: { key: TabKey; Component: () => React.ReactNode }[] = [
  { key: 'account', Component: AccountTab },
  { key: 'notifications', Component: () => <div className="settings-grid"><NotificationSettings /></div> },
  { key: 'appearance', Component: AppearanceTab },
  { key: 'systems', Component: SystemsTab },
];
const tabLabel = (k: TabKey) => i18n.t(`profile.tabs.${k}`);

export function ProfilePage() {
  const { t } = useTranslation();
  const { tab: tabKey } = useParams();
  const { hash } = useLocation();
  const me = useMeData();
  const tab = TABS.find((x) => x.key === tabKey);
  // Older emails link to /profile#notifications.
  if (!tab) return <Navigate to={`/profile/${hash === '#notifications' ? 'notifications' : 'account'}`} replace />;

  return (
    <Content>
      <TabTitle label={tabLabel(tab.key)} />
      <PageHeader eyebrow={me.name || me.email} title={t('profile.title')} />
      <nav className="tabs" aria-label={t('profile.sections')}>
        {TABS.map((x) => <Link key={x.key} to={`/profile/${x.key}`} aria-current={x.key === tab.key ? 'page' : undefined}>{tabLabel(x.key)}</Link>)}
      </nav>
      <tab.Component key={tab.key} />
    </Content>
  );
}

// PageHeader sets "Profile"; this refines the document title with the tab.
function TabTitle({ label }: { label: string }) {
  const { t } = useTranslation();
  usePageTitle(`${label} · ${t('profile.title')}`);
  return null;
}
