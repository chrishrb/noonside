import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { createApiKeyMutation, listApiKeysOptions, revokeApiKeyMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { APIKeyScope, System } from '@/api/types';
import { SectionHead } from '@/components/layout/Page';
import { Button } from '@/components/ui/Button';
import { useConfirm } from '@/components/ui/Confirm';
import { FormDialog } from '@/components/ui/Dialog';
import { Icon } from '@/components/ui/Icon';
import { Checkbox, Field, FieldError, Fieldset, Input, Segmented } from '@/components/ui/form';
import { CopyButton, SecretReveal, useTick } from '@/components/ui/misc';
import { Badge, Banner, Card, EmptyState, Skeleton } from '@/components/ui/primitives';
import { Actions, ActionsTh, Table } from '@/components/ui/Table';
import { toast } from '@/components/ui/toast';
import { API_URL } from '@/api/client';
import { fmtDate, fmtRel } from '@/lib/format';
import { applyServerError, type ServerError } from '@/lib/forms';

/** Device push (data sources tab): keys with data:ingest. */
export const PushKeysTab = ({ sys }: { sys: System }) => <ApiKeys sys={sys} scope="data:ingest" />;
/** Read access (integrations tab): keys with data:read. */
export const ReadKeysTab = ({ sys }: { sys: System }) => <ApiKeys sys={sys} scope="data:read" />;

// Keys live in one table but are shown by what they are for: a key with both
// scopes appears in both sections.
function ApiKeys({ sys, scope }: { sys: System; scope: APIKeyScope }) {
  const { t } = useTranslation();
  useTick();
  const q = useQuery(listApiKeysOptions({ path: { systemId: sys.id } }));
  const revoke = useMutation(revokeApiKeyMutation());
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const [createOpen, setCreateOpen] = useState(false);
  const push = scope === 'data:ingest';
  const title = t(push ? 'settings.tabs.push' : 'settings.tabs.keys');
  const other = t(push ? 'settings.tabs.keys' : 'settings.tabs.push');

  const base = new URL(API_URL, window.location.origin).toString().replace(/\/$/, '');
  const snippet = `curl -X POST ${base}/systems/${sys.id}/ingest/measurements \\
  -H "X-API-Key: $NOONSIDE_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "create_inverters": false,
    "measurements": [
      { "inverter": "1", "time": "${new Date().toISOString().slice(0, 16)}:00Z",
        "pac_w": 6120, "pdc_w": 6380, "udc_v": 612, "eday_wh": 18450,
        "status_code": 8, "status_text": "MPP" }
    ]
  }'`;

  const [example, setExample] = useState<'ha' | 'grafana'>('ha');
  const shown = push ? snippet : example === 'ha' ? haSnippet(base, sys.id) : grafanaSnippet(base, sys.id);
  const examplesH = `examples-${push ? 'push' : 'read'}-h`;

  if (q.isError) return <Banner tone="danger" title={t('apiKeys.loadFailed')}>{q.error.message}</Banner>;
  if (!q.data) return <Skeleton style={{ height: 240 }} />;
  const keys = q.data.filter((k) => k.scopes.includes(scope));

  return (
    <>
      <SectionHead title={title} sub={<>{t(push ? 'apiKeys.push.sub' : 'apiKeys.read.sub')} <code className="mono">X-API-Key</code>.</>}>
        <Button variant="primary" icon="plus" onClick={() => setCreateOpen(true)}>{t('apiKeys.create')}</Button>
      </SectionHead>
      <Card flush>
        {keys.length ? (
          <Table caption={title}>
            <thead><tr><th scope="col">{t('common.name')}</th><th scope="col">{t('apiKeys.key')}</th><th scope="col">{t('apiKeys.scopes')}</th><th scope="col">{t('common.created')}</th><th scope="col">{t('apiKeys.lastUsed')}</th><ActionsTh /></tr></thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id}>
                  <td><span className="flex items-center gap-2"><Icon name="key" size="sm" /><span className="font-medium">{k.name}</span></span></td>
                  <td className="mono whitespace-nowrap">{k.prefix}…</td>
                  <td><span className="flex flex-wrap gap-1">{k.scopes.map((s) => <Badge key={s} tone="outline">{s}</Badge>)}</span></td>
                  <td className="sm muted whitespace-nowrap">{fmtDate(k.created_at)}</td>
                  <td className="sm whitespace-nowrap">{k.last_used_at ? fmtRel(k.last_used_at) : <span className="muted">{t('common.never')}</span>}</td>
                  <td>
                    <Actions>
                      <Button variant="danger-ghost" size="sm" onClick={async () => {
                        const message = [t(push ? 'apiKeys.revoke.message' : 'apiKeys.revoke.messageRead'), k.scopes.length > 1 ? t('apiKeys.revoke.both', { other }) : ''].filter(Boolean).join(' ');
                        const ok = await confirm({ title: t('apiKeys.revoke.title', { name: k.name }), danger: true, confirmLabel: t('apiKeys.revoke.label'), message,
                          onConfirm: () => revoke.mutateAsync({ path: { systemId: sys.id, apiKeyId: k.id } }) });
                        if (ok) { toast(t('apiKeys.revoke.done', { name: k.name })); await invalidate('listApiKeys'); }
                      }}>{t('members.revoke.label')}</Button>
                    </Actions>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : (
          <EmptyState icon="key" title={t(push ? 'apiKeys.push.none' : 'apiKeys.read.none')} text={t(push ? 'apiKeys.push.noneText' : 'apiKeys.read.noneText')} />
        )}
      </Card>
      <section className="section card" aria-labelledby={examplesH}>
        <div className="grid gap-3">
          <h2 id={examplesH}>{t(push ? 'apiKeys.push.example' : 'apiKeys.examples.title')}</h2>
          {!push && (
            <div className="flex flex-wrap items-center gap-2">
              <Segmented label={t('apiKeys.examples.title')} value={example} onChange={setExample}
                options={[['ha', 'Home Assistant'], ['grafana', 'Grafana']]} />
            </div>
          )}
          <p className="sm">
            {push ? <>{t('apiKeys.push.text')} <code className="mono">/ingest/daily</code>.</>
              : t(example === 'ha' ? 'apiKeys.read.haText' : 'apiKeys.read.grafanaText')}
          </p>
          <pre className="code" tabIndex={0} aria-label={t(push ? 'apiKeys.push.example' : 'apiKeys.examples.title')}>{shown}</pre>
          <div><CopyButton text={shown} label={t('apiKeys.push.copy')} size="sm" /></div>
        </div>
      </section>
      <CreateKeyDialog sys={sys} scope={scope} open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

function CreateKeyDialog({ sys, scope, open, onOpenChange }: { sys: System; scope: APIKeyScope; open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation();
  const create = useMutation(createApiKeyMutation());
  const invalidate = useInvalidate();
  const [created, setCreated] = useState<{ name: string; key: string } | null>(null);
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const read = scope === 'data:read';
  const defaults = { name: '', ingest: !read, read };
  const form = useForm<{ name: string; ingest: boolean; read: boolean }>({ defaultValues: defaults });
  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) { setCreated(null); setServerError(null); form.reset(defaults); void invalidate('listApiKeys'); }
  };

  if (created) {
    return (
      <FormDialog open={open} onOpenChange={close} title={t('apiKeys.copyTitle')} submitLabel={t('apiKeys.copied')} hideCancel>
        <Banner tone="warn" title={t('apiKeys.onceTitle')}>{t('apiKeys.onceText')}</Banner>
        <SecretReveal label={created.name} value={created.key} />
      </FormDialog>
    );
  }
  return (
    <FormDialog open={open} onOpenChange={close} title={t('apiKeys.create')} subtitle={t(read ? 'apiKeys.createSubtitleRead' : 'apiKeys.createSubtitle')} submitLabel={t('apiKeys.createKey')}
      busy={create.isPending} serverError={serverError}
      onSubmit={form.handleSubmit(async ({ name, ingest, read }) => {
        setServerError(null);
        const scopes: APIKeyScope[] = [...(ingest ? ['data:ingest' as const] : []), ...(read ? ['data:read' as const] : [])];
        if (!scopes.length) { form.setError('ingest', { message: 'apiKeys.scopeRequired' }); return; }
        try {
          const r = await create.mutateAsync({ path: { systemId: sys.id }, body: { name: name.trim(), scopes } });
          setCreated({ name: r.api_key.name, key: r.key });
        } catch (err) { setServerError(applyServerError(err, form.setError, { name: 'name' })); }
      })}>
      <Field label={t('common.name')} required error={form.formState.errors.name?.message}>
        <Input placeholder={t(read ? 'apiKeys.namePlaceholderRead' : 'apiKeys.namePlaceholder')} maxLength={200} autoFocus {...form.register('name', { validate: (v) => v.trim() !== '' || 'validation.nameRequired' })} />
      </Field>
      <Fieldset legend={t('apiKeys.scopes')}>
        <div className="grid gap-2">
          <Checkbox label={<code className="mono">data:ingest</code>} help={t('apiKeys.scopeIngest')} {...form.register('ingest', { onChange: () => form.clearErrors('ingest') })} />
          <Checkbox label={<code className="mono">data:read</code>} help={t('apiKeys.scopeRead')} {...form.register('read', { onChange: () => form.clearErrors('ingest') })} />
          {form.formState.errors.ingest && <FieldError>{t('apiKeys.scopeRequired')}</FieldError>}
        </div>
      </Fieldset>
    </FormDialog>
  );
}

/** Home Assistant `rest:` sensors for live power, today's energy and the best window. */
function haSnippet(base: string, sid: string) {
  const sys = `${base}/systems/${sid}`;
  return `# configuration.yaml — the key goes into secrets.yaml as noonside_api_key
rest:
  - resource: ${sys}/live
    scan_interval: 60
    headers:
      X-API-Key: !secret noonside_api_key
    sensor:
      - name: "PV power"
        unique_id: noonside_${sid.slice(0, 8)}_power
        value_template: "{{ value_json.pac_w }}"
        unit_of_measurement: "W"
        device_class: power
        state_class: measurement
      - name: "PV energy today"
        unique_id: noonside_${sid.slice(0, 8)}_today
        value_template: "{{ value_json.today_kwh | default(0) }}"
        unit_of_measurement: "kWh"
        device_class: energy
        state_class: total_increasing
  - resource: ${sys}/advice?hours=3
    scan_interval: 900
    headers:
      X-API-Key: !secret noonside_api_key
    sensor:
      - name: "PV best window start"
        unique_id: noonside_${sid.slice(0, 8)}_window_start
        value_template: "{{ value_json.solar_window.start if value_json.solar_window is defined else none }}"
        device_class: timestamp
      - name: "PV best window end"
        unique_id: noonside_${sid.slice(0, 8)}_window_end
        value_template: "{{ value_json.solar_window.end if value_json.solar_window is defined else none }}"
        device_class: timestamp`;
}

/** A Grafana Infinity query (JSON, backend parser) for the day curve. */
function grafanaSnippet(base: string, sid: string) {
  return JSON.stringify({
    type: 'json',
    source: 'url',
    parser: 'backend',
    format: 'timeseries',
    url: `${base}/systems/${sid}/overview?period=day&date=\${__to:date:YYYY-MM-DD}`,
    url_options: { method: 'GET', headers: [{ key: 'X-API-Key', value: '<your data:read key>' }] },
    root_selector: '$zip(timestamps, total).{ "time": $[0], "power_w": $[1] }',
    columns: [
      { selector: 'time', text: 'Time', type: 'timestamp' },
      { selector: 'power_w', text: 'Power', type: 'number' },
    ],
  }, null, 2);
}
