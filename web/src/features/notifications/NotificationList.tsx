import { useRef, useState, type PointerEvent } from 'react';
import { useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import type { Notification, Severity } from '@/api/types';
import { Button } from '@/components/ui/Button';
import { Icon, type IconName } from '@/components/ui/Icon';
import { toastError } from '@/components/ui/toast';
import { fmtRel } from '@/lib/format';
import { useDismiss, useHiddenNotifications, useMarkRead } from './hooks';

const sevIcon: Record<Severity, IconName> = { error: 'alert', warning: 'alert', info: 'info' };

export function SeverityIcon({ severity }: { severity: Severity }) {
  const { t } = useTranslation();
  return (
    <span className={`notif-sev ${severity}`} title={t(`alerts.severity.${severity}`)}>
      <Icon name={sevIcon[severity]} />
      <span className="sr-only">{t(`alerts.severity.${severity}`)}</span>
    </span>
  );
}

// Alerts show their severity; other types their own icon. Unknown types
// (added by a newer server) look like announcements.
const typeIcon: Record<string, IconName> = { announcement: 'megaphone', forecast: 'sun', report: 'chart' };
const alertTypes = new Set(['alert_opened', 'alert_resolved', 'alert_reminder', 'test']);

function TypeIcon({ n }: { n: Notification }) {
  if (alertTypes.has(n.type)) return <SeverityIcon severity={n.severity} />;
  return <span className="notif-sev info" aria-hidden="true"><Icon name={typeIcon[n.type] ?? 'megaphone'} /></span>;
}

/**
 * Notifications as a list; opening one marks it read and goes to its page.
 * Each can be removed with its button or, on touch, by swiping it left.
 */
export function NotificationList({ items, onOpen }: { items: Notification[]; onOpen?: () => void }) {
  const navigate = useNavigate();
  const { mark } = useMarkRead();
  const dismiss = useDismiss();
  const hidden = useHiddenNotifications();
  return (
    <ul className="notif-list">
      {items.filter((n) => !hidden.has(n.id)).map((n) => (
        <NotificationItem key={n.id} n={n}
          onOpen={() => {
            onOpen?.();
            if (!n.read_at) mark({ ids: [n.id] }).catch(toastError);
            navigate(n.url);
          }}
          onRemove={() => dismiss(n)} />
      ))}
    </ul>
  );
}

const LOCK = 8; // px before a touch counts as a horizontal swipe (or yields to scrolling)
const COMMIT = 0.35; // share of the width that triggers the action
const FLICK = 0.5; // px/ms at release that triggers it early
const OUT_MS = 180;

interface Drag { id: number; x: number; y: number; lastX: number; lastT: number; v: number; w: number; horizontal?: boolean }

function NotificationItem({ n, onOpen, onRemove }: { n: Notification; onOpen: () => void; onRemove: () => void }) {
  const { t } = useTranslation();
  const row = useRef<HTMLLIElement>(null);
  const drag = useRef<Drag | null>(null);
  const swiped = useRef(false);
  const [dx, setDx] = useState(0);
  const [dragging, setDragging] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const [armed, setArmed] = useState(false);
  const unread = !n.read_at;

  /** Slides the row out and collapses it, then removes it. */
  const remove = () => {
    const el = row.current;
    if (leaving) return;
    if (!el || window.matchMedia('(prefers-reduced-motion: reduce)').matches) { onRemove(); return; }
    setLeaving(true);
    setDx(-el.offsetWidth);
    el.style.height = `${el.offsetHeight}px`;
    setTimeout(() => {
      el.style.height = '0px';
      setTimeout(onRemove, OUT_MS);
    }, OUT_MS);
  };

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    // Mice use the buttons (and keep text selection).
    if (e.pointerType === 'mouse' || leaving) return;
    drag.current = { id: e.pointerId, x: e.clientX, y: e.clientY, lastX: e.clientX, lastT: e.timeStamp, v: 0, w: e.currentTarget.offsetWidth };
    swiped.current = false;
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d || d.id !== e.pointerId) return;
    const mx = e.clientX - d.x, my = e.clientY - d.y;
    if (!d.horizontal) {
      if (Math.abs(mx) < LOCK && Math.abs(my) < LOCK) return;
      if (Math.abs(my) >= Math.abs(mx)) { drag.current = null; return; } // scrolling
      d.horizontal = true;
      swiped.current = true;
      e.currentTarget.setPointerCapture(e.pointerId);
      setDragging(true);
    }
    const dt = e.timeStamp - d.lastT;
    if (dt > 0) d.v = (e.clientX - d.lastX) / dt;
    d.lastX = e.clientX; d.lastT = e.timeStamp;
    setDx(mx > 0 ? Math.min(mx * 0.2, 24) : mx); // only left does something
    setArmed(mx < -d.w * COMMIT);
  };
  const onPointerUp = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    drag.current = null;
    if (!d?.horizontal) return;
    setTimeout(() => { swiped.current = false; }); // after the click this pointerup may cause
    setDragging(false);
    setArmed(false);
    const mx = e.clientX - d.x;
    if (mx < -d.w * COMMIT || (d.v < -FLICK && mx < -40)) { remove(); return; }
    setDx(0);
  };
  const onPointerCancel = () => {
    drag.current = null;
    swiped.current = false;
    setDragging(false);
    setArmed(false);
    setDx(0);
  };

  return (
    <li ref={row} className={clsx('notif-row', leaving && 'leaving')}>
      {dx < 0 && (
        <div className={clsx('notif-swipe-bg', armed && 'armed')} aria-hidden="true">
          <Icon name="trash" />
        </div>
      )}
      <div className="notif-fg" style={{ transform: dx ? `translateX(${dx}px)` : undefined, transition: dragging ? 'none' : undefined }}
        onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerCancel}
        onClickCapture={(e) => {
          // The click that ends a swipe doesn't open the notification.
          if (swiped.current) { e.preventDefault(); e.stopPropagation(); }
        }}>
        <button type="button" className={unread ? 'notif-item unread' : 'notif-item'} onClick={onOpen}>
          <TypeIcon n={n} />
          <span className="body">
            <span className="title">{n.title}</span>
            <span className="sm muted line-clamp-2">{n.body}</span>
            <span className="xs muted">{fmtRel(n.created_at)}</span>
          </span>
          {unread && <span className="notif-unread-dot" aria-label={t('notifications.unread')} />}
        </button>
        <div className="notif-actions">
          <Button variant="ghost" size="sm" icon="x" iconOnly label={t('notifications.remove')} onClick={remove} />
        </div>
      </div>
    </li>
  );
}
