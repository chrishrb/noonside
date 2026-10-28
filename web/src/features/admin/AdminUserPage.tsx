import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import {
  deleteUserAvatarMutation, deleteUserMutation, getUserOptions, sendUserPasswordResetMutation, updateUserMutation,
  uploadUserAvatarMutation,
} from '@/api/gen/@tanstack/react-query.gen';
import { ApiError } from '@/api/client';
import { useInvalidate } from '@/api/queries';
import type { AdminUser } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { roleLabel } from '@/auth/scopes';
import { ApiErrorPanel, Content, PageHeader, QueryView } from '@/components/layout/Page';
import { Button, ButtonLink } from '@/components/ui/Button';
import { useConfirm } from '@/components/ui/Confirm';
import { Switch } from '@/components/ui/form';
import { Badge, Card } from '@/components/ui/primitives';
import { toast, toastError } from '@/components/ui/toast';
import { useTick } from '@/components/ui/misc';
import { fmtDate, fmtRel } from '@/lib/format';
import { InviteDialog } from '@/features/members/InviteDialog';
import { ProfileForm } from '@/features/profile/ProfileForm';

/** A superadmin's page for one account: profile, access, systems, password, deletion. */
export function AdminUserPage() {
  const { userId = '' } = useParams();
  const me = useMeData();
  const user = useQuery({ ...getUserOptions({ path: { userId } }), enabled: me.is_superadmin });
  if (!me.is_superadmin) return <ApiErrorPanel error={new ApiError(403, 'superadmin only')} />;
  return <QueryView queries={[user]}>{() => <UserDetail user={user.data!} />}</QueryView>;
}

