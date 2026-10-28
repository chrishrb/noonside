import { useEffect, useRef, type PointerEvent } from 'react';

/** How long a chart's tooltip stays after a finger leaves the screen. */
export const TOUCH_TIP_MS = 1500;

/**
 * Hides a chart's tooltip shortly after a touch ends (or the browser takes the
 * touch over to scroll), so it doesn't keep covering the chart on phones. A
 * mouse keeps it while hovering, as before. Call `hold` on pointer down and
 * pass `release` to onPointerUp and onPointerCancel.
 */
export function useTouchTip(hide: () => void) {
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return {
    hold: () => window.clearTimeout(timer.current),
    release: (e: PointerEvent) => {
      if (e.pointerType === 'mouse') return;
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(hide, TOUCH_TIP_MS);
    },
  };
}
