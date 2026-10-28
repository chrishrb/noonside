import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';

import { useAuth } from '@/auth/AuthProvider';
import { Button } from '@/components/ui/Button';
import { Field, Input } from '@/components/ui/form';
import { Banner } from '@/components/ui/primitives';
import { AuthLayout, AuthTitles } from './AuthLayout';

const schema = z.object({ email: z.string().trim().min(1, 'validation.emailRequired').email('validation.emailInvalid') });

export function ForgotPasswordPage() {
  const { t } = useTranslation();
  const { resetPassword } = useAuth();
  const [sent, setSent] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { register, handleSubmit, formState } = useForm<z.infer<typeof schema>>({ resolver: zodResolver(schema) });

  return (
    <AuthLayout title={t('auth.forgot.pageTitle')}>
      <AuthTitles title={t('auth.forgot.title')}>{t('auth.forgot.intro')}</AuthTitles>
      {sent ? (
        <Banner tone="info" icon="mail" title={t('auth.forgot.sentTitle')}>{t('auth.forgot.sentText', { email: sent })}</Banner>
      ) : (
        <form
          className="form"
          noValidate
          onSubmit={handleSubmit(async ({ email }) => {
            setError(null);
            try { await resetPassword(email); setSent(email); } catch (e) { setError(e instanceof Error ? e.message : t('auth.forgot.failed')); }
          })}
        >
          {error && <Banner tone="danger" role="alert" title={t('auth.forgot.error')}>{error}</Banner>}
          <Field label={t('common.email')} error={formState.errors.email?.message}>
            <Input type="email" autoComplete="username" autoFocus {...register('email')} />
          </Field>
          <div className="form-actions start"><Button type="submit" variant="primary" icon="mail" busy={formState.isSubmitting}>{t('auth.sendResetLink')}</Button></div>
        </form>
      )}
      <p className="sm"><Link to="/login">{t('auth.backToSignIn')}</Link></p>
    </AuthLayout>
  );
}
