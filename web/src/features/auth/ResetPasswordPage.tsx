import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Link, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';

import { useAuth } from '@/auth/AuthProvider';
import { FullScreenLoading } from '@/auth/RequireAuth';
import { Button } from '@/components/ui/Button';
import { Field, Input } from '@/components/ui/form';
import { Banner } from '@/components/ui/primitives';
import { toast } from '@/components/ui/toast';
import { AuthLayout, AuthTitles } from './AuthLayout';
import { newPasswordFields, passwordsMatch } from './passwords';

const schema = z.object(newPasswordFields).refine(...passwordsMatch);

/** Target of the recovery email; the auth client signs the user in from the link. */
export function ResetPasswordPage() {
  const { t } = useTranslation();
  const { session, ready, updatePassword } = useAuth();
  const navigate = useNavigate();
  const [error, setError] = useState<string | null>(null);
  const { register, handleSubmit, formState } = useForm<z.infer<typeof schema>>({ resolver: zodResolver(schema) });

  if (!ready) return <FullScreenLoading text={t('auth.reset.checking')} />;
  if (!session) {
    return (
      <AuthLayout title={t('auth.forgot.pageTitle')}>
        <AuthTitles title={t('auth.reset.invalidTitle')}>{t('auth.reset.invalidText')}</AuthTitles>
        <div><Link className="btn btn-primary" to="/forgot-password">{t('auth.reset.newLink')}</Link></div>
      </AuthLayout>
    );
  }
  return (
    <AuthLayout title={t('auth.reset.title')}>
      <AuthTitles title={t('auth.reset.title')}>{t('auth.reset.for', { email: session.user.email })}</AuthTitles>
      <form
        className="form"
        noValidate
        onSubmit={handleSubmit(async (v) => {
          setError(null);
          try {
            await updatePassword(v.password);
            toast(t('auth.reset.changed'));
            navigate('/', { replace: true });
          } catch (e) {
            setError(e instanceof Error ? e.message : t('auth.reset.failed'));
          }
        })}
      >
        {error && <Banner tone="danger" role="alert" title={t('errors.couldNotSave')}>{error}</Banner>}
        <Field label={t('auth.reset.newPassword')} required help={t('auth.passwordHelp')} error={formState.errors.password?.message}>
          <Input type="password" autoComplete="new-password" autoFocus {...register('password')} />
        </Field>
        <Field label={t('auth.repeatPassword')} required error={formState.errors.password2?.message}>
          <Input type="password" autoComplete="new-password" {...register('password2')} />
        </Field>
        <div className="form-actions start"><Button type="submit" variant="primary" busy={formState.isSubmitting}>{t('auth.reset.submit')}</Button></div>
      </form>
    </AuthLayout>
  );
}
