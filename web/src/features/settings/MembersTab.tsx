import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { listInvitationsOptions, listMembersOptions, removeMemberMutation, revokeInvitationMutation, updateMemberMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { Member, Role, System } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { ROLES, ROLE_SCOPES, actorRole, assignableRoles, can, roleHelp, roleLabel } from '@/auth/scopes';
import { SectionHead } from '@/components/layout/Page';
import { Button } from '@/components/ui/Button';
import { useConfirm } from '@/components/ui/Confirm';
import { Icon } from '@/components/ui/Icon';
import { Disclosure } from '@/components/ui/misc';
import { Banner, Card, EmptyState, Person, RoleBadge, Skeleton } from '@/components/ui/primitives';
import { Actions, ActionsTh, Table } from '@/components/ui/Table';
import { toast, toastError } from '@/components/ui/toast';
import { fmtDate } from '@/lib/format';
import { InviteDialog } from '@/features/members/InviteDialog';

export function MembersTab({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const sid = sys.id;
  const me = useMeData();
  const p = { path: { systemId: sid } };
  const members = useQuery(listMembersOptions(p));
  const invites = useQuery(listInvitationsOptions(p));
  const [inviteOpen, setInviteOpen] = useState(false);
  const canInvite = can(me, sid, 'members:invite');

  if (members.isError) return <Banner tone="danger" title={t('members.loadFailed')}>{members.error.message}</Banner>;
  if (!members.data) return <Skeleton style={{ height: 240 }} />;

  const owners = members.data.filter((m) => m.role === 'owner').length;
  return (
    <>
      <SectionHead title={t('members.title')} sub={t('members.sub', { members: t('counts.member', { count: members.data.length }), owners: t('counts.owner', { count: owners }) })}>
        {canInvite && <Button variant="primary" icon="plus" onClick={() => setInviteOpen(true)}>{t('members.invite')}</Button>}
      </SectionHead>
      <Card flush>
        <Table caption={t('members.title')}>
          <thead><tr><th scope="col">{t('members.person')}</th><th scope="col">{t('common.role')}</th><th scope="col">{t('members.joined')}</th><ActionsTh /></tr></thead>
          <tbody>{members.data.map((m) => <MemberRow key={m.user_id} m={m} sys={sys} />)}</tbody>
        </Table>
        <Disclosure summary={t('members.roleHelp')}>
          <Table>
            <thead><tr><th scope="col">{t('common.role')}</th><th scope="col">{t('members.permissions')}</th></tr></thead>
            <tbody>
              {ROLES.map((r) => (
                <tr key={r}>
                  <td><RoleBadge role={r} /></td>
                  <td className="sm">{roleHelp(r)}<span className="xs muted mt-0.5 block">{ROLE_SCOPES[r].join(' · ')}</span></td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Disclosure>
      </Card>
      <section className="section" aria-labelledby="pi-h">
        <SectionHead title={t('members.pending')} id="pi-h" />
        <Card flush>
          {invites.data?.length ? (
            <Table caption={t('members.pending')}>
              <thead><tr><th scope="col">{t('common.email')}</th><th scope="col">{t('common.role')}</th><th scope="col">{t('members.sent')}</th><th scope="col">{t('members.expires')}</th><ActionsTh /></tr></thead>
              <tbody>{invites.data.map((i) => <InvitationRow key={i.id} sys={sys} inv={i} />)}</tbody>
            </Table>
          ) : (
            <EmptyState icon="mail" title={t('members.noPending')} text={canInvite ? t('members.noPendingInvite') : t('members.noPendingView')} />
          )}
        </Card>
      </section>
      {canInvite && <InviteDialog open={inviteOpen} onOpenChange={setInviteOpen} systemId={sid} />}
    </>
  );
}

function MemberRow({ m, sys }: { m: Member; sys: System }) {
  const { t } = useTranslation();
  const sid = sys.id;
  const me = useMeData();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const update = useMutation(updateMemberMutation());
  const remove = useMutation(removeMemberMutation());
  const actor = actorRole(me, sid);
  const self = m.user_id === me.id;
  const editable = can(me, sid, 'members:manage') && !(m.role === 'owner' && actor !== 'owner');
  const params = { path: { systemId: sid, userId: m.user_id } };

  const changeRole = async (role: Role) => {
    if (self && ROLES.indexOf(role) > ROLES.indexOf(m.role)) {
      const ok = await confirm({ title: t('members.ownRole.title'), confirmLabel: t('members.ownRole.label'),
        message: t('members.ownRole.message', { role: roleLabel(role), system: sys.name }) });
      if (!ok) return;
    }
    try {
      await update.mutateAsync({ ...params, body: { role } });
      toast(t('members.roleChanged', { role: roleLabel(role) }));
      await invalidate('listMembers', ...(self ? (['getMe', 'listSystems'] as const) : []));
    } catch (e) { toastError(e); }
  };

  const onRemove = async () => {
    const ok = await confirm({
      title: self ? t('members.leave.title', { system: sys.name }) : t('members.remove.title', { name: m.name }), danger: true,
      confirmLabel: self ? t('members.leave.label') : t('members.remove.label'),
      message: self ? t('members.leave.message') : t('members.remove.message', { name: m.name, system: sys.name }),
      onConfirm: () => remove.mutateAsync(params),
    });
    if (!ok) return;
    toast(self ? t('members.leave.done', { system: sys.name }) : t('members.remove.done', { name: m.name }));
    if (self) { await invalidate('getMe', 'listSystems', 'getDashboard'); navigate('/'); } else await invalidate('listMembers');
  };

  const opts = assignableRoles(actor);
  if (!opts.includes(m.role)) opts.unshift(m.role);
  return (
    <tr>
      <td><Person name={m.name || m.email} email={m.email} you={self} avatar={m.avatar_url} /></td>
      <td>
        {editable ? (
          <>
            <label className="sr-only" htmlFor={`role-${m.user_id}`}>{t('members.roleFor', { name: m.name })}</label>
            <select id={`role-${m.user_id}`} className="select select-sm role-select" value={m.role} disabled={update.isPending}
              onChange={(e) => changeRole(e.target.value as Role)}>
              {opts.map((r) => <option key={r} value={r}>{roleLabel(r)}</option>)}
            </select>
          </>
        ) : <RoleBadge role={m.role} />}
      </td>
      <td className="sm muted whitespace-nowrap">{fmtDate(m.created_at)}</td>
      <td>
        <Actions>
          {self ? <Button variant="ghost" size="sm" onClick={onRemove}>{t('members.leaveShort')}</Button>
            : editable && <Button variant="danger-ghost" size="sm" onClick={onRemove} aria-label={t('members.remove.aria', { name: m.name })}>{t('common.remove')}</Button>}
        </Actions>
      </td>
    </tr>
  );
}

function InvitationRow({ sys, inv }: { sys: System; inv: { id: string; email: string; role: Role; created_at: string; expires_at: string } }) {
  const { t } = useTranslation();
  const me = useMeData();
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const revoke = useMutation(revokeInvitationMutation());
  const canRevoke = can(me, sys.id, 'members:invite') && (inv.role !== 'owner' || actorRole(me, sys.id) === 'owner');
  return (
    <tr>
      <td><span className="flex items-center gap-2"><Icon name="mail" size="sm" /><span className="sm">{inv.email}</span></span></td>
      <td><RoleBadge role={inv.role} /></td>
      <td className="sm muted whitespace-nowrap">{fmtDate(inv.created_at)}</td>
      <td className="sm muted whitespace-nowrap">{fmtDate(inv.expires_at)}</td>
      <td>
        <Actions>
          {canRevoke && (
            <Button variant="danger-ghost" size="sm" onClick={async () => {
              const ok = await confirm({ title: t('members.revoke.title'), danger: true, confirmLabel: t('members.revoke.label'), message: t('members.revoke.message', { email: inv.email }),
                onConfirm: () => revoke.mutateAsync({ path: { systemId: sys.id, invitationId: inv.id } }) });
              if (ok) { toast(t('members.revoke.done')); await invalidate('listInvitations'); }
            }}>{t('members.revoke.label')}</Button>
          )}
        </Actions>
      </td>
    </tr>
  );
}
