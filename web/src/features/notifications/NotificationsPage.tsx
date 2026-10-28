import { useInfiniteQuery } from '@tanstack/react-query';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import { listNotificationsInfiniteOptions } from '@/api/gen/@tanstack/react-query.gen';
import { Content, PageHeader } from '@/components/layout/Page';
import { Button } from '@/components/ui/Button';
import { Switch } from '@/components/ui/form';
import { Banner, Card, EmptyState, Spinner } from '@/components/ui/primitives';
import { toast, toastError } from '@/components/ui/toast';
import { useConfirm } from '@/components/ui/Confirm';
import { useTick } from '@/components/ui/misc';
import { NotificationList } from './NotificationList';
import { useClearRead, useHiddenNotifications, useMarkRead, useUnreadCount } from './hooks';
import { SwipeHint } from './SwipeHint';

const PAGE = 50;

export function NotificationsPage() {
  const { t } = useTranslation();
  useTick();
  const [search, setSearch] = useSearchParams();
  const unreadOnly = search.get('unread') === '1';
  const { mark, pending } = useMarkRead();
  const clearRead = useClearRead();
  const confirm = useConfirm();
  const hidden = useHiddenNotifications();
  const query = { unread: unreadOnly || undefined, limit: PAGE };
  const list = useInfiniteQuery({
    ...listNotificationsInfiniteOptions({ query }),
    initialPageParam: {}, // the first page has no cursor
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const unread = useUnreadCount();
  const anyRead = items.some((n) => n.read_at && !hidden.has(n.id));
  const shown = items.filter((n) => !hidden.has(n.id)).length;

  return (
    <Content>
      <PageHeader title={t('notifications.title')} sub={t('notifications.intro')}
        actions={<>
          <Button icon="trash" disabled={!anyRead}
            onClick={async () => {
              const ok = await confirm({ title: t('notifications.removeRead.title'), message: t('notifications.removeRead.message'),
                confirmLabel: t('notifications.removeRead.label'), danger: true, onConfirm: clearRead });
              if (ok) toast(t('notifications.removeRead.done'));
            }}>{t('notifications.removeRead.label')}</Button>
          <Button icon="check" disabled={!unread} busy={pending}
            onClick={() => mark({ all: true }).then(() => toast(t('notifications.allRead')), toastError)}>{t('notifications.markAllRead')}</Button>
        </>} />
      <div className="mb-4">
        <Switch label={t('notifications.unreadOnly')} checked={unreadOnly}
          onCheckedChange={(v) => setSearch(v ? { unread: '1' } : {}, { replace: true })} />
      </div>
      {shown > 0 && <SwipeHint />}
      {list.isError ? <Banner tone="danger" title={t('notifications.loadFailed')}>{list.error.message}</Banner> : (
        <Card flush className="notif-page">
          {list.isPending ? <div className="empty"><Spinner /></div>
            : shown ? <NotificationList items={items} />
            : <EmptyState icon="bell" title={unreadOnly ? t('notifications.emptyUnread') : t('notifications.empty')} text={t('notifications.emptyText')} />}
        </Card>
      )}
      {list.hasNextPage && (
        <div className="mt-4 flex justify-center">
          <Button busy={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{t('notifications.loadMore')}</Button>
        </div>
      )}
    </Content>
  );
}
