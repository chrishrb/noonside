import { useState, type PointerEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Popover } from 'radix-ui';
import clsx from 'clsx';
import { Icon } from '@/components/ui/Icon';
import { useTick } from '@/components/ui/misc';
import { fmtDateTime, fmtRel } from '@/lib/format';

/**
 * "Updated 3 min ago" for live data, refreshed every 30 s. When the server
 * marks the data stale (no reading for a while although the sun is up), it
 * turns amber and explains why on hover or tap.
 */
export function Freshness({ time, stale, tz, format, className }: {
  time: string | null | undefined;
  stale?: boolean;
  /** Time zone for the absolute time in the explanation. */
  tz?: string;
  /** Wraps the relative time, e.g. (when) => t('status.updated', { when }). */
  format?: (when: string) => ReactNode;
  className?: string;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  useTick();
  const when = fmtRel(time);
  const text = format ? format(when) : t('freshness.updated', { when });
  if (!stale) return <span className={clsx('freshness', className)}>{text}</span>;
  const hover = (next: boolean) => (e: PointerEvent) => { if (e.pointerType === 'mouse') setOpen(next); };
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button type="button" className={clsx('freshness stale', className)} onPointerEnter={hover(true)} onPointerLeave={hover(false)}>
          <Icon name="alert" size="sm" />
          <span className="sr-only">{t('freshness.staleTitle')}: </span>
          {text}
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content className="freshness-tip" side="top" sideOffset={6} collisionPadding={16} onOpenAutoFocus={(e) => e.preventDefault()}>
          <strong>{t('freshness.staleTitle')}</strong>
          <span>{t('freshness.staleHelp', { time: fmtDateTime(time, tz) })}</span>
          <Popover.Arrow className="freshness-tip-arrow" />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
