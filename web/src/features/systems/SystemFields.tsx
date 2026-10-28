import { z } from 'zod';
import { useWatch, type UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { Checkbox, Field, FormGrid, FormSection, FormSections, Input, Select } from '@/components/ui/form';
import { todayIn } from '@/lib/format';
import { numOrUndef } from '@/lib/forms';
import type { System, SystemCreate } from '@/api/types';
import { AddressField } from './AddressField';
import { MAX_PLANES, PlanesField } from './PlanesField';

const TZ = ['Europe/Berlin', 'Europe/Vienna', 'Europe/Zurich', 'Europe/Amsterdam', 'Europe/Brussels', 'Europe/Paris', 'Europe/London', 'Europe/Madrid', 'Europe/Rome', 'Europe/Warsaw', 'UTC'];

type FieldKey = 'peak' | 'latitude' | 'longitude' | 'tilt' | 'azimuth' | 'tariff' | 'co2' | 'retention';
// Messages are functions so they are rendered in the language active at validation time.
const optNum = (min: number | undefined, max: number | undefined, field: FieldKey) => {
  const label = () => i18n.t(`system.fields.${field}`);
  let n = z.number({ error: () => i18n.t('validation.number', { label: label() }) });
  if (min != null) n = n.min(min, { error: () => i18n.t('validation.min', { label: label(), min }) });
  if (max != null) n = n.max(max, { error: () => i18n.t('validation.max', { label: label(), max }) });
  return z.preprocess(numOrUndef, n.optional());
};
const reqNum = (min: number, max: number | undefined, field: FieldKey) => {
  const label = () => i18n.t(`system.fields.${field}`);
  let n = z.number({ error: () => i18n.t('validation.required', { label: label() }) })
    .min(min, { error: () => i18n.t('validation.min', { label: label(), min }) });
  if (max != null) n = n.max(max, { error: () => i18n.t('validation.max', { label: label(), max }) });
  return z.preprocess(numOrUndef, n);
};

const planeSchema = z.object({
  // Kept (not shown) so saving keeps the plane and its inverters.
  id: z.string().optional(),
  name: z.string().trim().max(100, 'validation.max100'),
  kwp: reqNum(0, undefined, 'peak'),
  tilt_deg: reqNum(0, 90, 'tilt'),
  azimuth_deg: reqNum(-180, 180, 'azimuth'),
});

export const systemSchema = z.object({
  name: z.string().trim().min(1, 'validation.nameRequired').max(200, 'validation.max200'),
  location: z.string().trim(),
  timezone: z.string(),
  commissioned_at: z.string(),
  peak_power_kwp: optNum(0, undefined, 'peak'),
  orientation: z.string().trim(),
  operator: z.string().trim(),
  module_info: z.string().trim(),
  // Not shown: set from the picked address suggestion (empty: the server looks the address up).
  latitude: optNum(-90, 90, 'latitude'),
  longitude: optNum(-180, 180, 'longitude'),
  planes: z.array(planeSchema).max(MAX_PLANES),
  tariff_per_kwh: optNum(0, undefined, 'tariff'),
  currency: z.string().trim().regex(/^[A-Za-z]{3}$/, 'validation.currency'),
  co2_kg_per_kwh: optNum(0, undefined, 'co2'),
  price_zone: z.enum(['', 'DE-LU', 'AT']),
  unpaid_at_negative_prices: z.boolean(),
  minute_retention_years: optNum(0, 100, 'retention').refine((n) => n == null || Number.isInteger(n), 'validation.wholeYears'),
});
export type SystemForm = z.input<typeof systemSchema>;
export type SystemValues = z.output<typeof systemSchema>;

export function systemDefaults(s?: System): SystemForm {
  return {
    name: s?.name ?? '',
    location: s?.location ?? '',
    timezone: s?.timezone ?? 'Europe/Berlin',
    commissioned_at: s?.commissioned_at ?? '',
    peak_power_kwp: s?.peak_power_kwp ?? '',
    orientation: s?.orientation ?? '',
    operator: s?.operator ?? '',
    module_info: s?.module_info ?? '',
    latitude: s?.latitude ?? '',
    longitude: s?.longitude ?? '',
    planes: (s?.planes ?? []).map((p) => ({ id: p.id, name: p.name ?? '', kwp: p.kwp, tilt_deg: p.tilt_deg, azimuth_deg: p.azimuth_deg })),
    tariff_per_kwh: s?.tariff_per_kwh ?? '',
    currency: s?.currency ?? 'EUR',
    co2_kg_per_kwh: s?.co2_kg_per_kwh ?? 0.38,
    price_zone: s?.price_zone ?? '',
    unpaid_at_negative_prices: s?.unpaid_at_negative_prices ?? false,
    minute_retention_years: s?.minute_retention_years ?? '',
  };
}

/** Request body for create/update; empty optional fields are left out. */
export function systemBody(v: SystemValues): SystemCreate {
  // Empty retention is sent as null, which clears it (keep everything). The
  // planes replace the saved ones (none: the weather assumes flat panels).
  const body: SystemCreate = {
    ...v, currency: v.currency.toUpperCase(), minute_retention_years: v.minute_retention_years ?? null,
    price_zone: v.price_zone || null,
    planes: v.planes.map((p) => ({ ...p, name: p.name || undefined })),
  };
  if (!v.commissioned_at) delete body.commissioned_at;
  return body;
}

/** Server messages ("latitude must be …") → form field; coordinates belong to the address. */
export const SYSTEM_ERRORS = {
  name: 'name', location: 'location', latitude: 'location', longitude: 'location', peak_power_kwp: 'peak_power_kwp', tariff_per_kwh: 'tariff_per_kwh',
  currency: 'currency', co2_kg_per_kwh: 'co2_kg_per_kwh', commissioned_at: 'commissioned_at', timezone: 'timezone',
  minute_retention_years: 'minute_retention_years', price_zone: 'price_zone',
} as const;

/** Commissioning date from which § 51 EEG (Solarspitzengesetz) pays nothing at negative prices. */
export const SOLARSPITZEN_FROM = '2025-02-25';

/** First day of the oldest year whose minute data is kept, e.g. "2024-01-01". */
export function retentionCutoff(years: number, timezone: string): string {
  return `${Number(todayIn(timezone).slice(0, 4)) - years}-01-01`;
}

export function SystemFields({ form, timezone }: { form: UseFormReturn<SystemForm, unknown, SystemValues>; timezone?: string }) {
  const { t } = useTranslation();
  const { register, formState: { errors } } = form;
  const tz = !timezone || TZ.includes(timezone) ? TZ : [timezone, ...TZ];
  const e = (k: keyof SystemForm) => errors[k]?.message as string | undefined;
  const [commissioned, unpaid] = useWatch({ control: form.control, name: ['commissioned_at', 'unpaid_at_negative_prices'] });
  const suggestUnpaid = !unpaid && !!commissioned && commissioned >= SOLARSPITZEN_FROM;
  const zones = [
    { value: '', label: t('system.priceZone.none') },
    { value: 'DE-LU', label: t('system.priceZone.deLu') },
    { value: 'AT', label: t('system.priceZone.at') },
  ];
  return (
    <FormSections>
      <FormSection title={t('system.basics')} description={t('system.basicsText')}>
        <FormGrid>
          <Field label={t('common.name')} required span2 error={e('name')}><Input maxLength={200} {...register('name')} /></Field>
          <AddressField form={form} />
          <Field label={t('system.timezone')} help={t('system.timezoneHelp')} error={e('timezone')}><Select options={tz} {...register('timezone')} /></Field>
          <Field label={t('system.commissioned')} optional error={e('commissioned_at')}><Input type="date" max={todayIn()} {...register('commissioned_at')} /></Field>
          <Field label={t('system.fields.peak')} error={e('peak_power_kwp')}><Input type="number" min={0} step={0.01} addon="kWp" {...register('peak_power_kwp')} /></Field>
        </FormGrid>
      </FormSection>
      <FormSection title={t('system.plant')} description={t('system.plantText')}>
        <FormGrid>
          <Field label={t('system.orientation')} optional><Input placeholder={t('system.orientationPlaceholder')} {...register('orientation')} /></Field>
          <Field label={t('system.operator')} optional><Input {...register('operator')} /></Field>
          <Field label={t('system.modules')} optional span2><Input placeholder={t('system.modulesPlaceholder')} {...register('module_info')} /></Field>
          <PlanesField form={form} />
        </FormGrid>
      </FormSection>
      <FormSection title={t('system.economics')} description={t('system.economicsText')}>
        <FormGrid>
          <Field label={t('system.fields.tariff')} error={e('tariff_per_kwh')}><Input type="number" min={0} step="any" addon={t('system.perKwh')} {...register('tariff_per_kwh')} /></Field>
          <Field label={t('system.currency')} error={e('currency')}><Input maxLength={3} {...register('currency')} /></Field>
          <Field label={t('system.fields.co2')} error={e('co2_kg_per_kwh')}><Input type="number" min={0} step="any" addon="kg/kWh" {...register('co2_kg_per_kwh')} /></Field>
          <Field label={t('system.priceZone.label')} optional help={t('system.priceZone.help')} error={e('price_zone')}><Select options={zones} {...register('price_zone')} /></Field>
          <div className="field span-2">
            <Checkbox label={t('system.unpaidNegative')} help={suggestUnpaid ? t('system.unpaidNegativeSuggest') : t('system.unpaidNegativeHelp')} {...register('unpaid_at_negative_prices')} />
          </div>
        </FormGrid>
      </FormSection>
      <FormSection title={t('system.retention')} description={t('system.retentionText')}>
        <FormGrid>
          <Field label={t('system.keepCurves')} optional error={e('minute_retention_years')} help={t('system.keepCurvesHelp')}>
            <Input type="number" min={0} max={100} step={1} addon={t('system.yearsAddon')} placeholder={t('system.all')} {...register('minute_retention_years')} />
          </Field>
        </FormGrid>
      </FormSection>
    </FormSections>
  );
}