function UserDetail({ user }: { user: AdminUser }) {
  const { t } = useTranslation();
  useTick();
  const me = useMeData();
  const self = user.id === me.id;
  const name = user.name || user.email;
  const path = { userId: user.id };
  const navigate = useNavigate();
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const update = useMutation(updateUserMutation());
  const upload = useMutation(uploadUserAvatarMutation());
  const removeAvatar = useMutation(deleteUserAvatarMutation());
  const reset = useMutation(sendUserPasswordResetMutation());
  const remove = useMutation(deleteUserMutation());
  const [inviteOpen, setInviteOpen] = useState(false);
  // The own account also shows in the shell (avatar, name).
  const refresh = () => (self ? invalidate('getUser', 'listUsers', 'listMembers', 'getMe') : invalidate('getUser', 'listUsers', 'listMembers'));

  const toggleSuper = async () => {
    const to = !user.is_superadmin;
    const ok = await confirm({
      title: to ? t('admin.users.promote.title', { name }) : t('admin.users.revoke.title', { name }),
      danger: !to, confirmLabel: to ? t('admin.users.promote.label') : t('admin.users.revoke.label'),
      message: `${to ? t('admin.users.promote.message', { name }) : t('admin.users.revoke.message', { name })} ${t('admin.users.takesEffect')}`,
      onConfirm: () => update.mutateAsync({ path, body: { is_superadmin: to } }),
    });
    if (ok) { toast(to ? t('admin.users.promote.done', { name }) : t('admin.users.revoke.done', { name })); await refresh(); }
  };

  const toggleDisabled = async () => {
    const to = !user.disabled;
    const ok = await confirm({
      title: to ? t('admin.user.disable.title', { name }) : t('admin.user.enable.title', { name }),
      danger: to, confirmLabel: to ? t('admin.user.disable.label') : t('admin.user.enable.label'),
      message: to ? t('admin.user.disable.message', { name }) : t('admin.user.enable.message', { name }),
      onConfirm: () => update.mutateAsync({ path, body: { disabled: to } }),
    });
    if (ok) { toast(to ? t('admin.user.disable.done', { name }) : t('admin.user.enable.done', { name })); await refresh(); }
  };

  const del = async () => {
    const ok = await confirm({
      title: t('admin.user.delete.title', { name }), danger: true, confirmLabel: t('admin.user.delete.label'),
      message: t('admin.user.delete.message', { name }), typeToConfirm: user.email,
      onConfirm: () => remove.mutateAsync({ path }),
    });
    if (ok) {
      toast(t('admin.user.delete.done', { name }));
      await invalidate('listUsers', 'listMembers');
      navigate('/admin/users', { replace: true });
    }
  };

  return (
    <Content>
      <PageHeader eyebrow={<Link to="/admin/users">{t('nav.users')}</Link>} title={name}
        sub={<span className="flex flex-wrap items-center gap-2">
          {user.email}
          {self && <Badge tone="outline">{t('common.you')}</Badge>}
          {user.is_superadmin && <Badge tone="accent" icon="shield">{t('common.superadmin')}</Badge>}
          {user.disabled && <Badge tone="danger">{t('admin.user.disabledBadge')}</Badge>}
        </span>} />
      <div className="settings-grid">
        <ProfileForm user={user} emailEditable uploads={me.features.uploads}
          description={t('admin.user.profileHelp')}
          onSave={async (changed) => {
            await update.mutateAsync({ path, body: changed });
            await refresh();
            toast(t('admin.user.saved', { name: changed.name ?? name }));
          }}
          onUpload={async (image) => { await upload.mutateAsync({ path, body: image }); await refresh(); }}
          onRemove={async () => { await removeAvatar.mutateAsync({ path }); await refresh(); }} />

        <Card as="section" aria-labelledby="access-h" className="grid gap-4">
          <div className="grid max-w-[60ch] gap-1">
            <h2 id="access-h">{t('admin.user.access')}</h2>
            <p className="sm muted">
              {t('common.created')} {fmtDate(user.created_at)} · {t('admin.users.lastSignIn')}{' '}
              {user.last_sign_in_at ? fmtRel(user.last_sign_in_at) : t('common.never')}
            </p>
          </div>
          <div className="grid gap-3">
            <Switch label={<span className="grid"><span>{t('common.superadmin')}</span><span className="xs muted">{t('admin.users.superadminHelp')}</span></span>}
              checked={user.is_superadmin} disabled={self} onCheckedChange={toggleSuper} />
            <Switch label={<span className="grid"><span>{t('admin.user.canSignIn')}</span><span className="xs muted">{t('admin.user.canSignInHelp')}</span></span>}
              checked={!user.disabled} disabled={self} onCheckedChange={toggleDisabled} />
            {self && <p className="xs muted">{t('admin.user.selfHint')}</p>}
          </div>
        </Card>

        <Card as="section" aria-labelledby="sys-h" className="grid gap-3">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="grid max-w-[60ch] gap-1"><h2 id="sys-h">{t('switcher.systems')}</h2><p className="sm muted">{t('admin.user.systemsHelp')}</p></div>
            <Button icon="plus" onClick={() => setInviteOpen(true)}>{t('admin.users.addToSystem')}</Button>
          </div>
          {user.systems.length ? (
            <ul className="grid gap-2">
              {user.systems.map((s) => (
                <li key={s.system_id} className="flex flex-wrap items-center justify-between gap-2">
                  <Link className="font-medium" to={`/s/${s.system_id}/settings/members`}>{s.name}</Link>
                  <Badge tone="outline">{roleLabel(s.role)}</Badge>
                </li>
              ))}
            </ul>
          ) : <p className="sm muted">{t('admin.users.noSystems')}</p>}
        </Card>

        <Card as="section" aria-labelledby="pw-h">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="grid max-w-[60ch] gap-1"><h2 id="pw-h">{t('common.password')}</h2><p className="sm muted">{t('admin.user.resetHelp', { name })}</p></div>
            <Button icon="mail" busy={reset.isPending} disabled={user.disabled} onClick={async () => {
              try { await reset.mutateAsync({ path }); toast(t('profile.resetSent', { email: user.email })); } catch (e) { toastError(e); }
            }}>{t('auth.sendResetLink')}</Button>
          </div>
        </Card>

        {!self && (
          <Card as="section" aria-labelledby="del-h">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div className="grid max-w-[60ch] gap-1"><h2 id="del-h">{t('admin.user.delete.heading')}</h2><p className="sm muted">{t('admin.user.delete.help')}</p></div>
              <Button variant="danger" icon="trash" onClick={del}>{t('admin.user.delete.label')}</Button>
            </div>
          </Card>
        )}
      </div>
      <div className="mt-6"><ButtonLink to="/admin/users" icon="left">{t('admin.user.back')}</ButtonLink></div>
      {inviteOpen && <InviteDialog open onOpenChange={setInviteOpen} email={user.email} title={t('admin.users.addToSystem')} />}
    </Content>
  );
}
