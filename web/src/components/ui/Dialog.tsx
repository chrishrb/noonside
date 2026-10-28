import type { FormEvent, ReactNode } from 'react';
import { Dialog as RDialog } from 'radix-ui';
import clsx from 'clsx';
import { useTranslation } from 'react-i18next';
import { Button } from './Button';
import { Banner } from './primitives';
import type { ServerError } from '@/lib/forms';

export interface FormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  subtitle?: ReactNode;
  wide?: boolean;
  submitLabel?: string;
  cancelLabel?: string;
  hideCancel?: boolean;
  danger?: boolean;
  busy?: boolean;
  /** Error from the server that isn't tied to a field. */
  serverError?: ServerError | null;
  onSubmit?: (e: FormEvent<HTMLFormElement>) => void;
  children?: ReactNode;
}

/** Modal with a form: header, scrolling body, footer with cancel/submit. */
export function FormDialog({
  open, onOpenChange, title, subtitle, wide, submitLabel, cancelLabel, hideCancel, danger, busy, serverError, onSubmit, children,
}: FormDialogProps) {
  const { t } = useTranslation();
  return (
    <RDialog.Root open={open} onOpenChange={(o) => { if (!busy) onOpenChange(o); }}>
      <RDialog.Portal>
        <RDialog.Overlay className="dialog-overlay" />
        <RDialog.Content className={clsx('dialog', wide && 'wide')} aria-describedby={undefined}>
          <form
            noValidate
            onSubmit={(e) => {
              e.preventDefault();
              if (onSubmit) onSubmit(e);
              else onOpenChange(false);
            }}
          >
            <div className="dialog-head">
              <div className="titles">
                <RDialog.Title asChild><h2>{title}</h2></RDialog.Title>
                {subtitle && <p className="sm muted">{subtitle}</p>}
              </div>
              <RDialog.Close asChild>
                <Button variant="ghost" size="sm" iconOnly icon="x" label={t('common.close')} />
              </RDialog.Close>
            </div>
            <div className="dialog-body">
              {serverError && <Banner tone="danger" role="alert" title={serverError.title}>{serverError.message}</Banner>}
              {children}
            </div>
            <div className="dialog-foot">
              {!hideCancel && <Button onClick={() => onOpenChange(false)} disabled={busy}>{cancelLabel ?? t('common.cancel')}</Button>}
              {submitLabel && <Button type="submit" variant={danger ? 'danger' : 'primary'} busy={busy}>{submitLabel}</Button>}
            </div>
          </form>
        </RDialog.Content>
      </RDialog.Portal>
    </RDialog.Root>
  );
}
