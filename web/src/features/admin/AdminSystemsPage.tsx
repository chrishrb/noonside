import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { listAdminSystemsOptions } from '@/api/gen/@tanstack/react-query.gen';
import { ApiError } from '@/api/client';
import { useMeData } from '@/auth/useMe';
import { membership } from '@/auth/scopes';
import { useCreateSystem } from '@/components/layout/createSystem';
import { ApiErrorPanel, Content, PageHeader, QueryView } from '@/components/layout/Page';
import { Button, ButtonLink } from '@/components/ui/Button';
import { Badge, Card, EmptyState, RoleBadge } from '@/components/ui/primitives';
import { Actions, ActionsTh, Table } from '@/components/ui/Table';
import { fmtDate, fmtKwp } from '@/lib/format';

export function AdminSystemsPage() {
  const { t } = useTranslation();
  const me = useMeData();
  // One request with the counts and owners: per-system member and inverter
  // queries overwhelm the browser with hundreds of systems. Refetched on
  // every visit, as edits elsewhere (members, inverters) don't invalidate it.
  const systems = useQuery({ ...listAdminSystemsOptions(), enabled: me.is_superadmin, refetchOnMount: 'always' });
  const create = useCreateSystem();
  if (!me.is_superadmin) return <ApiErrorPanel error={new ApiError(403, 'superadmin only')} />;

  return (
    <QueryView queries={[systems]}>
      {() => (
        <Content>
          <PageHeader eyebrow={t('nav.administration')} title={t('nav.allSystems')} sub={t('admin.systems.intro')}
            actions={<Button variant="primary" icon="plus" onClick={create}>{t('dashboard.newSystem')}</Button>} />
          <Card flush>
            {systems.data!.length ? (
              <Table caption={t('nav.allSystems')}>
                <thead>
                  <tr>
                    <th scope="col">{t('common.system')}</th><th scope="col" className="num">{t('yield.inverters.peak')}</th><th scope="col" className="num">{t('inverters.title')}</th><th scope="col" className="num">{t('members.title')}</th>
                    <th scope="col">{t('admin.systems.owners')}</th><th scope="col">{t('admin.systems.access')}</th><th scope="col">{t('common.created')}</th><ActionsTh />
                  </tr>
                </thead>
                <tbody>
                  {systems.data!.map(({ system: s, inverter_count, member_count, owners }) => {
                    const mine = membership(me, s.id);
                    return (
                      <tr key={s.id}>
                        <td><span className="cell-main"><Link to={`/s/${s.id}/overview`} className="font-medium">{s.name}</Link><span className="xs muted">{s.location || t('common.noLocation')}</span></span></td>
                        <td className="num">{fmtKwp(s.peak_power_kwp)}</td>
                        <td className="num">{inverter_count}</td>
                        <td className="num"><Link to={`/s/${s.id}/settings/members`} aria-label={t('admin.systems.membersOf', { count: member_count, system: s.name })}>{member_count}</Link></td>
                        <td className="sm">{owners.map((x) => x.name || x.email).join(', ') || '—'}</td>
                        <td>{mine ? <RoleBadge role={mine.role} /> : <Badge tone="outline">{t('admin.systems.superAccess')}</Badge>}</td>
                        <td className="sm muted whitespace-nowrap">{fmtDate(s.created_at, s.timezone)}</td>
                        <td><Actions><ButtonLink variant="ghost" size="sm" to={`/s/${s.id}/settings/general`}>{t('nav.settings')}</ButtonLink></Actions></td>
                      </tr>
                    );
                  })}
                </tbody>
              </Table>
            ) : <EmptyState icon="home" title={t('dashboard.noSystems.title')} text={t('admin.systems.emptyText')} />}
          </Card>
        </Content>
      )}
    </QueryView>
  );
}
