import { getPushConfigOptions } from '@/api/gen/@tanstack/react-query.gen';
import { useQuery } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { FormDialog } from '@/components/ui/Dialog';
import { Icon, type IconName } from '@/components/ui/Icon';
import { LogoMark } from '@/components/ui/Logo';
import { toast, toastError } from '@/components/ui/toast';
import { dismissInstall, promptInstall, useInstall } from '@/lib/install';
import { pushSupported, subscribePush, usePushState } from '@/lib/push';
import { store } from '@/lib/storage';

const PUSH_DISMISSED = 'noonside.pushPromptDismissed';
const DISMISS_DAYS = 30;

/**
 * One tap to set up the app: Chromium installs it and then asks for
 * notifications; iOS has no install API, so it shows the steps instead (the
 * installed app then asks for notifications, see PushNotice).
 */
export function useInstallApp() {
  const { t } = useTranslation();
  const { canPrompt } = useInstall();
  const [iosOpen, setIosOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const start = async () => {
    if (!canPrompt) { setIosOpen(true); return; }
    setBusy(true);
    try {
      if (!await promptInstall()) return;
      // Permission is per origin, so the installed app gets it too. Without a
      // tap left, the browser may not show the prompt; PushNotice asks again.
      const push = pushSupported() ? await subscribePush().catch(() => null) : null;
      toast(push === 'granted' ? t('pwa.installedWithPush') : t('pwa.installed'));
    } catch (e) { toastError(e); } finally { setBusy(false); }
  };

  const dialog = <IosInstallDialog open={iosOpen} onOpenChange={setIosOpen} />;
  return { start, busy, dialog };
}

function IosInstallDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation();
  return (
    <FormDialog open={open} onOpenChange={onOpenChange} title={t('pwa.installTitle')} subtitle={t('pwa.ios.intro')}
      submitLabel={t('pwa.ios.done')} hideCancel>
      <ol className="grid gap-4">
        <Step n={1} icon="share">{t('pwa.ios.share')}</Step>
        <Step n={2} icon="plus">{t('pwa.ios.add')}</Step>
        <Step n={3} icon="bell">{t('pwa.ios.open')}</Step>
      </ol>
    </FormDialog>
  );
}

function Step({ n, icon, children }: { n: number; icon: IconName; children: ReactNode }) {
  return (
    <li className="flex items-start gap-3">
      <span className="grid size-7 flex-none place-items-center rounded-full bg-accent-soft text-sm font-semibold text-accent-text" aria-hidden>{n}</span>
      <span className="flex-1 pt-0.5">{children}</span>
      <Icon name={icon} size="sm" className="mt-1 flex-none text-muted" />
    </li>
  );
}

/** Notice above the tab bar (phones, tablets): install in the browser tab, then turn on notifications in the app. */
export function InstallBar() {
  const { standalone } = useInstall();
  return standalone ? <PushNotice /> : <InstallNotice />;
}

function InstallNotice() {
  const { t } = useTranslation();
  const { canPrompt, ios, dismissed } = useInstall();
  const { start, busy, dialog } = useInstallApp();
  if (dismissed || !(canPrompt || ios)) return null;
  return (
    <aside className="install-bar" aria-label={t('pwa.installTitle')}>
      <LogoMark />
      <span className="install-text">
        <strong>{t('pwa.installTitle')}</strong>
        <span className="xs muted">{t('pwa.installText')}</span>
      </span>
      <Button variant="primary" size="sm" busy={busy} onClick={start}>{t('pwa.install')}</Button>
      <Button variant="ghost" size="sm" iconOnly icon="x" label={t('pwa.notNow')} onClick={dismissInstall} />
      {dialog}
    </aside>
  );
}

function PushNotice() {
  const { t } = useTranslation();
  const { state, loaded } = usePushState();
  const cfg = useQuery(getPushConfigOptions());
  const [dismissed, setDismissed] = useState(() => Number(store.get(PUSH_DISMISSED) ?? 0) > Date.now());
  const [busy, setBusy] = useState(false);
  if (!loaded || state !== 'off' || !cfg.data?.enabled || dismissed) return null;
  const dismiss = () => {
    store.set(PUSH_DISMISSED, String(Date.now() + DISMISS_DAYS * 24 * 60 * 60 * 1000));
    setDismissed(true);
  };
  return (
    <aside className="install-bar" aria-label={t('pwa.pushTitle')}>
      <span className="brand-mark" aria-hidden><Icon name="bell" size="sm" /></span>
      <span className="install-text">
        <strong>{t('pwa.pushTitle')}</strong>
        <span className="xs muted">{t('pwa.pushText')}</span>
      </span>
      <Button variant="primary" size="sm" busy={busy} onClick={async () => {
        setBusy(true);
        try {
          const permission = await subscribePush();
          if (permission === 'granted') toast(t('notifications.push.enabled'));
          else dismiss(); // declined: don't ask again right away
        } catch (e) { toastError(e); } finally { setBusy(false); }
      }}>{t('notifications.push.enable')}</Button>
      <Button variant="ghost" size="sm" iconOnly icon="x" label={t('pwa.notNow')} onClick={dismiss} />
    </aside>
  );
}
