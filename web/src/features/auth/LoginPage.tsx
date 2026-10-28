import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Link, Navigate, useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';

import { useAuth } from '@/auth/AuthProvider';
import { Button } from '@/components/ui/Button';
import { Field, Input } from '@/components/ui/form';
import { Banner } from '@/components/ui/primitives';
import { toast } from '@/components/ui/toast';
import { AuthLayout, AuthTitles } from './AuthLayout';
import { DevAccounts } from './DevAccounts';

const schema = z.object({
  email: z.string().trim().min(1, 'validation.emailRequired').email('validation.emailInvalid'),
  password: z.string().min(1, 'validation.passwordRequired'),
});
type Values = z.infer<typeof schema>;

export function LoginPage() {
  const { t } = useTranslation();
  const { session, ready, signIn } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from;
  const [error, setError] = useState<string | null>(null);
  const { register, handleSubmit, formState } = useForm<Values>({ resolver: zodResolver(schema) });

  if (ready && session) return <Navigate to={from || '/'} replace />;

  const login = async (email: string, password: string) => {
    setError(null);
    try {
      await signIn(email, password);
      toast(t('auth.login.signedIn'));
      navigate(from || '/', { replace: true });
    } catch (e) {
      setError(e instanceof Error ? e.message : t('auth.login.failed'));
    }
  };

  return (
    <AuthLayout title={t('auth.login.title')}>
      <AuthTitles title={t('auth.login.title')}>{t('auth.login.intro')}</AuthTitles>
      <form
        className="form"
        noValidate
        onSubmit={handleSubmit((v) => login(v.email, v.password))}
      >
        {error && <Banner tone="danger" role="alert" title={t('auth.login.error')}>{error}.</Banner>}
        <Field label={t('common.email')} error={formState.errors.email?.message}>
          <Input type="email" autoComplete="username" autoFocus {...register('email')} />
        </Field>
        <Field label={t('common.password')} error={formState.errors.password?.message}>
          <Input type="password" autoComplete="current-password" {...register('password')} />
        </Field>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Link className="btn btn-ghost btn-sm" style={{ paddingInline: 4 }} to="/forgot-password">{t('auth.forgotPassword')}</Link>
          <Button type="submit" variant="primary" busy={formState.isSubmitting}>{t('auth.login.submit')}</Button>
        </div>
      </form>
      {import.meta.env.DEV && <DevAccounts onSignIn={login} />}
    </AuthLayout>
  );
}
