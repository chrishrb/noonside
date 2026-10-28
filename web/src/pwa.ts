import { toast as sonner } from 'sonner';
import { registerSW } from 'virtual:pwa-register';
import i18n from '@/i18n';
import { toast } from '@/components/ui/toast';

const HOUR = 60 * 60 * 1000;

/** Registers the service worker and offers a reload when a new version is ready. */
export function registerServiceWorker() {
  if (!('serviceWorker' in navigator)) return;
  const update = registerSW({
    onNeedRefresh() {
      // workbox reports an update found later twice (installed + waiting); the id keeps one toast.
      sonner(i18n.t('pwa.updateReady'), {
        id: 'pwa-update',
        className: 'toast',
        duration: Infinity,
        action: { label: i18n.t('pwa.reload'), onClick: () => void update(true) },
      });
    },
    onOfflineReady() {
      toast(i18n.t('pwa.offlineReady'));
    },
    onRegisteredSW(_url, reg) {
      if (!reg) return;
      // Installed apps stay open for days: look for new versions now and then.
      const check = () => { if (navigator.onLine) void reg.update(); };
      setInterval(check, HOUR);
      document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') check(); });
    },
  });
}
