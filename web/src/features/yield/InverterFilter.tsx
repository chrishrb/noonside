import { useState, useSyncExternalStore, type ReactNode } from 'react';
import { Popover } from 'radix-ui';
import { useTranslation } from 'react-i18next';
import type { Inverter } from '@/api/types';
import { Button } from '@/components/ui/Button';
import { FormDialog } from '@/components/ui/Dialog';
import { Swatch } from '@/components/ui/primitives';
import { fmtKwp } from '@/lib/format';

export interface FilterSection {
  key: string;
  /** Plane name; empty when no inverter is on a plane. */
  label: string;
  color?: string;
  invs: Inverter[];
}

const PHONE = '(max-width: 767px)';
const usePhone = () => useSyncExternalStore((cb) => {
  const mq = window.matchMedia(PHONE);
  mq.addEventListener('change', cb);
  return () => mq.removeEventListener('change', cb);
}, () => window.matchMedia(PHONE).matches);

/**
 * One row: a button with the current selection, and `children` (the chart
 * view toggle) at the end. The button opens a checklist per group, where a
 * group header selects the whole group, in a popover, or a bottom sheet on
 * phones.
 */
export function InverterFilter({ summary, sections, invs, sel, onChange, children }: {
  summary: string; sections: FilterSection[]; invs: Inverter[]; children?: ReactNode;
  /** Selected inverter ids; empty means all. */
  sel: string[]; onChange: (next: string[]) => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const phone = usePhone();
  const shown = new Set(sel.length ? sel : invs.map((i) => i.id));
  // At least one inverter stays shown; all of them is stored as "all" (empty).
  const set = (next: Set<string>) => {
    if (!next.size) return;
    onChange(next.size === invs.length ? [] : invs.filter((i) => next.has(i.id)).map((i) => i.id));
  };
  const toggle = (ids: string[], on: boolean) => {
    const next = new Set(shown);
    for (const id of ids) if (on) next.add(id); else next.delete(id);
    set(next);
  };

  const list = (
    <div className={sections.length > 1 ? 'pick-list cols' : 'pick-list'}>
      <label className="pick-row pick-all">
        <span className="pick-name">{t('yield.allInverters')}</span>
        <input type="checkbox" checked={!sel.length} onChange={() => onChange([])} />
      </label>
      {sections.map((s) => {
        const ids = s.invs.map((i) => i.id);
        const n = ids.filter((id) => shown.has(id)).length;
        return (
          <div key={s.key} role="group" aria-label={s.label || undefined}>
            {s.label && (
              <label className="pick-row pick-head">
                {s.color && <Swatch color={s.color} />}
                <span className="pick-name eyebrow">{s.label}</span>
                <input type="checkbox" checked={n === ids.length} ref={(el) => { if (el) el.indeterminate = n > 0 && n < ids.length; }}
                  onChange={(e) => toggle(ids, e.target.checked)} />
              </label>
            )}
            {s.invs.map((i) => (
              <label key={i.id} className="pick-row">
                <Swatch color={i.color} />
                <span className="pick-name">{i.name}</span>
                <span className="xs muted">{fmtKwp(i.peak_power_kwp)}</span>
                <input type="checkbox" checked={shown.has(i.id)} onChange={(e) => toggle([i.id], e.target.checked)} />
              </label>
            ))}
          </div>
        );
      })}
    </div>
  );
  // Radix sets the popup attributes on the popover trigger itself.
  const trigger = phone
    ? <Button icon="sliders" aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(true)}>{summary}</Button>
    : <Button icon="sliders">{summary}</Button>;

  return (
    <div className="inv-filter">
      {phone ? trigger : (
        <Popover.Root open={open} onOpenChange={setOpen}>
          <Popover.Trigger asChild>{trigger}</Popover.Trigger>
          <Popover.Portal>
            <Popover.Content className="inv-pop" align="start" sideOffset={6} aria-label={t('yield.invertersShown')}>{list}</Popover.Content>
          </Popover.Portal>
        </Popover.Root>
      )}
      <div className="inv-filter-end">{children}</div>
      {phone && <FormDialog open={open} onOpenChange={setOpen} title={t('yield.invertersShown')} hideCancel submitLabel={t('common.done')}
        onSubmit={() => setOpen(false)}>
        {list}
      </FormDialog>}
    </div>
  );
}
