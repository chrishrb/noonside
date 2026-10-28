import { useId, useMemo, useState, type KeyboardEvent } from 'react';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router';
import { Popover } from 'radix-ui';
import { useTranslation } from 'react-i18next';

import { Icon } from '@/components/ui/Icon';
import { can } from '@/auth/scopes';
import { num } from '@/lib/format';
import { SysTile } from '@/components/ui/primitives';
import type { Me, System } from '@/api/types';

/**
 * Combobox to switch systems, keeping the current sub-page. In the sidebar,
 * or `compact` in the mobile top bar.
 */
export function SystemSwitcher({ me, systems, current, onCreate, compact }: { me: Me; systems: System[]; current: System; onCreate: () => void; compact?: boolean }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const [active, setActive] = useState(0);
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const [search] = useSearchParams();
  // The sidebar and the top bar each render one; ids must differ.
  const labelId = `sys-switch-${useId()}`;

  const items = useMemo(() => {
    const t = q.trim().toLowerCase();
    return systems.filter((s) => !t || `${s.name} ${s.location}`.toLowerCase().includes(t));
  }, [systems, q]);

  const openWith = (o: boolean) => {
    setOpen(o);
    if (o) { setQ(''); setActive(Math.max(0, systems.findIndex((s) => s.id === current.id))); }
  };

  const choose = (id: string) => {
    setOpen(false);
    if (id === current.id) return;
    const sub = /^\/s\/[^/]+(\/.*)?$/.exec(pathname)?.[1] ?? '/overview';
    const keep = new URLSearchParams();
    if (sub === '/overview') for (const k of ['period', 'date']) { const v = search.get(k); if (v) keep.set(k, v); }
    navigate(`/s/${id}${sub}${keep.size ? `?${keep}` : ''}`);
  };

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' && items.length) { e.preventDefault(); setActive((active + 1) % items.length); }
    else if (e.key === 'ArrowUp' && items.length) { e.preventDefault(); setActive((active - 1 + items.length) % items.length); }
    else if (e.key === 'Enter' && items[active]) { e.preventDefault(); choose(items[active].id); }
  };

  const settingsVisible = can(me, current.id, 'system:write') || can(me, current.id, 'members:read');

  return (
    <div className={compact ? 'sys-switch compact' : 'sys-switch'}>
      <span className={compact ? 'sr-only' : 'eyebrow sys-switch-label'} id={`${labelId}-label`}>{t('common.system')}</span>
      <Popover.Root open={open} onOpenChange={openWith}>
        <Popover.Trigger asChild>
          <button type="button" className="sys-trigger" aria-haspopup="listbox" aria-labelledby={`${labelId}-label ${labelId}-name`}
            onKeyDown={(e) => { if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && !open) { e.preventDefault(); openWith(true); } }}>
            <SysTile name={current.name} src={current.image_url} />
            <span className="sys-trigger-text">
              <span className="sys-trigger-name truncate" id={`${labelId}-name`}>{current.name}</span>
              {!compact && <span className="xs muted truncate">{current.location || t('common.noLocation')}</span>}
            </span>
            <Icon name="down" size="sm" />
          </button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content className="sys-pop" align="start" sideOffset={compact ? 8 : 4}
            // In the mobile top bar, focusing the search would pop up the keyboard over the list.
            onOpenAutoFocus={compact ? (e) => e.preventDefault() : undefined}>
            <div className="sys-search">
              <Icon name="search" size="sm" />
              <label className="sr-only" htmlFor="sys-q">{t('switcher.search')}</label>
              <input id="sys-q" type="search" placeholder={t('switcher.search')} autoComplete="off" role="combobox" aria-expanded="true"
                aria-controls="sys-list" aria-activedescendant={items[active] ? `sysopt-${items[active].id}` : undefined}
                value={q} onChange={(e) => { setQ(e.target.value); setActive(0); }} onKeyDown={onKey} autoFocus={!compact} />
            </div>
            <ul className="sys-list" id="sys-list" role="listbox" aria-label={t('switcher.systems')}>
              {items.length ? items.map((s, i) => (
                <li key={s.id} id={`sysopt-${s.id}`} role="option" aria-selected={s.id === current.id}
                  className={i === active ? 'sys-opt active' : 'sys-opt'} onClick={() => choose(s.id)} onMouseEnter={() => setActive(i)}>
                  <SysTile name={s.name} src={s.image_url} />
                  <span className="sys-opt-text">
                    <span className="sys-opt-name truncate">{s.name}</span>
                    <span className="xs muted truncate">{s.location || t('common.noLocation')} · {num(s.peak_power_kwp, 2)} kWp</span>
                  </span>
                  {s.id === current.id && <Icon name="check" size="sm" />}
                </li>
              )) : <li className="sys-empty" role="presentation">{t('switcher.noMatch', { q: q.trim() })}</li>}
            </ul>
            {(settingsVisible || me.is_superadmin) && (
              <div className="sys-pop-foot">
                {settingsVisible && <Link className="sys-action" to={`/s/${current.id}/settings`} onClick={() => setOpen(false)}><Icon name="sliders" size="sm" />{t('switcher.settings')}</Link>}
                {me.is_superadmin && (
                  <>
                    <Link className="sys-action" to="/admin/systems" onClick={() => setOpen(false)}><Icon name="home" size="sm" />{t('nav.allSystems')}</Link>
                    <button type="button" className="sys-action" onClick={() => { setOpen(false); onCreate(); }}><Icon name="plus" size="sm" />{t('switcher.create')}</button>
                  </>
                )}
              </div>
            )}
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </div>
  );
}
