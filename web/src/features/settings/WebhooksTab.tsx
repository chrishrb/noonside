import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import { createWebhookMutation, deleteWebhookMutation, listWebhookDeliveriesOptions, listWebhooksOptions, testWebhookMutation, updateWebhookMutation } from '@/api/gen/@tanstack/react-query.gen';
import { zLocale, zWebhookEvent } from '@/api/gen/zod.gen';
import { useInvalidate } from '@/api/queries';
import type { System, Webhook, WebhookDelivery, WebhookEvent } from '@/api/types';
import { SectionHead } from '@/components/layout/Page';
import { Button } from '@/components/ui/Button';
import { useConfirm } from '@/components/ui/Confirm';
import { FormDialog } from '@/components/ui/Dialog';
import { Checkbox, Field, FieldError, Fieldset, FormGrid, Input, Select, Switch } from '@/components/ui/form';
import { CopyButton, Disclosure, useTick } from '@/components/ui/misc';
import { Badge, Banner, Card, EmptyState, Skeleton, Stat, type BadgeTone } from '@/components/ui/primitives';
import { Table } from '@/components/ui/Table';
import { toast, toastError } from '@/components/ui/toast';
import { LANGUAGE_NAMES, currentLanguage } from '@/i18n';
import { fmtDateTime, fmtRel } from '@/lib/format';
import { applyServerError, type ServerError } from '@/lib/forms';
import { VERIFY_SNIPPET, newSecret } from '@/features/notifications/NotificationTargets';

export const WEBHOOK_EVENTS: WebhookEvent[] = zWebhookEvent.options;
/** i18n key segment per event (keys can't contain dots). */
const eventKey = (e: string) => e.replace('.', '_') as 'alert_opened' | 'alert_resolved' | 'daily_summary' | 'surplus_forecast';
const refresh = 'listWebhooks' as const;

/** Receivers often answer with a whole HTML page; keep toasts short. */
const clip = (s: string, n = 160) => (s.length > n ? `${s.slice(0, n).trimEnd()}…` : s);

const STATUS_TONE: Record<string, BadgeTone> = { sent: 'success', failed: 'danger', pending: 'outline' };

export function WebhooksTab({ sys }: { sys: System }) {
  const { t } = useTranslation();
  useTick();
  const q = useQuery(listWebhooksOptions({ path: { systemId: sys.id } }));
  const [editing, setEditing] = useState<Webhook | 'new' | null>(null);
  const [deliveries, setDeliveries] = useState<Webhook | null>(null);

  if (q.isError) return <Banner tone="danger" title={t('webhooks.loadFailed')}>{q.error.message}</Banner>;
  if (!q.data) return <Skeleton style={{ height: 240 }} />;

  return (
    <>
      <SectionHead title={t('settings.tabs.webhooks')} sub={t('webhooks.sub')}>
        <Button variant="primary" icon="plus" onClick={() => setEditing('new')}>{t('webhooks.add')}</Button>
      </SectionHead>
      <div className="grid gap-4">
        {q.data.length ? q.data.map((w) => (
          <WebhookCard key={w.id} sys={sys} w={w} onEdit={() => setEditing(w)} onDeliveries={() => setDeliveries(w)} />
        )) : (
          <Card><EmptyState icon="link" title={t('webhooks.none')} text={t('webhooks.noneText')} /></Card>
        )}
      </div>
      <section className="section card" aria-labelledby="wh-payload-h">
        <div className="grid gap-3">
          <h2 id="wh-payload-h">{t('webhooks.payload.title')}</h2>
          <p className="sm muted">{t('webhooks.payload.text')}</p>
          <Disclosure summary={t('webhooks.payload.example')}>
            <pre className="code" tabIndex={0} aria-label={t('webhooks.payload.example')}>{PAYLOAD_SAMPLE}</pre>
          </Disclosure>
          <Disclosure summary={t('notifications.targets.verifyTitle')}>
            <div className="grid gap-2">
              <p className="sm">{t('webhooks.payload.verify')}</p>
              <pre className="code" tabIndex={0} aria-label={t('notifications.targets.verifyTitle')}>{VERIFY_SNIPPET}</pre>
            </div>
          </Disclosure>
        </div>
      </section>
      {editing && <WebhookDialog sys={sys} w={editing === 'new' ? null : editing} onClose={() => setEditing(null)} />}
      {deliveries && <DeliveriesDialog sys={sys} w={deliveries} onClose={() => setDeliveries(null)} />}
    </>
  );
}

