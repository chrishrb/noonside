import { Link } from 'react-router';
import { btnClass } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import type { Period } from '@/api/types';
import { useTranslation } from 'react-i18next';
import { currentLabel } from '@/lib/period';
import { PeriodPicker } from './PeriodPicker';

export interface DateNavProps {
  period: Period;
  date: string;
  label: string;
  prevDate?: string;
  nextDate?: string;
  prevDisabled?: boolean;
  /** Earliest selectable day (commissioning) and today, as YYYY-MM-DD. */
  min: string;
  max: string;
  /** URL for a date in the period, or for the current period (undefined). */
  hrefFor: (date: string | undefined) => string;
  onPick: (date: string) => void;
}

/** Previous/next period around a period picker, and a jump back to the current period. */
export function DateNav({ period, date, label, prevDate, nextDate, prevDisabled, min, max, hrefFor, onPick }: DateNavProps) {
  const { t } = useTranslation();
  if (period === 'total') return <div className="date-nav"><span className="label" aria-live="polite">{label}</span></div>;

  const arrow = (to: string | undefined, text: string, icon: 'left' | 'right', disabled: boolean) =>
    disabled || !to ? (
      <button className={btnClass('secondary', 'md', true)} type="button" aria-disabled="true" aria-label={text}><Icon name={icon} size="sm" /></button>
    ) : (
      <Link className={btnClass('secondary', 'md', true)} to={hrefFor(to)} aria-label={text}><Icon name={icon} size="sm" /></Link>
    );

  return (
    <div className="date-nav">
      {arrow(prevDate, t('dateNav.previous'), 'left', !!prevDisabled)}
      <PeriodPicker period={period} date={date} min={min} max={max} label={label} onPick={onPick} />
      {arrow(nextDate, t('dateNav.next'), 'right', !nextDate)}
      {nextDate && <Link className={btnClass('ghost', 'sm')} to={hrefFor(undefined)}>{currentLabel(period)}</Link>}
    </div>
  );
}
