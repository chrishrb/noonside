import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { createInverterMutation, deleteInverterMutation, updateInverterMutation } from '@/api/gen/@tanstack/react-query.gen';
import { updateInverter } from '@/api/gen/sdk.gen';
import { useInvalidate, useInverters } from '@/api/queries';
import type { Inverter, System } from '@/api/types';
import { SectionHead } from '@/components/layout/Page';
import { Button } from '@/components/ui/Button';
import { useConfirm } from '@/components/ui/Confirm';
import { FormDialog } from '@/components/ui/Dialog';
import { Field, FormGrid, Input, Select } from '@/components/ui/form';
import { Banner, Card, EmptyState, Skeleton, Swatch } from '@/components/ui/primitives';
import { Actions, ActionsTh, Table } from '@/components/ui/Table';
import { toast, toastError } from '@/components/ui/toast';
import { fmtKwp } from '@/lib/format';
import { applyServerError, numOrUndef, type ServerError } from '@/lib/forms';
import { planeName } from '@/lib/planes';

export const INVERTER_COLORS = ['#2F6FEB', '#12A594', '#D97706', '#C2410C', '#0E7490', '#4D7C0F'];

const invalidateIds = ['listInverters', 'getOverview', 'getLive', 'getDashboard'] as const;

export function InvertersTab({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const q = useInverters(sys.id);
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const del = useMutation(deleteInverterMutation());
  const [editing, setEditing] = useState<Inverter | 'new' | null>(null);
  const [moving, setMoving] = useState<string | null>(null);

  if (q.isError) return <Banner tone="danger" title={t('inverters.loadFailed')}>{q.error.message}</Banner>;
  if (!q.data) return <Skeleton style={{ height: 240 }} />;
  const invs = [...q.data].sort((a, b) => a.sort_order - b.sort_order);
  const kwp = invs.reduce((a, i) => a + i.peak_power_kwp, 0);
  const planeNames = new Map(sys.planes.map((p, k) => [p.id ?? '', planeName(p, k, (n) => t('system.planes.fallback', { n }))]));
  const hasPlanes = sys.planes.length > 0;

  // Swap two neighbours and renumber everything 1..n.
  const move = async (k: number, dir: -1 | 1) => {
    const order = [...invs];
    [order[k], order[k + dir]] = [order[k + dir], order[k]];
    setMoving(invs[k].id);
    try {
      await Promise.all(order.map((inv, n) => inv.sort_order === n + 1 ? null : updateInverter({
        path: { systemId: sys.id, inverterId: inv.id }, body: { sort_order: n + 1 },
      })));
      await invalidate(...invalidateIds);
    } catch (e) { toastError(e); } finally { setMoving(null); }
  };

  const onDelete = async (inv: Inverter) => {
    const ok = await confirm({ title: t('inverters.deleteConfirm.title', { name: inv.name }), danger: true, confirmLabel: t('inverters.deleteConfirm.label'),
      message: t('inverters.deleteConfirm.message'),
      onConfirm: () => del.mutateAsync({ path: { systemId: sys.id, inverterId: inv.id } }) });
    if (ok) { toast(t('inverters.deleted', { name: inv.name })); await invalidate(...invalidateIds); }
  };

  return (
    <>
      <SectionHead title={t('inverters.title')} sub={t('inverters.sub', { count: t('counts.inverter', { count: invs.length }), kwp: fmtKwp(kwp), total: fmtKwp(sys.peak_power_kwp) })}>
        <Button variant="primary" icon="plus" onClick={() => setEditing('new')}>{t('inverters.add')}</Button>
      </SectionHead>
      {invs.length > 0 && Math.abs(kwp - sys.peak_power_kwp) > 0.05 && (
        <Banner tone="warn" className="mb-4">
          {t('inverters.mismatch', { kwp: fmtKwp(kwp), total: fmtKwp(sys.peak_power_kwp) })}
        </Banner>
      )}
      <Card flush>
        {invs.length ? (
          <Table caption={t('inverters.title')}>
            <thead>
              <tr><th scope="col"><span className="sr-only">{t('inverters.order')}</span></th><th scope="col">{t('yield.inverters.inverter')}</th><th scope="col">{t('inverters.model')}</th>{hasPlanes && <th scope="col">{t('inverters.plane')}</th>}<th scope="col" className="num">{t('yield.inverters.peak')}</th><ActionsTh /></tr>
            </thead>
            <tbody>
              {invs.map((i, k) => (
                <tr key={i.id}>
                  <td>
                    <div className="flex items-center gap-0.5">
                      <Button variant="ghost" size="sm" iconOnly icon="up" label={t('inverters.moveUp', { name: i.name })} disabled={k === 0 || !!moving} busy={moving === i.id} onClick={() => move(k, -1)} />
                      <Button variant="ghost" size="sm" iconOnly icon="down" label={t('inverters.moveDown', { name: i.name })} disabled={k === invs.length - 1 || !!moving} onClick={() => move(k, 1)} />
                    </div>
                  </td>
                  <td>
                    <span className="flex items-center gap-2"><Swatch color={i.color} />
                      <span className="cell-main"><span className="font-medium">{i.name}</span><span className="xs muted">{t('inverters.ref', { ref: i.external_ref })}</span></span>
                    </span>
                  </td>
                  <td><span className="cell-main"><span className="sm">{i.model || '—'}</span><span className="xs muted">{i.serial_number ? t('inverters.sn', { sn: i.serial_number }) : t('inverters.noSerial')}</span></span></td>
                  {hasPlanes && <td><span className="sm">{(i.plane_id && planeNames.get(i.plane_id)) || <span className="muted">—</span>}</span></td>}
                  <td className="num">{fmtKwp(i.peak_power_kwp)}</td>
                  <td>
                    <Actions>
                      <Button variant="ghost" size="sm" icon="edit" onClick={() => setEditing(i)}>{t('common.edit')}</Button>
                      <Button variant="danger-ghost" size="sm" iconOnly icon="trash" label={t('inverters.deleteAria', { name: i.name })} onClick={() => onDelete(i)} />
                    </Actions>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : (
          <EmptyState icon="bolt" title={t('yield.noInverters.title')} text={t('inverters.emptyText')} />
        )}
      </Card>
      {editing && <InverterDialog sys={sys} inv={editing === 'new' ? null : editing} count={invs.length} onClose={() => setEditing(null)} />}
    </>
  );
}

const schema = z.object({
  external_ref: z.string().trim().min(1, 'validation.refRequired').max(100),
  name: z.string().trim().min(1, 'validation.nameRequired').max(200),
  model: z.string().trim(),
  serial_number: z.string().trim(),
  peak_power_kwp: z.preprocess(numOrUndef, z.number({ error: () => i18n.t('validation.number', { label: i18n.t('system.fields.peak') }) })
    .min(0, { error: () => i18n.t('validation.min', { label: i18n.t('system.fields.peak'), min: 0 }) }).optional()),
  max_ac_power_w: z.preprocess(numOrUndef, z.number({ error: () => i18n.t('validation.number', { label: i18n.t('inverters.maxAc') }) })
    .positive({ error: () => i18n.t('validation.min', { label: i18n.t('inverters.maxAc'), min: 1 }) }).optional()),
  sort_order: z.preprocess(numOrUndef, z.number().int('validation.wholeNumber').optional()),
  color: z.string().regex(/^#[0-9a-fA-F]{6}$/, 'validation.color'),
  plane_id: z.string(),
});
type Form = z.input<typeof schema>;
type Values = z.output<typeof schema>;

function InverterDialog({ sys, inv, count, onClose }: { sys: System; inv: Inverter | null; count: number; onClose: () => void }) {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const create = useMutation(createInverterMutation());
  const update = useMutation(updateInverterMutation());
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const form = useForm<Form, unknown, Values>({
    resolver: zodResolver(schema),
    defaultValues: inv
      ? { external_ref: inv.external_ref, name: inv.name, model: inv.model, serial_number: inv.serial_number, peak_power_kwp: inv.peak_power_kwp, max_ac_power_w: inv.max_ac_power_w ?? '', sort_order: inv.sort_order, color: inv.color || INVERTER_COLORS[0], plane_id: inv.plane_id ?? '' }
      : { external_ref: String(count + 1), name: `WR ${count + 1}`, model: '', serial_number: '', peak_power_kwp: '', max_ac_power_w: '', sort_order: count + 1, color: INVERTER_COLORS[count % INVERTER_COLORS.length], plane_id: '' },
  });
  const e = form.formState.errors;
  const msg = (k: keyof Form) => e[k]?.message as string | undefined;

  return (
    <FormDialog open onOpenChange={(o) => !o && onClose()} title={inv ? t('inverters.editTitle', { name: inv.name }) : t('inverters.add')} submitLabel={inv ? t('inverters.save') : t('inverters.add')}
      busy={create.isPending || update.isPending} serverError={serverError}
      onSubmit={form.handleSubmit(async (v) => {
        setServerError(null);
        // An empty rated AC power or plane is sent as null, which clears it.
        const body = { ...v, peak_power_kwp: v.peak_power_kwp ?? 0, max_ac_power_w: v.max_ac_power_w ?? null, plane_id: v.plane_id || null };
        try {
          const r = inv
            ? await update.mutateAsync({ path: { systemId: sys.id, inverterId: inv.id }, body })
            : await create.mutateAsync({ path: { systemId: sys.id }, body });
          await invalidate(...invalidateIds);
          toast(inv ? t('inverters.saved', { name: r.name }) : t('inverters.added', { name: r.name }));
          onClose();
        } catch (err) {
          setServerError(applyServerError(err, form.setError, { 'an inverter with ref': 'external_ref', external_ref: 'external_ref', name: 'name', peak_power_kwp: 'peak_power_kwp', max_ac_power_w: 'max_ac_power_w', plane: 'plane_id', color: 'color' }));
        }
      })}>
      <FormGrid>
        <Field label={t('inverters.refLabel')} required help={t('inverters.refHelp')} error={msg('external_ref')}>
          <Input maxLength={100} {...form.register('external_ref')} />
        </Field>
        <Field label={t('common.name')} required error={msg('name')}><Input maxLength={200} {...form.register('name')} /></Field>
        <Field label={t('inverters.model')} optional><Input {...form.register('model')} /></Field>
        <Field label={t('inverters.serial')} optional><Input {...form.register('serial_number')} /></Field>
        <Field label={t('system.fields.peak')} error={msg('peak_power_kwp')}><Input type="number" min={0} step={0.01} addon="kWp" {...form.register('peak_power_kwp')} /></Field>
        <Field label={t('inverters.maxAc')} optional help={t('inverters.maxAcHelp')} error={msg('max_ac_power_w')}>
          <Input type="number" min={1} step="any" addon="W" {...form.register('max_ac_power_w')} />
        </Field>
        {sys.planes.length > 0 && (
          <Field label={t('inverters.plane')} optional help={t('inverters.planeHelp')} error={msg('plane_id')}>
            <Select {...form.register('plane_id')} options={[{ value: '', label: t('inverters.noPlane') },
              ...sys.planes.map((p, k) => ({ value: p.id ?? '', label: planeName(p, k, (n) => t('system.planes.fallback', { n })) }))]} />
          </Field>
        )}
        <Field label={t('inverters.sortOrder')} error={msg('sort_order')}><Input type="number" step={1} {...form.register('sort_order')} /></Field>
        <Field label={t('inverters.color')} span2 error={msg('color')}>
          <div className="flex items-center gap-2">
            <input type="color" className="color-input" {...form.register('color')} />
            <span className="sm muted">{t('inverters.colorHelp')}</span>
          </div>
        </Field>
      </FormGrid>
    </FormDialog>
  );
}
