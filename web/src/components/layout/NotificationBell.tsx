import { listNotificationsOptions } from '@/api/gen/@tanstack/react-query.gen';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Link } from 'react-router';
import { Popover } from 'radix-ui';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import { Spinner } from '@/components/ui/primitives';
import { useTick } from '@/components/ui/misc';
import { toastError } from '@/components/ui/toast';
import { NotificationList } from '@/features/notifications/NotificationList';
import { useHiddenNotifications, useMarkRead } from '@/features/notifications/hooks';

/** Bell with the unread count; opens the latest notifications. */
export function NotificationBell({ unread }: { unread: number }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const list = useQuery({ ...listNotificationsOptions({ query: { limit: 10 } }), enabled: open });
  const { mark, pending } = useMarkRead();
  const hidden = useHiddenNotifications();
  useTick();
  const label = unread ? t('notifications.bellUnread', { count: unread }) : t('notifications.bell');
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button type="button" className="btn btn-ghost btn-icon bell" aria-label={label} title={label}>
          <Icon name="bell" />
          {unread > 0 && <span className="bell-count" aria-hidden="true">{unread > 99 ? '99+' : unread}</span>}
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content className="sys-pop notif-pop" align="end" sideOffset={8} aria-label={t('notifications.title')}>
          <div className="notif-head">
            <strong>{t('notifications.title')}</strong>
            {unread > 0 && <Button size="sm" variant="ghost" busy={pending} onClick={() => mark({ all: true }).catch(toastError)}>{t('notifications.markAllRead')}</Button>}
          </div>
          {list.isPending ? <div className="empty"><Spinner /></div>
            : list.isError ? <p className="sm muted p-2">{t('notifications.loadFailed')}</p>
            : list.data.items.some((n) => !hidden.has(n.id)) ? <NotificationList items={list.data.items} onOpen={() => setOpen(false)} />
            : <p className="sm muted p-2">{t('notifications.emptyText')}</p>}
          <div className="sys-pop-foot">
            <Link className="sys-action" to="/notifications" onClick={() => setOpen(false)}><Icon name="bell" size="sm" />{t('notifications.viewAll')}</Link>
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
