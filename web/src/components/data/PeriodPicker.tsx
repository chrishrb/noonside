import { useRef, useState, type KeyboardEvent } from 'react';
import { Popover } from 'radix-ui';
import { useTranslation } from 'react-i18next';
import type { Period } from '@/api/types';
import { btnClass } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import { daysInMonth, fmtDayLong, fmtMonth, monthNames, pad, parseYmd, weekdayNames, ymd, type YMD } from '@/lib/format';

type View = Exclude<Period, 'total'>;
const LEVEL: Record<View, number> = { day: 0, month: 1, year: 2 };

export interface PeriodPickerProps {
  period: View;
  /** Selected date and the selectable range, as YYYY-MM-DD. */
  date: string;
  min: string;
  max: string;
  /** Text on the trigger, e.g. "Mon, 28 Sept 2026". */
  label: string;
  onPick: (date: string) => void;
}

/** Trigger showing the period, opening a calendar with day, month and year grids. */
export function PeriodPicker({ period, date, min, max, label, onPick }: PeriodPickerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const content = useRef<HTMLDivElement>(null);
  const pickKey = period === 'day' ? 'pickDay' : period === 'month' ? 'pickMonth' : 'pickYear';
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button type="button" className={btnClass('secondary', 'md', false, 'dp-trigger')} aria-haspopup="dialog" aria-label={`${t(`dateNav.${pickKey}`)}: ${label}`}>
          <Icon name="calendar" size="sm" />
          <span aria-live="polite">{label}</span>
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content ref={content} className="dp" align="center" sideOffset={6} aria-label={t(`dateNav.${pickKey}`)}
          onOpenAutoFocus={(e) => { e.preventDefault(); content.current?.querySelector<HTMLElement>('[data-focus]')?.focus(); }}>
          <Calendar period={period} date={date} min={min} max={max} onPick={(d) => { setOpen(false); onPick(d); }} />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

const clampStr = (s: string, lo: string, hi: string) => (s < lo ? lo : s > hi ? hi : s);
const addMonths = (d: YMD, n: number): YMD => {
  const m = d.year * 12 + d.month - 1 + n;
  const year = Math.floor(m / 12), month = (m % 12) + 1;
  return { year, month, day: Math.min(d.day, daysInMonth(year, month)) };
};
const addDaysYmd = (d: YMD, n: number): YMD => {
  const x = new Date(Date.UTC(d.year, d.month - 1, d.day + n));
  return { year: x.getUTCFullYear(), month: x.getUTCMonth() + 1, day: x.getUTCDate() };
};
const monthKey = (d: YMD) => `${d.year}-${pad(d.month)}`;

function Calendar({ period, date, min, max, onPick }: Omit<PeriodPickerProps, 'label'>) {
  const { t } = useTranslation();
  const [view, setView] = useState<View>(period);
  // The focused cell; its month (day view), year (month view) or page (year view) is shown.
  const [focus, setFocus] = useState<YMD>(() => parseYmd(clampStr(date, min, max)));

  const sel = parseYmd(date), lo = parseYmd(min), hi = parseYmd(max), today = max;
  const minY = lo.year, maxY = hi.year;
  // Year pages of 12 end at the latest year, so the first page shows the most recent ones.
  const pageStart = maxY - 11 - 12 * Math.floor((maxY - focus.year) / 12);

  // `from` (an element in the grid) moves keyboard focus to the new focused cell after the update.
  const go = (d: YMD, v: View = view, from?: Element) => {
    setFocus(parseYmd(clampStr(ymd(d), min, max)));
    setView(v);
    const body = from?.closest('.dp-body');
    if (body) requestAnimationFrame(() => body.querySelector<HTMLElement>('[data-focus]')?.focus());
  };
  const choose = (d: YMD, level: View, from: Element) => {
    if (LEVEL[level] === LEVEL[period]) onPick(ymd(level === 'day' ? d : level === 'month' ? { ...d, day: 1 } : { year: d.year, month: 1, day: 1 }));
    else go(d, level === 'year' ? 'month' : 'day', from);
  };

  const onKey = (e: KeyboardEvent) => {
    const step: Record<string, number> = view === 'day'
      ? { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
      : { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -3, ArrowDown: 3 };
    const n = step[e.key];
    const page = e.key === 'PageUp' ? -1 : e.key === 'PageDown' ? 1 : 0;
    if (!n && !page) return;
    e.preventDefault();
    const el = e.currentTarget;
    if (view === 'day') go(n ? addDaysYmd(focus, n) : addMonths(focus, page), view, el);
    else if (view === 'month') go(n ? addMonths(focus, n) : addMonths(focus, 12 * page), view, el);
    else go(addMonths(focus, 12 * (n || 12 * page)), view, el);
  };

  // Header: title (zooms out) and arrows for the shown month, year or page of years.
  let title: string, up: View | null, prev: YMD, next: YMD, prevOk: boolean, nextOk: boolean, prevLabel: string, nextLabel: string;
  if (view === 'day') {
    title = fmtMonth(focus.year, focus.month); up = 'month';
    prev = addMonths(focus, -1); next = addMonths(focus, 1);
    prevOk = monthKey(prev) >= monthKey(lo); nextOk = monthKey(next) <= monthKey(hi);
    prevLabel = t('dateNav.prevMonth'); nextLabel = t('dateNav.nextMonth');
  } else if (view === 'month') {
    title = String(focus.year); up = 'year';
    prev = addMonths(focus, -12); next = addMonths(focus, 12);
    prevOk = prev.year >= minY; nextOk = next.year <= maxY;
    prevLabel = t('dateNav.prevYear'); nextLabel = t('dateNav.nextYear');
  } else {
    title = `${Math.max(pageStart, minY)} – ${pageStart + 11}`; up = null;
    prev = addMonths(focus, -144); next = addMonths(focus, 144);
    prevOk = pageStart > minY; nextOk = pageStart + 11 < maxY;
    prevLabel = t('dateNav.prevYears'); nextLabel = t('dateNav.nextYears');
  }
  const arrow = (to: YMD, ok: boolean, text: string, icon: 'left' | 'right') => (
    <button type="button" className={btnClass('ghost', 'sm', true)} aria-label={text} title={text} disabled={!ok}
      onClick={() => go(to)}><Icon name={icon} size="sm" /></button>
  );

  const cell = (key: string, text: string, opts: { focused: boolean; selected: boolean; current: boolean; disabled: boolean; aria?: string; onClick: (el: Element) => void }) => (
    <button key={key} type="button" className="dp-cell" tabIndex={opts.focused ? 0 : -1} data-focus={opts.focused || undefined}
      aria-pressed={opts.selected} aria-current={opts.current ? 'date' : undefined} aria-label={opts.aria} disabled={opts.disabled}
      onClick={(e) => opts.onClick(e.currentTarget)}>{text}</button>
  );

  let body;
  if (view === 'day') {
    const first = (new Date(Date.UTC(focus.year, focus.month - 1, 1)).getUTCDay() + 6) % 7; // Monday = 0
    const n = daysInMonth(focus.year, focus.month);
    body = (
      <div className="dp-grid dp-grid-7" role="group" aria-label={title}>
        {weekdayNames('short').map((w) => <span key={w} className="dp-wd" aria-hidden>{w.replace('.', '')}</span>)}
        {Array.from({ length: first }, (_, i) => <span key={`b${i}`} />)}
        {Array.from({ length: n }, (_, i) => {
          const d = { year: focus.year, month: focus.month, day: i + 1 }, s = ymd(d);
          return cell(s, String(i + 1), {
            focused: d.day === focus.day, selected: s === date && period === 'day', current: s === today,
            disabled: s < min || s > max, aria: fmtDayLong(s), onClick: (el) => choose(d, 'day', el),
          });
        })}
      </div>
    );
  } else if (view === 'month') {
    const names = monthNames('short');
    body = (
      <div className="dp-grid dp-grid-3" role="group" aria-label={title}>
        {names.map((name, i) => {
          const d = { year: focus.year, month: i + 1, day: Math.min(focus.day, daysInMonth(focus.year, i + 1)) }, k = monthKey(d);
          return cell(k, name.replace('.', ''), {
            focused: d.month === focus.month, selected: k === monthKey(sel), current: k === today.slice(0, 7),
            disabled: k < min.slice(0, 7) || k > max.slice(0, 7), aria: fmtMonth(d.year, d.month), onClick: (el) => choose(d, 'month', el),
          });
        })}
      </div>
    );
  } else {
    body = (
      <div className="dp-grid dp-grid-3" role="group" aria-label={title}>
        {Array.from({ length: 12 }, (_, i) => {
          const y = pageStart + i, d = { year: y, month: focus.month, day: Math.min(focus.day, daysInMonth(y, focus.month)) };
          return cell(String(y), String(y), {
            focused: y === focus.year, selected: y === sel.year, current: String(y) === today.slice(0, 4),
            disabled: y < minY || y > maxY, onClick: (el) => choose(d, 'year', el),
          });
        })}
      </div>
    );
  }

  return (
    <>
      <div className="dp-head">
        {arrow(prev, prevOk, prevLabel, 'left')}
        {up ? (
          <button type="button" className="dp-title" aria-label={`${title}, ${t(up === 'month' ? 'dateNav.chooseMonth' : 'dateNav.chooseYear')}`}
            onClick={() => go(focus, up)}>
            {title}<Icon name="down" size="sm" />
          </button>
        ) : <span className="dp-title" aria-live="polite">{title}</span>}
        {arrow(next, nextOk, nextLabel, 'right')}
      </div>
      <div className="dp-body" onKeyDown={onKey}>{body}</div>
    </>
  );
}
