import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { useBlocker } from 'react-router';
import { useTranslation } from 'react-i18next';
import { FormDialog } from './Dialog';
import { Field, Input } from './form';
import { serverErrorOf, type ServerError } from '@/lib/forms';

export interface ConfirmOptions {
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
  /** Require typing this text (e.g. the system name) before confirming. */
  typeToConfirm?: string;
  /** Runs before closing; a thrown error is shown in the dialog. */
  onConfirm?: () => Promise<unknown> | unknown;
}

type ConfirmFn = (o: ConfirmOptions) => Promise<boolean>;
const ConfirmContext = createContext<ConfirmFn | null>(null);

/** Provides `useConfirm()`, an awaitable confirmation dialog. */
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const [opts, setOpts] = useState<ConfirmOptions | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [typed, setTyped] = useState('');
  const [typedError, setTypedError] = useState<string>();
  const [error, setError] = useState<ServerError | null>(null);
  const resolver = useRef<(v: boolean) => void>(undefined);

  const confirm = useCallback<ConfirmFn>((o) => {
    setOpts(o); setTyped(''); setTypedError(undefined); setError(null); setOpen(true);
    return new Promise<boolean>((resolve) => { resolver.current = resolve; });
  }, []);

  const finish = (v: boolean) => {
    setOpen(false);
    resolver.current?.(v);
    resolver.current = undefined;
  };

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {opts && (
        <FormDialog
          open={open}
          onOpenChange={(o) => { if (!o) finish(false); }}
          title={opts.title}
          submitLabel={opts.confirmLabel ?? t('common.confirm')}
          danger={opts.danger}
          busy={busy}
          serverError={error}
          onSubmit={async () => {
            if (opts.typeToConfirm && typed.trim() !== opts.typeToConfirm) {
              setTypedError(t('confirm.typeExactly', { text: opts.typeToConfirm }));
              return;
            }
            setError(null);
            if (opts.onConfirm) {
              setBusy(true);
              try { await opts.onConfirm(); } catch (e) { setError(serverErrorOf(e)); return; } finally { setBusy(false); }
            }
            finish(true);
          }}
        >
          <p>{opts.message}</p>
          {opts.typeToConfirm && (
            <Field label={t('confirm.typeToConfirm', { text: opts.typeToConfirm })} required error={typedError}>
              <Input autoComplete="off" value={typed} onChange={(e) => { setTyped(e.target.value); setTypedError(undefined); }} autoFocus />
            </Field>
          )}
        </FormDialog>
      )}
    </ConfirmContext.Provider>
  );
}

export function useConfirm(): ConfirmFn {
  const c = useContext(ConfirmContext);
  if (!c) throw new Error('useConfirm outside ConfirmProvider');
  return c;
}

/**
 * Asks before leaving the page (in-app navigation, reload, closing the tab)
 * while `dirty` is set, so unsaved form values aren't dropped silently.
 */
export function useUnsavedChanges(dirty: boolean) {
  const { t } = useTranslation();
  const confirm = useConfirm();
  const blocker = useBlocker(({ currentLocation, nextLocation }) => dirty && currentLocation.pathname !== nextLocation.pathname);

  useEffect(() => {
    if (blocker.state !== 'blocked') return;
    void confirm({
      title: t('confirm.discardTitle'), danger: true, confirmLabel: t('confirm.discardLabel'),
      message: t('confirm.discardMessage'),
    }).then((ok) => (ok ? blocker.proceed() : blocker.reset()));
  }, [blocker, confirm, t]); // react-router hands out a new blocker object on each state change

  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);
}
