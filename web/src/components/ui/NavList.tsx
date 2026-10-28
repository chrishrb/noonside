import { Children, useId, type ReactNode } from 'react';
import { Link } from 'react-router';
import { Switch as RSwitch } from 'radix-ui';
import { useTranslation } from 'react-i18next';
import { Icon } from './Icon';
import { Card } from './primitives';

/**
 * Card with a heading and a list of rows (NavRow, ToggleRow, RadioRow).
 * `section` gives a smaller heading, for cards below a page's first one.
 */
export function NavList({ title, intro, children, head, section }: { title?: ReactNode; intro?: ReactNode; children?: ReactNode; head?: ReactNode; section?: boolean }) {
  const id = useId();
  const H = section ? 'h3' : 'h2';
  const rows = Children.toArray(children).length > 0;
  return (
    <Card flush as="section" aria-labelledby={title ? id : undefined}>
      {(title || head) && (
        <div className={rows ? 'nav-list-head' : 'nav-list-head nav-list-head-only'}>
          {head}
          {title && <H id={id}>{title}</H>}
          {intro && <p className="sm muted">{intro}</p>}
        </div>
      )}
      {rows && <ul className="nav-list">{children}</ul>}
    </Card>
  );
}

/** A row that opens a sub-page: title, a summary line and an arrow. */
export function NavRow({ to, title, sub }: { to: string; title: ReactNode; sub?: ReactNode }) {
  return (
    <li>
      <Link to={to} className="nav-row">
        <span className="nav-row-text">
          <span className="nav-row-title">{title}</span>
          {sub && <span className="nav-row-sub">{sub}</span>}
        </span>
        <Icon name="right" className="nav-row-arrow" />
      </Link>
    </li>
  );
}

/** A row with a switch on the right; the whole text toggles it. */
export function ToggleRow({ title, help, checked, onCheckedChange, disabled, strong }: {
  title: ReactNode; help?: ReactNode; checked: boolean; onCheckedChange: (v: boolean) => void; disabled?: boolean; strong?: boolean;
}) {
  const id = useId();
  return (
    <li className="nav-row nav-row-static">
      <span className="nav-row-text">
        <label htmlFor={id} className={strong ? 'nav-row-title font-semibold' : 'nav-row-title'}>{title}</label>
        {help && <span className="nav-row-sub">{help}</span>}
      </span>
      <RSwitch.Root className="switch-root" id={id} checked={checked} onCheckedChange={onCheckedChange} disabled={disabled}>
        <RSwitch.Thumb className="switch-thumb" />
      </RSwitch.Root>
    </li>
  );
}

/** One option of a radio group laid out as a row. */
export function RadioRow({ name, title, help, checked, onChange, disabled }: {
  name: string; title: ReactNode; help?: ReactNode; checked: boolean; onChange: () => void; disabled?: boolean;
}) {
  return (
    <li>
      <label className="nav-row nav-row-radio">
        <span className="nav-row-text">
          <span className="nav-row-title">{title}</span>
          {help && <span className="nav-row-sub">{help}</span>}
        </span>
        <input type="radio" name={name} checked={checked} onChange={onChange} disabled={disabled} />
      </label>
    </li>
  );
}

export function BackLink({ to, children }: { to: string; children?: ReactNode }) {
  const { t } = useTranslation();
  return <Link to={to} className="back-link"><Icon name="left" />{children ?? t('common.back')}</Link>;
}
