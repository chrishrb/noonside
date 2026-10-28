import { useSyncExternalStore } from 'react';
import { onlineManager } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Icon } from '@/components/ui/Icon';

export function useOnline(): boolean {
  return useSyncExternalStore(
    (cb) => onlineManager.subscribe(cb),
    () => onlineManager.isOnline(),
  );
}

/** Shown under the top bar while offline; pages keep showing the persisted query cache. */
export function OfflineBar() {
  const { t } = useTranslation();
  const online = useOnline();
  if (online) return null;
  return (
    <div className="offline-bar" role="status">
      <Icon name="offline" size="sm" />
      {t('pwa.offline')}
    </div>
  );
}
