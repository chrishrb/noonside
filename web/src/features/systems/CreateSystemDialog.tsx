import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { createSystemMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import { FormDialog } from '@/components/ui/Dialog';
import { toast } from '@/components/ui/toast';
import { applyServerError, type ServerError } from '@/lib/forms';
import { SYSTEM_ERRORS, SystemFields, systemBody, systemDefaults, systemSchema, type SystemForm, type SystemValues } from './SystemFields';

/** Superadmin dialog to create a system; the creator becomes owner. */
export function CreateSystemDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const invalidate = useInvalidate();
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const form = useForm<SystemForm, unknown, SystemValues>({ resolver: zodResolver(systemSchema), defaultValues: systemDefaults() });
  const create = useMutation(createSystemMutation());

  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) { form.reset(systemDefaults()); setServerError(null); }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={close}
      title={t('dashboard.newSystem')}
      subtitle={t('system.createSubtitle')}
      wide
      submitLabel={t('switcher.create')}
      busy={create.isPending}
      serverError={serverError}
      onSubmit={form.handleSubmit(async (v) => {
        setServerError(null);
        try {
          const s = await create.mutateAsync({ body: systemBody(v) });
          await invalidate('listSystems', 'listAdminSystems', 'getMe', 'getDashboard');
          toast(t('system.created', { name: s.name }));
          close(false);
          navigate(`/s/${s.id}/settings/inverters`);
        } catch (err) {
          setServerError(applyServerError(err, form.setError, SYSTEM_ERRORS));
        }
      })}
    >
      <SystemFields form={form} />
    </FormDialog>
  );
}
