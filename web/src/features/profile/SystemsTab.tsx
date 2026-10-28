import { useMutation } from '@tanstack/react-query';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { removeMemberMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { SystemMembership } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { Button } from '@/components/ui/Button';
import { useConfirm } from '@/components/ui/Confirm';
import { Icon } from '@/components/ui/Icon';
import { Badge, Card, CardHead, EmptyState, RoleBadge } from '@/components/ui/primitives';
import { Actions, ActionsTh, Table } from '@/components/ui/Table';
import { toast } from '@/components/ui/toast';

/** The systems the user is a member of, with their role; leaving one. */
export function SystemsTab() {
  const { t } = useTranslation();
  const me = useMeData();
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const leave = useMutation(removeMemberMutation());
  const leaveSystem = async (m: SystemMembership) => {
    const ok = await confirm({ title: t('members.leave.title', { system: m.name }), danger: true, confirmLabel: t('members.leave.label'),
      message: t('profile.leaveMessage'),
      onConfirm: () => leave.mutateAsync({ path: { systemId: m.system_id, userId: me.id } }) });
    if (ok) { toast(t('members.leave.done', { system: m.name })); await invalidate('getMe', 'listSystems', 'getDashboard'); }
  };

  return (
    <div className="settings-grid">
      <Card flush as="section" aria-labelledby="ms-h">
        <CardHead title={t('profile.memberships')} id="ms-h"><span className="sm muted">{t('counts.system', { count: me.systems.length })}</span></CardHead>
        {me.systems.length ? (<>
          {/* Phones get a list: the permission badges would squeeze a table. */}
          <ul className="member-list sm:hidden">
            {me.systems.map((m) => (
              <li key={m.system_id}>
                <div className="member-list-head">
                  <div className="grid min-w-0 justify-items-start gap-1">
                    <Link to={`/s/${m.system_id}/overview`} className="font-medium break-words">{m.name}</Link>
                    <RoleBadge role={m.role} />
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => leaveSystem(m)}>{t('members.leaveShort')}</Button>
                </div>
                <details className="scope-details">
                  <summary><Icon name="down" size="sm" />{t('counts.permission', { count: m.scopes.length })}</summary>
                  <span className="flex flex-wrap gap-1">{m.scopes.map((s) => <Badge key={s} tone="outline">{s}</Badge>)}</span>
                </details>
              </li>
            ))}
          </ul>
          <div className="max-sm:hidden">
            <Table>
              <thead><tr><th scope="col">{t('common.system')}</th><th scope="col">{t('common.role')}</th><th scope="col">{t('members.permissions')}</th><ActionsTh /></tr></thead>
              <tbody>
                {me.systems.map((m) => (
                  <tr key={m.system_id}>
                    <td><Link to={`/s/${m.system_id}/overview`} className="font-medium">{m.name}</Link></td>
                    <td><RoleBadge role={m.role} /></td>
                    <td><span className="flex flex-wrap gap-1">{m.scopes.map((s) => <Badge key={s} tone="outline">{s}</Badge>)}</span></td>
                    <td><Actions><Button variant="ghost" size="sm" onClick={() => leaveSystem(m)}>{t('members.leaveShort')}</Button></Actions></td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </div>
        </>
        ) : (
          <EmptyState icon="users" title={t('profile.noMemberships')} text={me.is_superadmin ? t('profile.noMembershipsSuper') : t('profile.noMembershipsText')} />
        )}
      </Card>
    </div>
  );
}
