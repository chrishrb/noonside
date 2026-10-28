import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { Link, type LinkProps } from 'react-router';
import clsx from 'clsx';
import { useTranslation } from 'react-i18next';
import { Icon, type IconName } from './Icon';
import { Spinner } from './primitives';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-ghost';

interface Common {
  variant?: ButtonVariant;
  size?: 'md' | 'sm';
  icon?: IconName;
  /** Icon-only button; `label` becomes the accessible name. */
  iconOnly?: boolean;
  label?: string;
}

export const btnClass = (variant: ButtonVariant = 'secondary', size: 'md' | 'sm' = 'md', iconOnly = false, className?: string) =>
  clsx('btn', `btn-${variant}`, size === 'sm' && 'btn-sm', iconOnly && 'btn-icon', className);

type ButtonProps = Common & ButtonHTMLAttributes<HTMLButtonElement> & { busy?: boolean };

/** Button with the design's variants. `busy` shows a spinner and blocks clicks. */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', icon, iconOnly, label, busy, className, children, disabled, type = 'button', onClick, ...rest },
  ref,
) {
  const { t } = useTranslation();
  return (
    <button
      ref={ref}
      type={type}
      className={btnClass(variant, size, iconOnly, clsx('relative', className))}
      aria-label={iconOnly ? label : rest['aria-label']}
      title={iconOnly ? label : rest.title}
      aria-busy={busy || undefined}
      aria-disabled={disabled || busy || undefined}
      onClick={(e) => {
        if (disabled || busy) { e.preventDefault(); return; }
        onClick?.(e);
      }}
      {...rest}
    >
      {busy && (
        <span className="absolute inset-0 grid place-items-center">
          <Spinner />
          <span className="sr-only">{t('common.working')}</span>
        </span>
      )}
      <span className={clsx('contents', busy && '[&>*]:invisible')}>
        {icon && <Icon name={icon} size="sm" />}
        {children && <span className={clsx(busy && 'invisible')}>{children}</span>}
      </span>
    </button>
  );
});

/** A router link styled as a button. */
export function ButtonLink({ variant = 'secondary', size = 'md', icon, iconOnly, label, className, children, ...rest }: Common & LinkProps & { children?: ReactNode }) {
  return (
    <Link className={btnClass(variant, size, iconOnly, className)} aria-label={iconOnly ? label : undefined} title={iconOnly ? label : undefined} {...rest}>
      {icon && <Icon name={icon} size="sm" />}
      {children}
    </Link>
  );
}
