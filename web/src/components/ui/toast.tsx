import { toast as sonner } from 'sonner';
import i18n from '@/i18n';
import { cap } from '@/lib/format';
import { Icon } from './Icon';

/** Short confirmation message (bottom right). */
export function toast(message: string) {
  sonner(message, { className: 'toast', duration: 3200, icon: <Icon name="check" size="sm" /> });
}

export function toastError(err: unknown) {
  const msg = err instanceof Error && err.message ? err.message : i18n.t('common.somethingWrong');
  sonner(cap(msg), { className: 'toast err', duration: 6000, icon: <Icon name="alert" size="sm" /> });
}
