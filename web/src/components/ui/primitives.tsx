import type { CSSProperties, HTMLAttributes, ReactNode } from 'react';
import clsx from 'clsx';
import { useTranslation } from 'react-i18next';
import { Icon, type IconName } from './Icon';
import { initials } from '@/lib/format';
import { roleLabel } from '@/auth/scopes';
import type { Role } from '@/api/types';

export const Spinner = ({ className }: { className?: string }) => <span className={clsx('spinner', className)} aria-hidden="true" />;

export function Card({ flush, as: As = 'div', className, ...rest }: HTMLAttributes<HTMLElement> & { flush?: boolean; as?: 'div' | 'section' | 'article' }) {
  return <As className={clsx('card', flush && 'card-flush', className)} {...rest} />;
}

export function CardHead({ title, id, children }: { title: ReactNode; id?: string; children?: ReactNode }) {
  return (
    <div className="card-head">
      <h2 id={id}>{title}</h2>
      {children}
    </div>
  );
}

export type BadgeTone = 'neutral' | 'success' | 'warn' | 'danger' | 'accent' | 'outline';
export function Badge({ tone = 'neutral', dot, icon, children, className }: { tone?: BadgeTone; dot?: boolean; icon?: IconName; children: ReactNode; className?: string }) {
  return (
    <span className={clsx('badge', tone !== 'neutral' && `badge-${tone}`, className)}>
      {dot && <span className="badge-dot" />}
      {icon && <Icon name={icon} size="sm" />}
      {children}
    </span>
  );
}

export function RoleBadge({ role }: { role: Role }) {
  useTranslation();
  return <Badge tone={role === 'owner' ? 'accent' : role === 'admin' ? 'neutral' : 'outline'}>{roleLabel(role)}</Badge>;
}

export function Banner({ tone = 'info', icon, title, children, className, role }: { tone?: 'info' | 'warn' | 'danger'; icon?: IconName; title?: ReactNode; children?: ReactNode; className?: string; role?: 'alert' | 'status' }) {
  return (
    <div className={clsx('banner', tone, className)} role={role}>
      <Icon name={icon ?? (tone === 'info' ? 'info' : 'alert')} />
      <div className="body">
        {title && <strong>{title}</strong>}
        {children && <span>{children}</span>}
      </div>
    </div>
  );
}

export function EmptyState({ icon = 'info', title, text, action }: { icon?: IconName; title: string; text?: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty">
      <Icon name={icon} />
      <h3>{title}</h3>
      {text && <p>{text}</p>}
      {action}
    </div>
  );
}

export const Skeleton = ({ style, className }: { style?: CSSProperties; className?: string }) => <div className={clsx('skel', className)} style={style} />;

/** Profile picture, or the initials without one. */
export const Avatar = ({ name, src, className }: { name: string; src?: string; className?: string }) => src
  ? <img className={clsx('avatar', className)} src={src} alt="" aria-hidden="true" loading="lazy" decoding="async" />
  : <span className={clsx('avatar', className)} aria-hidden="true">{initials(name)}</span>;

/** A system's picture, or its initials without one (system switcher, cards). */
export const SysTile = ({ name, src, className }: { name: string; src?: string; className?: string }) => src
  ? <img className={clsx('sys-tile', className)} src={src} alt="" aria-hidden="true" loading="lazy" decoding="async" />
  : <span className={clsx('sys-tile', className)} aria-hidden="true">{initials(name)}</span>;

export const Swatch = ({ color }: { color: string }) => <span className="swatch" style={{ background: color }} />;

export function ProgressBar({ value, tone }: { value: number; tone?: 'accent' | 'ok' | 'low' }) {
  return (
    <div className={clsx('progress', tone)} role="presentation">
      <span style={{ width: `${Math.max(0, Math.min(100, value))}%` }} />
    </div>
  );
}

export function ShareBar({ pct, color, children }: { pct: number; color: string; children: ReactNode }) {
  return (
    <span className="share">
      <span className="bar" aria-hidden="true"><span style={{ width: `${Math.max(0, Math.min(100, pct))}%`, background: color }} /></span>
      {children}
    </span>
  );
}

/** Inverter status as shown in tables: producing, error, derating or off. */
export function StatusCell({ statusText, errorText }: { statusText?: string; errorText?: string }) {
  const { t } = useTranslation();
  if (errorText) return <span className="status err"><Icon name="alert" size="sm" />{statusText || t('common.error')}</span>;
  if (statusText === 'MPP') return <span className="status ok"><Icon name="dot" size="sm" />{t('statusCell.producing')}</span>;
  if (statusText?.startsWith('Derating')) return <span className="status warn"><Icon name="alert" size="sm" />{statusText}</span>;
  return <span className="status off"><Icon name="circle" size="sm" />{statusText || t('common.unknown')}</span>;
}

/** Avatar + name + email, as in member and user tables. */
export function Person({ name, email, you, avatar }: { name: string; email: string; you?: boolean; avatar?: string }) {
  const { t } = useTranslation();
  return (
    <span className="flex items-center gap-2">
      <Avatar name={name} src={avatar} />
      <span className="cell-main">
        <span className="font-medium">
          {name}
          {you && <Badge tone="outline" className="ml-1.5">{t('common.you')}</Badge>}
        </span>
        <span className="xs muted">{email}</span>
      </span>
    </span>
  );
}

/** Two-line stat: small label, then value (dashboard strips, data source meta). */
export function Stat({ label, children, className }: { label: ReactNode; children: ReactNode; className?: string }) {
  return (
    <div className={clsx('stat', className)}>
      <span className="l">{label}</span>
      <span className="v">{children}</span>
    </div>
  );
}
