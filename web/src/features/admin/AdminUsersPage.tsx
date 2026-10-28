import { useMutation, useQuery } from '@tanstack/react-query';
import { useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router';
import { createUserMutation, listUsersOptions } from '@/api/gen/@tanstack/react-query.gen';
import { ApiError } from '@/api/client';
import { useInvalidate } from '@/api/queries';
import type { AdminUser } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { roleLabel } from '@/auth/scopes';
import { ApiErrorPanel, Content, PageHeader, QueryView } from '@/components/layout/Page';
import { Button, ButtonLink } from '@/components/ui/Button';
import { FormDialog } from '@/components/ui/Dialog';
import { Checkbox, Field, FormGrid, Input } from '@/components/ui/form';
import { Badge, Card, EmptyState, Person } from '@/components/ui/primitives';
import { Actions, ActionsTh, Table } from '@/components/ui/Table';
import { toast } from '@/components/ui/toast';
import { useTick } from '@/components/ui/misc';
import { fmtDate, fmtRel } from '@/lib/format';
import { applyServerError, type ServerError } from '@/lib/forms';
import { newPasswordFields, passwordsMatch } from '@/features/auth/passwords';
import { InviteDialog } from '@/features/members/InviteDialog';

export function AdminUsersPage() {
  const me = useMeData();
  const users = useQuery({ ...listUsersOptions(), enabled: me.is_superadmin });
  if (!me.is_superadmin) return <ApiErrorPanel error={new ApiError(403, 'superadmin only')} />;
  return <QueryView queries={[users]}>{() => <Users users={users.data!} />}</QueryView>;
}

function Users({ users }: { users: AdminUser[] }) {
  const { t } = useTranslation();
  useTick();
  const me = useMeData();
  const [q, setQ] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [inviteEmail, setInviteEmail] = useState<string | null>(null);
  const shown = useMemo(() => {
    const s = q.trim().toLowerCase();
    return users.filter((u) => !s || `${u.name} ${u.email}`.toLowerCase().includes(s));
  }, [users, q]);
  const supers = users.filter((u) => u.is_superadmin).length;

  return (
    <Content>
      <PageHeader eyebrow={t('nav.administration')} title={t('nav.users')} sub={`${t('counts.account', { count: users.length })} · ${t('counts.superadmin', { count: supers })}`}
        actions={<Button variant="primary" icon="plus" onClick={() => setCreateOpen(true)}>{t('admin.users.create')}</Button>} />
      <div className="mb-3 max-w-[360px]">
        <label className="sr-only" htmlFor="u-search">{t('admin.users.search')}</label>
        <Input type="search" id="u-search" placeholder={t('admin.users.searchPlaceholder')} value={q} onChange={(e) => setQ(e.target.value)} />
      </div>
      <Card flush>
        {shown.length ? (
          <Table caption={t('admin.users.all')}>
            <thead><tr><th scope="col">{t('admin.users.user')}</th><th scope="col">{t('admin.users.flag')}</th><th scope="col">{t('switcher.systems')}</th><th scope="col">{t('common.created')}</th><th scope="col">{t('admin.users.lastSignIn')}</th><ActionsTh /></tr></thead>
            <tbody>
              {shown.map((u) => (
                <tr key={u.id}>
                  <td><Link className="person-link" to={`/admin/users/${u.id}`}><Person name={u.name || u.email} email={u.email} you={u.id === me.id} avatar={u.avatar_url} /></Link></td>
                  <td>
                    {u.is_superadmin || u.disabled ? (
                      <span className="flex flex-wrap gap-1.5">
                        {u.is_superadmin && <Badge tone="accent" icon="shield">{t('common.superadmin')}</Badge>}
                        {u.disabled && <Badge tone="danger">{t('admin.user.disabledBadge')}</Badge>}
                      </span>
                    ) : <span className="sm muted">—</span>}
                  </td>
                  <td>
                    {u.systems.length ? (
                      <span className="flex flex-wrap gap-1.5">{u.systems.map((s) => <Badge key={s.system_id} tone="outline">{s.name} · {roleLabel(s.role)}</Badge>)}</span>
                    ) : <span className="sm muted">{t('admin.users.noSystems')}</span>}
                  </td>
                  <td className="sm muted whitespace-nowrap">{fmtDate(u.created_at)}</td>
                  <td className="sm whitespace-nowrap">{u.last_sign_in_at ? fmtRel(u.last_sign_in_at) : <span className="muted">{t('common.never')}</span>}</td>
                  <td>
                    <Actions>
                      <Button variant="ghost" size="sm" onClick={() => setInviteEmail(u.email)}>{t('admin.users.addToSystem')}</Button>
                      <ButtonLink variant="ghost" size="sm" to={`/admin/users/${u.id}`}>{t('admin.users.edit')}</ButtonLink>
                    </Actions>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : <EmptyState icon="search" title={t('admin.users.noMatch')} text={t('admin.users.noMatchText')} />}
      </Card>
      <CreateUserDialog open={createOpen} onOpenChange={setCreateOpen} />
      {inviteEmail && <InviteDialog key={inviteEmail} open onOpenChange={(o) => !o && setInviteEmail(null)} email={inviteEmail} title={t('admin.users.addToSystem')} />}
    </Content>
  );
}

const schema = z.object({
  email: z.string().trim().min(1, 'validation.emailRequired').email('validation.emailInvalid'),
  name: z.string().trim().min(1, 'validation.nameRequired').max(200),
  superadmin: z.boolean(),
  ...newPasswordFields,
}).refine(...passwordsMatch);
type Values = z.infer<typeof schema>;

function CreateUserDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation();
  const create = useMutation(createUserMutation());
  const invalidate = useInvalidate();
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const defaults = { email: '', name: '', password: '', password2: '', superadmin: false };
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: defaults });
  const close = (o: boolean) => { onOpenChange(o); if (!o) { form.reset(defaults); setServerError(null); } };
  const e = form.formState.errors;
  return (
    <FormDialog open={open} onOpenChange={close} title={t('admin.users.create')} submitLabel={t('admin.users.create')} busy={create.isPending} serverError={serverError}
      subtitle={t('admin.users.createSubtitle')}
      onSubmit={form.handleSubmit(async (v) => {
        setServerError(null);
        try {
          const u = await create.mutateAsync({ body: { email: v.email, name: v.name, password: v.password, superadmin: v.superadmin } });
          await invalidate('listUsers');
          toast(t('system.created', { name: u.name }));
          close(false);
        } catch (err) {
          setServerError(applyServerError(err, form.setError, { email: 'email', 'a user with': 'email', name: 'name', password: 'password' }));
        }
      })}>
      <FormGrid>
        <Field label={t('common.email')} required span2 error={e.email?.message}><Input type="email" autoComplete="off" autoFocus {...form.register('email')} /></Field>
        <Field label={t('common.name')} required span2 error={e.name?.message}><Input maxLength={200} {...form.register('name')} /></Field>
        <Field label={t('admin.users.initialPassword')} required help={t('admin.users.initialPasswordHelp')} error={e.password?.message}>
          <Input type="password" autoComplete="new-password" {...form.register('password')} />
        </Field>
        <Field label={t('auth.repeatPassword')} required error={e.password2?.message}><Input type="password" autoComplete="new-password" {...form.register('password2')} /></Field>
        <div className="span-2"><Checkbox label={t('common.superadmin')} help={t('admin.users.superadminHelp')} {...form.register('superadmin')} /></div>
      </FormGrid>
    </FormDialog>
  );
}
