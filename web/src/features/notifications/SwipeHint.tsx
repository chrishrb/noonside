import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import { SWIPE_HINT, store } from '@/lib/storage';

/** Explains the swipe gestures once, on touch screens. */
export function SwipeHint() {
  const { t } = useTranslation();
  const [show, setShow] = useState(() => !store.get(SWIPE_HINT) && window.matchMedia('(pointer: coarse)').matches);
  if (!show) return null;
  return (
    <p className="notif-hint sm muted">
      <Icon name="info" size="sm" />
      <span className="flex-1">{t('notifications.swipeHint')}</span>
      <Button variant="ghost" size="sm" icon="x" iconOnly label={t('common.close')}
        onClick={() => { store.set(SWIPE_HINT, '1'); setShow(false); }} />
    </p>
  );
}
