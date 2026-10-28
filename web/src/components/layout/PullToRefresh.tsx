import { useEffect, useRef, useState, type RefObject } from 'react';
import { onlineManager, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Icon } from '@/components/ui/Icon';
import { Spinner } from '@/components/ui/primitives';

const MOBILE = '(max-width: 1023px)';
const THRESHOLD = 64; // px of (damped) pull that triggers a refresh
const HOLD = 48; // height kept open while refreshing
const MAX = 110;

/**
 * Pull down at the top of the page (below 1024px, where `scrollRef` is the
 * scroll container) to refetch every query on screen.
 */
export function PullToRefresh({ scrollRef }: { scrollRef: RefObject<HTMLElement | null> }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [pull, setPull] = useState(0);
  const [dragging, setDragging] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const busy = useRef(false);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    let start: { x: number; y: number } | null = null;
    let active = false; // decided on the first move: vertical pull or not
    let dist = 0;

    const onStart = (e: TouchEvent) => {
      if (busy.current || e.touches.length !== 1 || el.scrollTop > 0 || !window.matchMedia(MOBILE).matches) { start = null; return; }
      start = { x: e.touches[0].clientX, y: e.touches[0].clientY };
      active = false;
      dist = 0;
    };
    const onMove = (e: TouchEvent) => {
      if (!start) return;
      const dx = e.touches[0].clientX - start.x, dy = e.touches[0].clientY - start.y;
      if (!active) {
        // Scrolling up, or sideways (tables, chips): not a pull.
        if (dy <= 0 || Math.abs(dx) > dy || el.scrollTop > 0) { if (Math.abs(dx) > 4 || dy < -4) start = null; return; }
        active = true;
        setDragging(true);
      }
      e.preventDefault(); // no rubber band while pulling
      dist = Math.min(MAX, Math.max(0, dy) * 0.5);
      setPull(dist);
    };
    const onEnd = async () => {
      if (!start || !active) { start = null; return; }
      start = null;
      active = false;
      setDragging(false);
      if (dist < THRESHOLD || !onlineManager.isOnline()) { setPull(0); return; }
      busy.current = true;
      setRefreshing(true);
      setPull(HOLD);
      const min = new Promise((r) => setTimeout(r, 600)); // long enough to see it happened
      await Promise.all([qc.refetchQueries({ type: 'active' }), min]);
      busy.current = false;
      setRefreshing(false);
      setPull(0);
    };

    el.addEventListener('touchstart', onStart, { passive: true });
    el.addEventListener('touchmove', onMove, { passive: false });
    el.addEventListener('touchend', onEnd);
    el.addEventListener('touchcancel', onEnd);
    return () => {
      el.removeEventListener('touchstart', onStart);
      el.removeEventListener('touchmove', onMove);
      el.removeEventListener('touchend', onEnd);
      el.removeEventListener('touchcancel', onEnd);
    };
  }, [scrollRef, qc]);

  const ready = pull >= THRESHOLD;
  return (
    <div className="ptr" style={{ height: pull, transition: dragging ? 'none' : undefined }} aria-hidden={!refreshing}>
      {pull > 0 && (
        <span className="ptr-icon" role={refreshing ? 'status' : undefined} aria-label={refreshing ? t('pwa.refreshing') : undefined}>
          {refreshing ? <Spinner /> : <Icon name="refresh" size="sm" className={ready ? 'ready' : undefined} />}
        </span>
      )}
    </div>
  );
}
