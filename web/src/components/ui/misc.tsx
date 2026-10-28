import { useEffect, useState, type ReactNode } from 'react';
import { Button } from './Button';
import { Icon } from './Icon';
import { Field } from './form';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { toast, toastError } from './toast';

export async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast(i18n.t('common.copied'));
  } catch {
    toastError(new Error(i18n.t('common.copyFailed')));
  }
}

export function CopyButton({ text, label, size }: { text: string; label?: string; size?: 'sm' | 'md' }) {
  const { t } = useTranslation();
  return <Button icon="copy" size={size} className="flex-none" onClick={() => copyText(text)}>{label ?? t('common.copy')}</Button>;
}

/** A value shown once, with a copy button (API keys, invitation links). */
export function SecretReveal({ label, value, help }: { label: string; value: string; help?: ReactNode }) {
  return (
    <Field label={label} help={help}>
      <div className="secret">
        <code>{value}</code>
        <CopyButton text={value} />
      </div>
    </Field>
  );
}

/** A <details> disclosure with the design's chevron summary. */
export function Disclosure({ summary, children, open, onToggle }: { summary: ReactNode; children: ReactNode; open?: boolean; onToggle?: (open: boolean) => void }) {
  return (
    <details className="disclosure" open={open} onToggle={(e) => onToggle?.((e.currentTarget as HTMLDetailsElement).open)}>
      <summary><Icon name="down" size="sm" />{summary}</summary>
      {children}
    </details>
  );
}

/** Re-renders every `ms` so relative times ("3 min ago") stay current. */
export function useTick(ms = 30_000) {
  const [, set] = useState(0);
  useEffect(() => {
    const t = setInterval(() => set((n) => n + 1), ms);
    return () => clearInterval(t);
  }, [ms]);
}