function WebhookCard({ sys, w, onEdit, onDeliveries }: { sys: System; w: Webhook; onEdit: () => void; onDeliveries: () => void }) {
  const { t } = useTranslation();
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const path = { path: { systemId: sys.id, webhookId: w.id } };
  const update = useMutation(updateWebhookMutation());
  const del = useMutation(deleteWebhookMutation());
  const test = useMutation(testWebhookMutation());

  return (
    <Card as="article" className="grid gap-4" aria-labelledby={`wh-${w.id}`}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="grid min-w-0 gap-1">
          <h3 id={`wh-${w.id}`} className="break-words">{w.name}</h3>
          <span className="mono sm muted block max-w-full truncate" title={w.url}>{w.url}</span>
          <div className="flex flex-wrap gap-1">
            {w.events.map((e) => <Badge key={e} tone="outline">{t(`webhooks.events.${eventKey(e)}.label`)}</Badge>)}
            {w.has_secret && <Badge tone="neutral" icon="key">{t('webhooks.signed')}</Badge>}
          </div>
        </div>
        <Switch label={w.enabled ? t('webhooks.enabled') : t('webhooks.disabled')} checked={w.enabled} disabled={update.isPending} onCheckedChange={async (enabled) => {
          try {
            await update.mutateAsync({ ...path, body: { enabled } });
            toast(enabled ? t('webhooks.enabledToast', { name: w.name }) : t('webhooks.disabledToast', { name: w.name }));
            await invalidate(refresh);
          } catch (e) { toastError(e); }
        }} />
      </div>
      <div className="ds-meta">
        <Stat label={t('webhooks.lastDelivery')}>
          <span className="font-normal" title={w.last_delivery_at ? fmtDateTime(w.last_delivery_at, sys.timezone) : undefined}>
            {w.last_delivery_at ? fmtRel(w.last_delivery_at) : t('common.never')}
          </span>
        </Stat>
        <Stat label={t('webhooks.status')}>
          {w.last_status ? <Badge tone={STATUS_TONE[w.last_status]} dot>{t(`webhooks.statuses.${w.last_status}`)}</Badge> : <span className="font-normal muted">—</span>}
        </Stat>
        <Stat label={t('webhooks.language')}><span className="font-normal">{LANGUAGE_NAMES[w.locale]}</span></Stat>
      </div>
      {w.last_status === 'failed' && w.last_error && <Banner tone="danger" title={t('webhooks.lastError')}><span className="mono break-all">{w.last_error}</span></Banner>}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="danger-ghost" size="sm" icon="trash" onClick={async () => {
          const ok = await confirm({ title: t('webhooks.deleteConfirm.title', { name: w.name }), danger: true, confirmLabel: t('webhooks.deleteConfirm.label'),
            message: t('webhooks.deleteConfirm.message'), onConfirm: () => del.mutateAsync(path) });
          if (ok) { toast(t('webhooks.deleted', { name: w.name })); await invalidate(refresh); }
        }}>{t('common.delete')}</Button>
        <Button size="sm" icon="list" onClick={onDeliveries}>{t('webhooks.deliveries')}</Button>
        <Button size="sm" icon="edit" onClick={onEdit}>{t('common.edit')}</Button>
        <Button size="sm" icon="play" busy={test.isPending} onClick={async () => {
          try {
            const r = await test.mutateAsync(path);
            if (r.ok) toast(t('webhooks.testSent', { name: w.name }));
            else toastError(new Error(t('webhooks.testFailed', { error: clip(r.error ?? '') })));
            await invalidate(refresh, 'listWebhookDeliveries');
          } catch (e) { toastError(e); }
        }}>{t('webhooks.test')}</Button>
      </div>
    </Card>
  );
}

const schema = z.object({
  name: z.string().trim().min(1, 'validation.nameRequired').max(200, 'validation.max200'),
  url: z.string().trim().max(2000).regex(/^https:\/\/[^/\s]+/i, 'webhooks.urlInvalid'),
  secret: z.string().max(200, 'validation.max200'),
  removeSecret: z.boolean(),
  events: z.array(zWebhookEvent).min(1, 'webhooks.eventsRequired'),
  locale: zLocale,
  enabled: z.boolean(),
});
type Form = z.infer<typeof schema>;

