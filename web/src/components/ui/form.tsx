import {
  createContext, forwardRef, useContext, useId, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from 'react';
import { Switch as RSwitch } from 'radix-ui';
import clsx from 'clsx';
import { useTranslation } from 'react-i18next';
import { translateMaybe } from '@/i18n';
import { Icon, type IconName } from './Icon';

// Field wires label, help and error text to the control inside it.
interface FieldCtx { id: string; describedBy?: string; invalid: boolean; required?: boolean }
const FieldContext = createContext<FieldCtx | null>(null);

function useControlProps(id?: string) {
  const f = useContext(FieldContext);
  return {
    id: id ?? f?.id,
    'aria-describedby': f?.describedBy,
    'aria-invalid': f?.invalid ? true : undefined,
    'aria-required': f?.required || undefined,
  };
}

export interface FieldProps {
  label: ReactNode;
  help?: ReactNode;
  error?: string;
  required?: boolean;
  optional?: boolean;
  span2?: boolean;
  /** Hide the label visually (still read by screen readers). */
  srLabel?: boolean;
  id?: string;
  className?: string;
  children: ReactNode;
}

export function Field({ label, help, error, required, optional, span2, srLabel, id, className, children }: FieldProps) {
  const { t } = useTranslation();
  const auto = useId();
  const fid = id ?? auto;
  const describedBy = [help ? `${fid}-help` : '', error ? `${fid}-err` : ''].filter(Boolean).join(' ') || undefined;
  return (
    <FieldContext.Provider value={{ id: fid, describedBy, invalid: !!error, required }}>
      <div className={clsx('field', span2 && 'span-2', className)}>
        <label htmlFor={fid} className={clsx(srLabel && 'sr-only')}>
          {label}
          {required ? <span className="req" aria-hidden="true">*</span> : optional ? <span className="opt"> {t('common.optional')}</span> : null}
        </label>
        {children}
        {help && <p className="help" id={`${fid}-help`}>{help}</p>}
        {error && <FieldError id={`${fid}-err`}>{translateMaybe(error)}</FieldError>}
      </div>
    </FieldContext.Provider>
  );
}

export const FieldError = ({ id, children }: { id?: string; children: ReactNode }) => (
  <p className="error-text" id={id}><Icon name="alert" size="sm" /><span>{children}</span></p>
);

type InputProps = InputHTMLAttributes<HTMLInputElement> & { addon?: string; inputSize?: 'md' | 'sm' };
export const Input = forwardRef<HTMLInputElement, InputProps>(function Input({ addon, inputSize, className, id, ...rest }, ref) {
  const ctl = useControlProps(id);
  const input = <input ref={ref} className={clsx('input', inputSize === 'sm' && 'input-sm', className)} {...ctl} {...rest} />;
  return addon ? <div className="input-group">{input}<span className="addon">{addon}</span></div> : input;
});

export interface Option { value: string; label: string; disabled?: boolean }
type SelectProps = SelectHTMLAttributes<HTMLSelectElement> & { options: (Option | string)[]; selectSize?: 'md' | 'sm' };
export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select({ options, selectSize, className, id, ...rest }, ref) {
  const ctl = useControlProps(id);
  return (
    <select ref={ref} className={clsx('select', selectSize === 'sm' && 'select-sm', className)} {...ctl} {...rest}>
      {options.map((o) => {
        const opt = typeof o === 'string' ? { value: o, label: o } : o;
        return <option key={opt.value} value={opt.value} disabled={opt.disabled}>{opt.label}</option>;
      })}
    </select>
  );
});

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(function Textarea({ className, id, ...rest }, ref) {
  const ctl = useControlProps(id);
  return <textarea ref={ref} className={clsx('textarea', className)} {...ctl} {...rest} />;
});

/** Checkbox with an optional second line of help. */
export const Checkbox = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement> & { label: ReactNode; help?: ReactNode }>(
  function Checkbox({ label, help, className, ...rest }, ref) {
    return (
      <label className={clsx('check', className)}>
        <input ref={ref} type="checkbox" {...rest} />
        {help ? <span className="text"><span>{label}</span><span className="help">{help}</span></span> : <span>{label}</span>}
      </label>
    );
  },
);

export function Switch({ checked, onCheckedChange, label, disabled, id, name }: {
  checked: boolean; onCheckedChange: (v: boolean) => void; label: ReactNode; disabled?: boolean; id?: string; name?: string;
}) {
  const auto = useId();
  const sid = id ?? auto;
  return (
    <span className="switch">
      <RSwitch.Root className="switch-root" id={sid} name={name} checked={checked} onCheckedChange={onCheckedChange} disabled={disabled}>
        <RSwitch.Thumb className="switch-thumb" />
      </RSwitch.Root>
      <label htmlFor={sid} className="cursor-pointer">{label}</label>
    </span>
  );
}

export const FormGrid = ({ children, className }: { children: ReactNode; className?: string }) => <div className={clsx('form-grid', className)}>{children}</div>;

export const Fieldset = ({ legend, children }: { legend: string; children: ReactNode }) => (
  <fieldset className="fieldset"><legend>{legend}</legend>{children}</fieldset>
);

/** Groups FormSections; they sit beside their fields once it is at least 720px wide, and stack below that. */
export const FormSections = ({ children }: { children: ReactNode }) => <div className="form-sections">{children}</div>;

/** Titled group of fields with a short description, inside FormSections. */
export function FormSection({ title, description, children }: { title: string; description?: ReactNode; children: ReactNode }) {
  const id = useId();
  return (
    <div className="form-section" role="group" aria-labelledby={id}>
      <div className="form-section-head">
        <h2 id={id}>{title}</h2>
        {description && <p className="help">{description}</p>}
      </div>
      <div className="form-section-body">{children}</div>
    </div>
  );
}

/** An icon, or an emoji used as the mark (pink mode's heart). */
export type SegmentMark = IconName | { emoji: string };

function SegmentGlyph({ mark }: { mark?: SegmentMark }) {
  if (!mark) return null;
  if (typeof mark === 'object') return <span className="seg-emoji" aria-hidden="true">{mark.emoji}</span>;
  return <Icon name={mark} size="sm" />;
}

/** Segmented control as a radio group. */
export function Segmented<T extends string>({ value, onChange, options, label }: {
  value: T; onChange: (v: T) => void; options: [T, string, SegmentMark?][]; label: string;
}) {
  return (
    <div className="seg" role="radiogroup" aria-label={label}>
      {options.map(([v, l, mark]) => (
        <button key={v} type="button" role="radio" aria-checked={v === value} onClick={() => onChange(v)}><SegmentGlyph mark={mark} />{l}</button>
      ))}
    </div>
  );
}

/** Toggle chip (inverter filter, export presets). */
export function Chip({ pressed, onClick, color, children }: { pressed: boolean; onClick: () => void; color?: string; children: ReactNode }) {
  return (
    <button type="button" className="chip" aria-pressed={pressed} onClick={onClick}>
      {color && <span className="swatch" style={{ background: color }} />}
      {children}
    </button>
  );
}
