import { useMutation } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { deleteSystemImageMutation, deleteSystemMutation, updateSystemMutation, uploadSystemImageMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { System } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { can } from '@/auth/scopes';
import { Button } from '@/components/ui/Button';
import { useConfirm, useUnsavedChanges } from '@/components/ui/Confirm';
import { ImageUpload } from '@/components/ui/ImageUpload';
import { Banner, Card, SysTile } from '@/components/ui/primitives';
import { toast } from '@/components/ui/toast';
import { fmtDate } from '@/lib/format';
import { applyServerError, type ServerError } from '@/lib/forms';
import { LAST_SYSTEM, store } from '@/lib/storage';
import { SYSTEM_ERRORS, SystemFields, retentionCutoff, systemBody, systemDefaults, systemSchema, type SystemForm, type SystemValues } from '@/features/systems/SystemFields';

/** `children` go between the form and the danger zone (the targets section). */
export function GeneralTab({ sys, children }: { sys: System; children?: ReactNode }) {
  const { t } = useTranslation();
  const me = useMeData();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const form = useForm<SystemForm, unknown, SystemValues>({ resolver: zodResolver(systemSchema), defaultValues: systemDefaults(sys) });
  const update = useMutation(updateSystemMutation());
  const del = useMutation(deleteSystemMutation());
  const dirty = form.formState.isDirty;
  useUnsavedChanges(dirty);

  const onDelete = async () => {
    const ok = await confirm({
      title: t('general.deleteConfirm.title', { name: sys.name }), danger: true, confirmLabel: t('general.deleteConfirm.label'), typeToConfirm: sys.name,
      message: t('general.deleteConfirm.message'),
      onConfirm: () => del.mutateAsync({ path: { systemId: sys.id } }),
    });
    if (!ok) return;
    form.reset(); // nothing left to save; don't ask before leaving
    store.set(LAST_SYSTEM, null);
    await invalidate('listSystems', 'getMe', 'getDashboard');
    toast(t('general.deleted', { name: sys.name }));
    navigate('/');
  };

  return (
    <div className="settings-grid">
      <form className="card form" noValidate onSubmit={form.handleSubmit(async (v) => {
        setServerError(null);
        const years = v.minute_retention_years;
        const was = sys.minute_retention_years;
        if (years != null && (was == null || years < was)) {
          const ok = await confirm({
            title: t('general.retentionConfirm.title'), danger: true, confirmLabel: t('general.retentionConfirm.label'),
            message: t('general.retentionConfirm.message', { date: fmtDate(retentionCutoff(years, sys.timezone)) }),
          });
          if (!ok) return;
        }
        try {
          const saved = await update.mutateAsync({ path: { systemId: sys.id }, body: systemBody(v) });
          form.reset(systemDefaults(saved));
          await invalidate('getSystem', 'listSystems', 'getMe', 'getDashboard', 'getWeather', 'listInverters');
          toast(t('general.saved'));
        } catch (err) {
          setServerError(applyServerError(err, form.setError, SYSTEM_ERRORS));
        }
      })}>
        {serverError && <Banner tone="danger" role="alert" title={serverError.title}>{serverError.message}</Banner>}
        <SystemFields form={form} timezone={sys.timezone} />
        <div className="form-actions">
          <Button disabled={!dirty} onClick={() => { form.reset(); setServerError(null); }}>{t('confirm.discardLabel')}</Button>
          <Button type="submit" variant="primary" disabled={!dirty} busy={update.isPending}>{t('common.saveChanges')}</Button>
        </div>
      </form>
      {me.features.uploads && can(me, sys.id, 'system:write') && <SystemPicture sys={sys} />}
      {children}
      {can(me, sys.id, 'system:delete') && (
        <Card as="section" className="danger-zone" aria-labelledby="dz-h">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="grid max-w-[60ch] gap-1">
              <h2 id="dz-h">{t('general.delete')}</h2>
              <p className="sm muted">{t('general.deleteText', { name: sys.name })}</p>
            </div>
            <Button variant="danger" icon="trash" onClick={onDelete}>{t('general.delete')}</Button>
          </div>
        </Card>
      )}
    </div>
  );
}

/** The system's picture, shown in the system switcher and on the dashboard. */
function SystemPicture({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const upload = useMutation(uploadSystemImageMutation());
  const remove = useMutation(deleteSystemImageMutation());
  const path = { systemId: sys.id };
  const refresh = () => invalidate('getSystem', 'listSystems', 'getMe', 'getDashboard');
  return (
    <Card as="section" aria-labelledby="pic-h" className="grid gap-4">
      <div className="grid max-w-[60ch] gap-1">
        <h2 id="pic-h">{t('picture.system')}</h2>
        <p className="sm muted">{t('picture.systemHelp')}</p>
      </div>
      <ImageUpload max={1600} hasImage={!!sys.image_url} removeTitle={t('picture.removeSystem', { name: sys.name })}
        preview={<SysTile name={sys.name} src={sys.image_url} className="sys-tile-lg" />}
        onUpload={async (image) => { await upload.mutateAsync({ path, body: image }); await refresh(); }}
        onRemove={async () => { await remove.mutateAsync({ path }); await refresh(); }} />
    </Card>
  );
}