function WebhookDialog({ sys, w, onClose }: { sys: System; w: Webhook | null; onClose: () => void }) {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const create = useMutation(createWebhookMutation());
  const update = useMutation(updateWebhookMutation());
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const form = useForm<Form>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: w?.name ?? '', url: w?.url ?? '', secret: '', removeSecret: false,
      events: w?.events ?? ['alert.opened', 'alert.resolved'],
      locale: w?.locale ?? currentLanguage(), enabled: w?.enabled ?? true,
    },
  });
  const { register, formState: { errors } } = form;
  const [secret, removeSecret, events, enabled] = useWatch({ control: form.control, name: ['secret', 'removeSecret', 'events', 'enabled'] });
  const msg = (k: keyof Form) => errors[k]?.message as string | undefined;

  const toggleEvent = (e: WebhookEvent, on: boolean) => {
    const next = on ? [...events, e] : events.filter((x) => x !== e);
    form.setValue('events', WEBHOOK_EVENTS.filter((x) => next.includes(x)), { shouldDirty: true, shouldValidate: form.formState.isSubmitted });
  };

  const submit = form.handleSubmit(async (v) => {
    setServerError(null);
    const base = { name: v.name, url: v.url, events: v.events, locale: v.locale, enabled: v.enabled };
    try {
      if (w) {
        // Omitted secret keeps it; an empty one removes it.
        const s = v.secret.trim() ? { secret: v.secret.trim() } : v.removeSecret ? { secret: '' } : {};
        await update.mutateAsync({ path: { systemId: sys.id, webhookId: w.id }, body: { ...base, ...s } });
        toast(t('webhooks.saved', { name: v.name }));
      } else {
        await create.mutateAsync({ path: { systemId: sys.id }, body: { ...base, ...(v.secret.trim() && { secret: v.secret.trim() }) } });
        toast(t('webhooks.added', { name: v.name }));
      }
      await invalidate(refresh);
      onClose();
    } catch (err) {
      setServerError(applyServerError(err, form.setError, { name: 'name', url: 'url', secret: 'secret', events: 'events', locale: 'locale' }));
    }
  });

  const secretHelp = secret
    ? t('notifications.targets.secretCopy')
    : w?.has_secret ? (removeSecret ? t('webhooks.secretRemoved') : t('notifications.targets.secretKeep')) : t('webhooks.secretHelp');

  return (
    <FormDialog open onOpenChange={(o) => !o && onClose()} wide title={w ? t('webhooks.editTitle', { name: w.name }) : t('webhooks.add')}
      subtitle={t('webhooks.dialogSubtitle')} submitLabel={w ? t('webhooks.save') : t('webhooks.add')}
      busy={create.isPending || update.isPending} serverError={serverError} onSubmit={submit}>
      <FormGrid>
        <Field label={t('common.name')} required error={msg('name')}>
          <Input maxLength={200} placeholder={t('webhooks.namePlaceholder')} autoFocus {...register('name')} />
        </Field>
        <Field label={t('webhooks.language')} help={t('webhooks.languageHelp')} error={msg('locale')}>
          <Select options={zLocale.options.map((l) => ({ value: l, label: LANGUAGE_NAMES[l] }))} {...register('locale')} />
        </Field>
        <Field label={t('webhooks.url')} required span2 help={t('webhooks.urlHelp')} error={msg('url')}>
          <Input type="url" inputMode="url" autoComplete="off" spellCheck={false} placeholder="https://example.com/noonside-hook" {...register('url')} />
        </Field>
        <Field label={t('notifications.targets.webhookSecret')} optional span2 error={msg('secret')} help={secretHelp}>
          <div className="flex flex-wrap gap-2">
            <Input className="mono min-w-0 flex-1 basis-56" autoComplete="off" spellCheck={false} placeholder={w?.has_secret && !removeSecret ? '••••••••' : ''}
              {...register('secret', { onChange: () => form.setValue('removeSecret', false) })} />
            <Button icon="key" onClick={() => form.setValue('secret', newSecret(), { shouldDirty: true })}>{t('notifications.targets.generate')}</Button>
            {secret && <CopyButton text={secret} />}
          </div>
        </Field>
        {w?.has_secret && !secret && (
          <div className="field span-2"><Checkbox label={t('notifications.targets.removeSecret')} {...register('removeSecret')} /></div>
        )}
      </FormGrid>
      <Fieldset legend={t('webhooks.eventsLegend')}>
        <div className="grid gap-3">
          {WEBHOOK_EVENTS.map((e) => (
            <Checkbox key={e} checked={events.includes(e)} onChange={(ev) => toggleEvent(e, ev.target.checked)}
              label={<>{t(`webhooks.events.${eventKey(e)}.label`)} <code className="mono xs muted">{e}</code></>}
              help={t(`webhooks.events.${eventKey(e)}.help`)} />
          ))}
          {errors.events && <FieldError>{t('webhooks.eventsRequired')}</FieldError>}
        </div>
      </Fieldset>
      <Switch label={enabled ? t('webhooks.enabled') : t('webhooks.disabled')} checked={enabled} onCheckedChange={(on) => form.setValue('enabled', on, { shouldDirty: true })} />
    </FormDialog>
  );
}

