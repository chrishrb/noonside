import { useFieldArray, useWatch, type UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Field, Input } from '@/components/ui/form';
import { fmtKwp } from '@/lib/format';
import type { SystemForm, SystemValues } from './SystemFields';

export const MAX_PLANES = 10;

/** Module fields (one per roof side) for the weather model and PVGIS targets. */
export function PlanesField({ form }: { form: UseFormReturn<SystemForm, unknown, SystemValues> }) {
  const { t } = useTranslation();
  const { control, register, formState: { errors } } = form;
  const { fields, append, remove } = useFieldArray({ control, name: 'planes' });
  const planes = useWatch({ control, name: 'planes' }) ?? [];
  const peak = Number(useWatch({ control, name: 'peak_power_kwp' })) || 0;
  const sum = planes.reduce((a, p) => a + (Number(p?.kwp) || 0), 0);
  const e = (i: number, k: 'name' | 'kwp' | 'tilt_deg' | 'azimuth_deg') => errors.planes?.[i]?.[k]?.message as string | undefined;

  return (
    <div className="span-2 grid gap-3" role="group" aria-labelledby="planes-h">
      <div className="grid gap-1">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <span className="label" id="planes-h">{t('system.planes.title')}</span>
          {fields.length > 0 && peak > 0 && Math.abs(sum - peak) > 0.005 && (
            <span className="xs muted">{t('system.planes.sum', { sum: fmtKwp(sum), peak: fmtKwp(peak) })}</span>
          )}
        </div>
        <p className="help">{fields.length ? t('system.planes.help') : t('system.planes.none')}</p>
      </div>
      {fields.map((f, i) => (
        <div key={f.id} className="plane-row">
          <Field label={t('system.planes.name')} optional error={e(i, 'name')}>
            <Input maxLength={100} placeholder={t('system.planes.namePlaceholder')} {...register(`planes.${i}.name`)} />
          </Field>
          <Field label={t('system.fields.peak')} error={e(i, 'kwp')}>
            <Input type="number" min={0} step="any" addon="kWp" {...register(`planes.${i}.kwp`)} />
          </Field>
          <Field label={t('system.fields.tilt')} error={e(i, 'tilt_deg')} help={i === 0 ? t('system.tiltHelp') : undefined}>
            <Input type="number" min={0} max={90} step="any" addon="°" {...register(`planes.${i}.tilt_deg`)} />
          </Field>
          <Field label={t('system.fields.azimuth')} error={e(i, 'azimuth_deg')} help={i === 0 ? t('system.azimuthHelp') : undefined}>
            <Input type="number" min={-180} max={180} step="any" addon="°" {...register(`planes.${i}.azimuth_deg`)} />
          </Field>
          <Button className="plane-remove" variant="ghost" iconOnly icon="trash" label={t('system.planes.remove', { n: i + 1 })} onClick={() => remove(i)} />
        </div>
      ))}
      {fields.length < MAX_PLANES && (
        <div>
          <Button size="sm" icon="plus" onClick={() => append({ name: '', kwp: fields.length ? '' : peak || '', tilt_deg: 30, azimuth_deg: 0 })}>
            {t('system.planes.add')}
          </Button>
        </div>
      )}
    </div>
  );
}
