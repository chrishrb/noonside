import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { createInvitationMutation } from '@/api/gen/@tanstack/react-query.gen';
import { zRole } from '@/api/gen/zod.gen';
import { useInvalidate, useSystems } from '@/api/queries';
import type { CreatedInvitation, Role } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { actorRole, assignableRoles, can, roleHelp, roleLabel } from '@/auth/scopes';
import { FormDialog } from '@/components/ui/Dialog';
import { Field, Input, Select } from '@/components/ui/form';
import { SecretReveal } from '@/components/ui/misc';
import { fmtDateTime } from '@/lib/format';
import { applyServerError, type ServerError } from '@/lib/forms';

const schema = z.object({
  system: z.string().min(1, 'validation.systemRequired'),
  email: z.string().trim().min(1, 'validation.emailRequired').email('validation.emailInvalid'),
  role: zRole,
});
type Values = z.infer<typeof schema>;

interface Props {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  /** Fixed system; otherwise the user picks one they may invite to. */
  systemId?: string;
  /** Fixed email (admin "Add to system"). */
  email?: string;
  title?: string;
}

/** Invite someone by email. Superadmins get the accept link if it couldn't be emailed. */
export function InviteDialog({ open, onOpenChange, systemId, email, title }: Props) {
  const { t } = useTranslation();
  const me = useMeData();
  const { data: systems = [] } = useSystems();
  const invalidate = useInvalidate();
  const create = useMutation(createInvitationMutation());
  const [result, setResult] = useState<{ inv: CreatedInvitation; systemName: string } | null>(null);
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const allowed = systems.filter((s) => can(me, s.id, 'members:invite'));
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { system: systemId ?? allowed[0]?.id ?? '', email: email ?? '', role: 'viewer' },
  });
  const sid = useWatch({ control: form.control, name: 'system' });
  const roles = assignableRoles(actorRole(me, sid));

  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) {
      setResult(null); setServerError(null);
      form.reset({ system: systemId ?? allowed[0]?.id ?? '', email: email ?? '', role: 'viewer' });
    }
  };

  if (result) {
    const { inv, systemName } = result;
    return (
      <FormDialog open={open} onOpenChange={close} hideCancel submitLabel={t('common.done')}
        title={inv.email_sent ? t('invite.sent') : t('invite.share')}
        subtitle={inv.email_sent
          ? t('invite.sentText', { email: inv.invitation.email, system: systemName, role: roleLabel(inv.invitation.role) })
          : t('invite.noEmail')}>
        {/* The server returns the link only when it couldn't email it, and only to superadmins. */}
        {inv.accept_url && inv.token && (
          <>
            <SecretReveal label={t('invite.link')} value={inv.accept_url} help={t('invite.linkHelp', { until: fmtDateTime(inv.invitation.expires_at) })} />
            <p className="sm">
              <Link to={`/invitations/${encodeURIComponent(inv.token)}`} target="_blank">{t('invite.open')}</Link>{' '}
              <span className="muted">{t('invite.openSuffix')}</span>
            </p>
          </>
        )}
      </FormDialog>
    );
  }

  const e = form.formState.errors;
  return (
    <FormDialog open={open} onOpenChange={close} title={title ?? t('members.invite')} submitLabel={t('invite.submit')} busy={create.isPending} serverError={serverError}
      subtitle={t('invite.subtitle')}
      onSubmit={form.handleSubmit(async (v) => {
        setServerError(null);
        try {
          const inv = await create.mutateAsync({ path: { systemId: v.system }, body: { email: v.email, role: v.role } });
          await invalidate('listInvitations', 'listUsers');
          setResult({ inv, systemName: systems.find((s) => s.id === v.system)?.name ?? t('invite.theSystem') });
        } catch (err) {
          setServerError(applyServerError(err, form.setError, { email: 'email', 'a user with': 'email', role: 'role', 'only owners': 'role' }));
        }
      })}>
      {!systemId && (
        <Field label={t('common.system')} required error={e.system?.message}>
          <Select options={allowed.map((s) => ({ value: s.id, label: s.name }))} {...form.register('system', {
            onChange: () => { if (!assignableRoles(actorRole(me, form.getValues('system'))).includes(form.getValues('role'))) form.setValue('role', 'viewer'); },
          })} />
        </Field>
      )}
      <Field label={t('common.email')} required error={e.email?.message}>
        <Input type="email" autoComplete="off" readOnly={!!email} autoFocus={!email} {...form.register('email')} />
      </Field>
      <Field label={t('common.role')} help={t('invite.roleHelp')} error={e.role?.message}>
        <Select options={roles.map((r: Role) => ({ value: r, label: `${roleLabel(r)} — ${roleHelp(r)}` }))} {...form.register('role')} />
      </Field>
    </FormDialog>
  );
}
