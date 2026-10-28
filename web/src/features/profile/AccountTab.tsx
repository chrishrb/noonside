import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { deleteMyAvatarMutation, updateMeMutation, uploadMyAvatarMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import { useAuth } from '@/auth/AuthProvider';
import { useMeData } from '@/auth/useMe';
import { Button } from '@/components/ui/Button';
import { Badge, Card } from '@/components/ui/primitives';
import { toast, toastError } from '@/components/ui/toast';
import { ProfileForm } from './ProfileForm';

export function AccountTab() {
  const { t } = useTranslation();
  const me = useMeData();
  const { signOut, resetPassword } = useAuth();
  const invalidate = useInvalidate();
  const update = useMutation(updateMeMutation());
  const upload = useMutation(uploadMyAvatarMutation());
  const remove = useMutation(deleteMyAvatarMutation());
  const [resetBusy, setResetBusy] = useState(false);
  const refresh = () => invalidate('getMe', 'listMembers', 'listUsers', 'getUser');

  return (
    <div className="settings-grid">
      <ProfileForm user={me} uploads={me.features.uploads}
        description={me.is_superadmin
          ? <><Badge tone="accent">{t('common.superadmin')}</Badge> {t('profile.superadminText')}</>
          : t('profile.personalHelp')}
        onSave={async ({ name }) => {
          await update.mutateAsync({ body: { name } });
          await refresh();
          toast(t('profile.nameSaved'));
        }}
        onUpload={async (image) => { await upload.mutateAsync({ body: image }); await refresh(); }}
        onRemove={async () => { await remove.mutateAsync({}); await refresh(); }} />

      <Card as="section" aria-labelledby="pw-h">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="grid max-w-[60ch] gap-1"><h2 id="pw-h">{t('common.password')}</h2><p className="sm muted">{t('auth.forgot.intro')}</p></div>
          <Button icon="mail" busy={resetBusy} onClick={async () => {
            setResetBusy(true);
            try { await resetPassword(me.email); toast(t('profile.resetSent', { email: me.email })); } catch (e) { toastError(e); } finally { setResetBusy(false); }
          }}>{t('auth.sendResetLink')}</Button>
        </div>
      </Card>

      <Card as="section" aria-labelledby="so-h">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="grid max-w-[60ch] gap-1"><h2 id="so-h">{t('common.signOut')}</h2><p className="sm muted">{t('profile.signOutText')}</p></div>
          <Button icon="logout" onClick={() => signOut().then(() => toast(t('common.signedOut')))}>{t('common.signOut')}</Button>
        </div>
      </Card>
    </div>
  );
}