function DeliveriesDialog({ sys, w, onClose }: { sys: System; w: Webhook; onClose: () => void }) {
  const { t } = useTranslation();
  const q = useQuery(listWebhookDeliveriesOptions({ path: { systemId: sys.id, webhookId: w.id } }));
  return (
    <FormDialog open onOpenChange={(o) => !o && onClose()} wide title={t('webhooks.deliveriesTitle', { name: w.name })} subtitle={t('webhooks.deliveriesSubtitle')}
      submitLabel={t('common.close')} hideCancel>
      {q.isError ? <Banner tone="danger" title={t('webhooks.deliveriesFailed')}>{q.error.message}</Banner>
        : !q.data ? <Skeleton style={{ height: 160 }} />
          : !q.data.length ? <EmptyState icon="list" title={t('webhooks.noDeliveries')} text={t('webhooks.noDeliveriesText')} />
            : <DeliveryTable rows={q.data} tz={sys.timezone} />}
    </FormDialog>
  );
}

function DeliveryTable({ rows, tz }: { rows: WebhookDelivery[]; tz: string }) {
  const { t } = useTranslation();
  return (
    <Table caption={t('webhooks.deliveries')} scroll>
      <thead>
        <tr><th scope="col">{t('webhooks.event')}</th><th scope="col">{t('webhooks.status')}</th><th scope="col">{t('common.created')}</th><th scope="col">{t('webhooks.attempts')}</th></tr>
      </thead>
      <tbody>
        {rows.map((d) => (
          <tr key={d.id}>
            <td>
              <span className="grid gap-0.5">
                <span className="mono sm whitespace-nowrap">{d.event}</span>
                {d.last_error && <span className="xs break-all" style={{ color: 'var(--danger-text)' }}>{d.last_error}</span>}
              </span>
            </td>
            <td>
              <span className="grid gap-0.5">
                <Badge tone={STATUS_TONE[d.status]} dot>{t(`webhooks.statuses.${d.status}`)}</Badge>
                {d.status === 'pending' && d.next_attempt_at && <span className="xs muted whitespace-nowrap">{t('webhooks.retryAt', { time: fmtDateTime(d.next_attempt_at, tz) })}</span>}
              </span>
            </td>
            <td className="sm whitespace-nowrap">{fmtDateTime(d.sent_at ?? d.created_at, tz)}</td>
            <td className="sm tabular-nums">{d.attempts}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

const PAYLOAD_SAMPLE = `POST /your-endpoint
Content-Type: application/json
X-Noonside-Event: daily.summary
X-Noonside-Delivery: 3f0c…
X-Noonside-Signature: sha256=9b1d…

{
  "id": "3f0c…",
  "event": "daily.summary",
  "created_at": "2026-06-02T04:00:12Z",
  "system_id": "…",
  "system_name": "Barn roof",
  "url": "https://…/s/…",
  "data": {
    "date": "2026-06-01", "energy_kwh": 48.2, "expected_kwh": 51.0,
    "revenue": 3.95, "currency": "EUR", "co2_avoided_kg": 18.3,
    "inverters": [{ "name": "WR 1", "energy_kwh": 24.1 }]
  }
}`;
