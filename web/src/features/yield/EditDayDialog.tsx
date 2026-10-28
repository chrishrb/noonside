import { useMutation, useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { getDayEnergyOptions, setDayEnergyMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { DayEnergyInverter, DayEnergyUpdateItem } from '@/api/types';
import { Button } from '@/components/ui/Button';
import { FormDialog } from '@/components/ui/Dialog';
import { Field, Input } from '@/components/ui/form';
import { Skeleton } from '@/components/ui/primitives';
import { toast } from '@/components/ui/toast';
import { fmtDate, fmtKWh } from '@/lib/format';
import { applyServerError, type ServerError } from '@/lib/forms';
import { currentLocale } from '@/i18n';

type Values = Record<string, string>;

/**
 * The inverters whose correction changed: a number sets it, an empty field
 * removes it (the device's value applies again). Invalid numbers are left
 * to the form's validation.
 */
export function dayEnergyChanges(rows: DayEnergyInverter[], values: Values): DayEnergyUpdateItem[] {
  const out: DayEnergyUpdateItem[] = [];
  for (const r of rows) {
    const raw = (values[r.inverter_id] ?? '').trim().replace(',', '.');
    const next = raw === '' ? null : Number(raw);
    if (next != null && !(Number.isFinite(next) && next >= 0)) continue;
    if (next !== r.manual_kwh) out.push({ inverter_id: r.inverter_id, energy_kwh: next });
  }
  return out;
}

/** A kWh value for the input, in the UI's locale without grouping (parsed back by dayEnergyChanges). */
const inputKWh = (v: number) => new Intl.NumberFormat(currentLocale(), { maximumFractionDigits: 3, useGrouping: false }).format(v);

const valuesOf = (rows: DayEnergyInverter[]): Values =>
  Object.fromEntries(rows.map((r) => [r.inverter_id, r.manual_kwh == null ? '' : inputKWh(r.manual_kwh)]));

/** Corrects the daily energy of a system's inverters on one day. */
export function EditDayDialog({ sid, date, names, open, onOpenChange }: {
  sid: string; date: string; names: Map<string, string>; open: boolean; onOpenChange: (o: boolean) => void;
}) {
  const { t } = useTranslation();
  const q = useQuery({ ...getDayEnergyOptions({ path: { systemId: sid, day: date } }), enabled: open });
  const save = useMutation(setDayEnergyMutation());
  const invalidate = useInvalidate();
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const form = useForm<Values>({ defaultValues: {} });
  const rows = q.data?.inverters;
  const values = useWatch({ control: form.control });

  useEffect(() => { if (open && rows) form.reset(valuesOf(rows)); }, [open, rows, form]);

  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) setServerError(null);
  };

  return (
    <FormDialog open={open} onOpenChange={close} title={t('yield.correct.title')} subtitle={t('yield.correct.subtitle', { date: fmtDate(date) })}
      submitLabel={t('common.saveChanges')} busy={save.isPending} serverError={serverError}
      onSubmit={form.handleSubmit(async (values) => {
        setServerError(null);
        const changes = rows ? dayEnergyChanges(rows, values) : [];
        if (!changes.length) { close(false); return; }
        try {
          await save.mutateAsync({ path: { systemId: sid, day: date }, body: { inverters: changes } });
          await invalidate('getDayEnergy', 'getOverview', 'getDailyEnergy', 'getDashboard', 'getLive');
          toast(t('yield.correct.saved'));
          close(false);
        } catch (err) { setServerError(applyServerError(err, form.setError)); }
      })}>
      <p className="sm muted">{t('yield.correct.note')}</p>
      {!rows ? <Skeleton style={{ height: 120 }} /> : rows.map((r) => {
        const name = names.get(r.inverter_id) ?? t('yield.inverters.inverter');
        const value = values[r.inverter_id] ?? '';
        const device = r.device_kwh == null ? t('yield.correct.noDevice') : t('yield.correct.device', { value: fmtKWh(r.device_kwh) });
        return (
          <Field key={r.inverter_id} label={name} error={form.formState.errors[r.inverter_id]?.message}
            help={<span className="flex flex-wrap items-center gap-x-2">
              <span>{device}</span>
              {value !== '' && (
                <Button variant="ghost" size="sm" onClick={() => form.setValue(r.inverter_id, '', { shouldDirty: true })}>
                  {t('yield.correct.useDevice')}
                </Button>
              )}
            </span>}>
            <Input type="text" inputMode="decimal" autoComplete="off" addon="kWh"
              placeholder={r.device_kwh == null ? '' : inputKWh(r.device_kwh)}
              {...form.register(r.inverter_id, {
                validate: (v) => {
                  const s = (v ?? '').trim().replace(',', '.');
                  return s === '' || (Number.isFinite(Number(s)) && Number(s) >= 0) || 'yield.correct.nonNegative';
                },
              })} />
          </Field>
        );
      })}
    </FormDialog>
  );
}
