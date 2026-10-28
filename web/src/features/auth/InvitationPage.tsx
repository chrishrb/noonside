import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Link, useNavigate, useParams } from 'react-router';
import { Trans, useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { acceptInvitationMutation, getInvitationOptions } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { AcceptInvitationResult, InvitationInfo } from '@/api/types';
import { useAuth } from '@/auth/AuthProvider';
import { useMe } from '@/auth/useMe';
import { FullScreenLoading } from '@/auth/RequireAuth';
import { roleLabel } from '@/auth/scopes';
import { Button, ButtonLink } from '@/components/ui/Button';
import { Field, Input } from '@/components/ui/form';
import { Banner } from '@/components/ui/primitives';
import { toast } from '@/components/ui/toast';
import { fmtDateTime } from '@/lib/format';
import { applyServerError, serverErrorOf, type ServerError } from '@/lib/forms';
import { AuthLayout, AuthTitles } from './AuthLayout';
import { newPasswordFields, passwordsMatch } from './passwords';

function Side() {
  const { t } = useTranslation();
  return (
    <>
      <span />
      <p className="quote">{t('auth.invitation.sideQuote')}</p>
      <p className="muted-inv">{t('auth.invitation.sideExpiry')}</p>
    </>
  );
}
const side = <Side />;

/** Public accept page for `accept_url` links from invitation emails. */
export function InvitationPage() {
  const { t } = useTranslation();
  const token = useParams().token!;
  const { session, ready } = useAuth();
  const info = useQuery({ ...getInvitationOptions({ path: { token } }), retry: false });
  const me = useMe();

  if (!ready || info.isPending || (session && me.isPending)) return <FullScreenLoading text={t('auth.invitation.loading')} />;
  if (info.isError) {
    return (
      <AuthLayout title={t('auth.invitation.eyebrow')} side={side}>
        <AuthTitles title={t('auth.invitation.invalidTitle')}>{t('auth.invitation.invalidText')}</AuthTitles>
        <div><ButtonLink to="/login">{t('auth.invitation.goToSignIn')}</ButtonLink></div>
      </AuthLayout>
    );
  }
  const inv = info.data;
  return (
    <AuthLayout title={t('auth.invitation.eyebrow')} side={side}>
      <div className="titles">
        <span className="eyebrow">{t('auth.invitation.eyebrow')}</span>
        <h1>{t('auth.invitation.join', { system: inv.system_name })}</h1>
        <p className="muted">
          <Trans i18nKey="auth.invitation.invitedAs" values={{ role: roleLabel(inv.role), email: inv.email, until: fmtDateTime(inv.expires_at) }}
            components={{ b: <strong className="text-fg" /> }} />
        </p>
      </div>
      {me.data ? (
        me.data.email === inv.email ? <AcceptSignedIn token={token} info={inv} name={me.data.name} /> : <WrongAccount info={inv} email={me.data.email} />
      ) : inv.user_exists ? (
        <SignInAndAccept token={token} info={inv} />
      ) : (
        <CreateAndAccept token={token} info={inv} />
      )}
    </AuthLayout>
  );
}

function useFinish(info: InvitationInfo) {
  const navigate = useNavigate();
  const invalidate = useInvalidate();
  return async (res: AcceptInvitationResult) => {
    await invalidate('getMe', 'listSystems', 'getDashboard');
    toast(res.account_created ? i18n.t('auth.invitation.welcome') : i18n.t('auth.invitation.joined', { system: info.system_name }));
    navigate(`/s/${res.system_id}/overview`, { replace: true });
  };
}

const useAccept = () => useMutation(acceptInvitationMutation());

function AcceptSignedIn({ token, info, name }: { token: string; info: InvitationInfo; name: string }) {
  const { t } = useTranslation();
  const accept = useAccept();
  const finish = useFinish(info);
  const [error, setError] = useState<ServerError | null>(null);
  return (
    <div className="form">
      {error && <Banner tone="danger" role="alert" title={error.title}>{error.message}</Banner>}
      <Banner tone="info">{t('auth.invitation.signedInAs', { name })}</Banner>
      <div className="form-actions start">
        <Button variant="primary" busy={accept.isPending} onClick={async () => {
          setError(null);
          try { await finish(await accept.mutateAsync({ path: { token } })); } catch (e) { setError(serverErrorOf(e)); }
        }}>{t('auth.invitation.accept')}</Button>
        <ButtonLink to="/">{t('auth.invitation.notNow')}</ButtonLink>
      </div>
    </div>
  );
}

function WrongAccount({ info, email }: { info: InvitationInfo; email: string }) {
  const { t } = useTranslation();
  const { signOut } = useAuth();
  return (
    <>
      <Banner tone="warn" title={t('auth.invitation.wrongTitle', { email })}>{t('auth.invitation.wrongText', { email: info.email })}</Banner>
      <div className="form-actions start">
        <Button variant="primary" onClick={() => signOut()}>{t('auth.invitation.signOutContinue')}</Button>
        <ButtonLink to="/">{t('common.backToDashboard')}</ButtonLink>
      </div>
    </>
  );
}

function SignInAndAccept({ token, info }: { token: string; info: InvitationInfo }) {
  const { t } = useTranslation();
  const { signIn } = useAuth();
  const accept = useAccept();
  const finish = useFinish(info);
  const [error, setError] = useState<ServerError | null>(null);
  const { register, handleSubmit, formState } = useForm<{ password: string }>();
  return (
    <form className="form" noValidate onSubmit={handleSubmit(async ({ password }) => {
      setError(null);
      try {
        await signIn(info.email, password);
        await finish(await accept.mutateAsync({ path: { token } }));
      } catch (e) { setError(serverErrorOf(e)); }
    })}>
      {error && <Banner tone="danger" role="alert" title={error.title}>{error.message}</Banner>}
      <Field label={t('common.email')}><Input type="email" value={info.email} readOnly /></Field>
      <Field label={t('common.password')} required help={<>{t('auth.invitation.haveAccount')} <Link to="/forgot-password">{t('auth.forgotPassword')}</Link></>} error={formState.errors.password?.message}>
        <Input type="password" autoComplete="current-password" autoFocus {...register('password', { required: 'validation.passwordRequired' })} />
      </Field>
      <div className="form-actions start"><Button type="submit" variant="primary" busy={formState.isSubmitting}>{t('auth.invitation.signInJoin')}</Button></div>
    </form>
  );
}

const createSchema = z.object({ name: z.string().trim().min(1, 'validation.yourNameRequired').max(200), ...newPasswordFields }).refine(...passwordsMatch);
type CreateValues = z.infer<typeof createSchema>;

function CreateAndAccept({ token, info }: { token: string; info: InvitationInfo }) {
  const { t } = useTranslation();
  const { signIn } = useAuth();
  const accept = useAccept();
  const finish = useFinish(info);
  const [error, setError] = useState<ServerError | null>(null);
  const { register, handleSubmit, formState, setError: setFieldError } = useForm<CreateValues>({ resolver: zodResolver(createSchema) });
  const e = formState.errors;
  return (
    <form className="form" noValidate onSubmit={handleSubmit(async (v) => {
      setError(null);
      try {
        const res = await accept.mutateAsync({ path: { token }, body: { name: v.name, password: v.password } });
        await signIn(info.email, v.password);
        await finish(res);
      } catch (err) {
        setError(applyServerError(err, setFieldError, { name: 'name', password: 'password' }));
      }
    })}>
      {error && <Banner tone="danger" role="alert" title={error.title}>{error.message}</Banner>}
      <Field label={t('common.email')} help={t('auth.invitation.confirmed')}><Input type="email" value={info.email} readOnly /></Field>
      <Field label={t('auth.invitation.yourName')} required error={e.name?.message}><Input autoComplete="name" maxLength={200} autoFocus {...register('name')} /></Field>
      <Field label={t('auth.invitation.choosePassword')} required help={t('auth.passwordHelp')} error={e.password?.message}>
        <Input type="password" autoComplete="new-password" {...register('password')} />
      </Field>
      <Field label={t('auth.repeatPassword')} required error={e.password2?.message}><Input type="password" autoComplete="new-password" {...register('password2')} /></Field>
      <div className="form-actions start"><Button type="submit" variant="primary" busy={formState.isSubmitting}>{t('auth.invitation.createJoin')}</Button></div>
    </form>
  );
}
