import { useState, type ReactNode } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { useUnsavedChanges } from '@/components/ui/Confirm';
import { Field, FormSection, FormSections, Input } from '@/components/ui/form';
import { ImageUpload } from '@/components/ui/ImageUpload';
import { Avatar, Banner } from '@/components/ui/primitives';
import { applyServerError, type ServerError } from '@/lib/forms';

export interface ProfileValues { name: string; email: string }

/**
 * Picture, name and email of an account: the own profile (email read-only)
 * and a superadmin's user page (email editable).
 */
export function ProfileForm({ user, description, emailEditable, uploads, onSave, onUpload, onRemove }: {
  user: { name: string; email: string; avatar_url?: string };
  description: ReactNode;
  /** Superadmins can change the email; the own profile can't. */
  emailEditable?: boolean;
  /** Pictures can be uploaded (Me.features.uploads). */
  uploads: boolean;
  /** Saves the changed fields; toasts are up to the caller. */
  onSave: (changed: Partial<ProfileValues>) => Promise<unknown>;
  onUpload: (image: Blob) => Promise<unknown>;
  onRemove: () => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const [busy, setBusy] = useState(false);
  const form = useForm<ProfileValues>({ values: { name: user.name, email: user.email } });
  const dirty = form.formState.isDirty;
  useUnsavedChanges(dirty);
  const e = form.formState.errors;

  return (
    <form className="card form" noValidate onSubmit={form.handleSubmit(async (v) => {
      setServerError(null);
      const changed: Partial<ProfileValues> = {};
      if (v.name.trim() !== user.name) changed.name = v.name.trim();
      if (emailEditable && v.email.trim().toLowerCase() !== user.email) changed.email = v.email.trim();
      setBusy(true);
      try {
        if (Object.keys(changed).length) await onSave(changed);
        form.reset(v);
      } catch (err) {
        setServerError(applyServerError(err, form.setError, { name: 'name', email: 'email', 'a user with': 'email' }));
      } finally { setBusy(false); }
    })}>
      {serverError && <Banner tone="danger" role="alert" title={serverError.title}>{serverError.message}</Banner>}
      <FormSections>
        <FormSection title={t('profile.personal')} description={description}>
          <div className="grid gap-4">
            {uploads && (
              <ImageUpload square max={512} hasImage={!!user.avatar_url} onUpload={onUpload} onRemove={onRemove}
                removeTitle={t('picture.removeAvatar')}
                preview={<Avatar name={user.name || user.email} src={user.avatar_url} className="avatar-lg" />} />
            )}
            <Field label={t('common.name')} required error={e.name?.message}>
              <Input maxLength={200} autoComplete={emailEditable ? 'off' : 'name'} {...form.register('name', { validate: (v) => v.trim() !== '' || 'validation.nameRequired' })} />
            </Field>
            {emailEditable ? (
              <Field label={t('common.email')} required help={t('admin.user.emailHelp')} error={e.email?.message}>
                <Input type="email" autoComplete="off" {...form.register('email', {
                  validate: (v) => (v.trim() === '' ? 'validation.emailRequired' : /^\S+@\S+\.\S+$/.test(v.trim()) || 'validation.emailInvalid'),
                })} />
              </Field>
            ) : (
              <Field label={t('common.email')} help={t('profile.emailHelp')}><Input type="email" value={user.email} readOnly /></Field>
            )}
          </div>
        </FormSection>
      </FormSections>
      <div className="form-actions">
        <Button disabled={!dirty} onClick={() => { form.reset(); setServerError(null); }}>{t('confirm.discardLabel')}</Button>
        <Button type="submit" variant="primary" busy={busy} disabled={!dirty}>{t('common.saveChanges')}</Button>
      </div>
    </form>
  );
}
