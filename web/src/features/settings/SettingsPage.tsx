import { useEffect } from 'react';
import { Link, Navigate, useParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { useSystem } from '@/api/queries';
import { ApiError } from '@/api/client';
import { useMeData } from '@/auth/useMe';
import { can, type Scope } from '@/auth/scopes';
import { ApiErrorPanel, Content, PageHeader, QueryView, usePageTitle } from '@/components/layout/Page';
import type { System } from '@/api/types';
import { GeneralTab } from './GeneralTab';
import { MembersTab } from './MembersTab';
import { TargetsTab } from './TargetsTab';
import { InvertersTab } from './InvertersTab';
import { DailyImportTab, DataSourcesTab, PvoutputTab } from './DataSourcesTab';
import { PushKeysTab, ReadKeysTab } from './ApiKeysTab';
import { AlertRulesTab } from './AlertRulesTab';
import { SharingTab } from './SharingTab';
import { WebhooksTab } from './WebhooksTab';

type SectionKey = 'general' | 'targets' | 'inverters' | 'sources' | 'push' | 'import' | 'alerts' | 'pvoutput' | 'webhooks' | 'keys' | 'members' | 'sharing';
type TabKey = 'general' | 'inverters' | 'sources' | 'alerts' | 'integrations' | 'access';
interface Section { key: SectionKey; scope: Scope; Component: (p: { sys: System }) => React.ReactNode }

// Tabs group related sections, shown one below the other. Every section key
// stays a valid URL (/settings/keys, /settings/members …, also used in emails
// and webhooks): it opens its tab and scrolls to the section.
// Data sources hold what brings data in, integrations what sends it out or
// reads it; tabs with `intro` say so above their sections.
const TABS: { key: TabKey; intro?: boolean; sections: Section[] }[] = [
  { key: 'general', sections: [{ key: 'general', scope: 'system:write', Component: GeneralTab }, { key: 'targets', scope: 'system:write', Component: TargetsTab }] },
  { key: 'inverters', sections: [{ key: 'inverters', scope: 'inverter:write', Component: InvertersTab }] },
  {
    key: 'sources', intro: true, sections: [
      { key: 'sources', scope: 'datasource:manage', Component: DataSourcesTab },
      { key: 'push', scope: 'apikey:manage', Component: PushKeysTab },
      { key: 'import', scope: 'data:ingest', Component: DailyImportTab },
    ],
  },
  { key: 'alerts', sections: [{ key: 'alerts', scope: 'alerts:manage', Component: AlertRulesTab }] },
  {
    key: 'integrations', intro: true, sections: [
      { key: 'pvoutput', scope: 'datasource:manage', Component: PvoutputTab },
      { key: 'webhooks', scope: 'webhook:manage', Component: WebhooksTab },
      { key: 'keys', scope: 'apikey:manage', Component: ReadKeysTab },
    ],
  },
  { key: 'access', sections: [{ key: 'members', scope: 'members:read', Component: MembersTab }, { key: 'sharing', scope: 'share:manage', Component: SharingTab }] },
];
const tabLabel = (k: TabKey) => i18n.t(`settings.tabs.${k}`);

export function SettingsPage() {
  const { t } = useTranslation();
  const { sid, tab: key } = useParams();
  const me = useMeData();
  const sys = useSystem(sid!);
  // Only sections the caller may use; a tab without any disappears.
  const tabs = TABS.map((x) => ({ ...x, sections: x.sections.filter((s) => can(me, sid, s.scope)) })).filter((x) => x.sections.length);
  const tab = tabs.find((x) => x.key === key || x.sections.some((s) => s.key === key));

  if (!tabs.length) return <ApiErrorPanel error={new ApiError(403, 'missing scope system:write')} />;
  if (!tab) return <Navigate to={`/s/${sid}/settings/${tabs[0].key}`} replace />;

  return (
    <QueryView queries={[sys]}>
      {() => (
        <Content>
          <PageHeader eyebrow={sys.data!.name} title={t('nav.settings')} />
          {/* After the header, whose title it refines. */}
          <TabTitle label={tabLabel(tab.key)} name={sys.data!.name} />
          <nav className="tabs" aria-label={t('settings.sections')}>
            {tabs.map((x) => <Link key={x.key} to={`/s/${sid}/settings/${x.key}`} aria-current={x === tab ? 'page' : undefined}>{tabLabel(x.key)}</Link>)}
          </nav>
          {tab.intro && <p className="tab-intro sm muted">{t(`settings.intro.${tab.key as 'sources' | 'integrations'}`)}</p>}
          <TabBody key={`${sid}-${tab.key}`} sys={sys.data!} sections={tab.sections} focus={key !== tab.key ? key : undefined} />
        </Content>
      )}
    </QueryView>
  );
}

function TabBody({ sys, sections, focus }: { sys: System; sections: Section[]; focus?: string }) {
  // A section URL (e.g. /settings/keys) scrolls to its section once it has rendered.
  // (TabBody is keyed by tab, so this runs once per tab and section link.)
  const scrollTo = focus && focus !== sections[0].key ? focus : undefined;
  useEffect(() => {
    if (!scrollTo) return;
    const id = setTimeout(() => document.getElementById(`settings-${scrollTo}`)?.scrollIntoView({ block: 'start' }), 300);
    return () => clearTimeout(id);
  }, [scrollTo]);

  const [first, ...rest] = sections;
  // Targets go inside the general tab, above its danger zone.
  if (first.key === 'general') {
    return (
      <GeneralTab sys={sys}>
        {rest.map((s) => <div key={s.key} id={`settings-${s.key}`} className="settings-section"><s.Component sys={sys} /></div>)}
      </GeneralTab>
    );
  }
  return (
    <div className="settings-sections">
      {sections.map((s) => <div key={s.key} id={`settings-${s.key}`} className="settings-section"><s.Component sys={sys} /></div>)}
    </div>
  );
}

// PageHeader sets "Settings"; this refines the document title with the tab.
function TabTitle({ label, name }: { label: string; name: string }) {
  usePageTitle(`${label} · ${name}`);
  return null;
}
