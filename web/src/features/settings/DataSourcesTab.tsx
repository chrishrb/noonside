import { useMutation, useQuery } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { createDataSourceMutation, deleteDataSourceMutation, ingestDailyMutation, listDataSourcesOptions, runDataSourceMutation, updateDataSourceMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { DataSource, DataSourceKind, DataSourceConfig, DataSourceConfigWritable, System } from '@/api/types';
import { SectionHead } from '@/components/layout/Page';
import { Button } from '@/components/ui/Button';
import { useConfirm } from '@/components/ui/Confirm';
import { FormDialog } from '@/components/ui/Dialog';
import { Checkbox, Field, Fieldset, FormGrid, Input, Select, Switch, Textarea } from '@/components/ui/form';
import { Badge, Banner, Card, EmptyState, Skeleton, Stat } from '@/components/ui/primitives';
import { toast, toastError } from '@/components/ui/toast';
import { useTick } from '@/components/ui/misc';
import { parseDailyCsv } from '@/lib/csv';
import { every, fmtDateTime, fmtRel } from '@/lib/format';
import { serverErrorOf, type ServerError } from '@/lib/forms';
import { kinds, kindOf, visibleFields, type ConfigValues, type KindField, type KindInfo } from './dataSourceKinds';
import { CredentialsDialog, HostedPanel } from './HostedUpload';
import { useMeData } from '@/auth/useMe';
import { Link } from 'react-router';

const refresh = ['listDataSources', 'getOverview', 'getLive', 'getDashboard'] as const;

type Direction = KindInfo['direction'];

/** Loggers noonside reads (data sources tab). */
export const DataSourcesTab = ({ sys }: { sys: System }) => <SourceList sys={sys} direction="in" />;
/** Uploads to PVOutput (integrations tab); the same API, kind pvoutput_export. */
export const PvoutputTab = ({ sys }: { sys: System }) => <SourceList sys={sys} direction="out" />;
/** CSV daily import (data sources tab). */
export const DailyImportTab = ({ sys }: { sys: System }) => <DailyImport sys={sys} />;

function SourceList({ sys, direction }: { sys: System; direction: Direction }) {
  const { t } = useTranslation();
  const out = direction === 'out';
  useTick();
  const q = useQuery(listDataSourcesOptions({ path: { systemId: sys.id } }));
  const [editing, setEditing] = useState<DataSource | 'new' | null>(null);
  // A hosted source's password, shown once after creating or resetting it.
  const [credentials, setCredentials] = useState<{ d: DataSource; password: string; reset: boolean } | null>(null);
  if (q.isError) return <Banner tone="danger" title={t('sources.loadFailed')}>{q.error.message}</Banner>;
  if (!q.data) return <Skeleton style={{ height: 240 }} />;
  const list = q.data.filter((d) => kindOf(d.kind).direction === direction);
  return (
    <>
      <SectionHead title={t(out ? 'settings.tabs.pvoutput' : 'settings.tabs.sources')} sub={t(out ? 'pvoutput.sub' : 'sources.sub')}>
        <Button variant="primary" icon="plus" onClick={() => setEditing('new')}>{t(out ? 'pvoutput.add' : 'sources.add')}</Button>
      </SectionHead>
      <div className="grid gap-4">
        {list.length ? list.map((d) => <SourceCard key={d.id} sys={sys} d={d} onEdit={() => setEditing(d)} onPassword={(r, password) => setCredentials({ d: r, password, reset: true })} />) : (
          <Card>{out
            ? <EmptyState icon="upload" title={t('pvoutput.none')} text={t('pvoutput.noneText')} />
            : <EmptyState icon="database" title={t('sources.none')} text={t('sources.noneText')} />}</Card>
        )}
      </div>
      {editing && <SourceDialog sys={sys} direction={direction} ds={editing === 'new' ? null : editing} onClose={() => setEditing(null)}
        onCreatedHosted={(d, password) => { setEditing(null); setCredentials({ d, password, reset: false }); }} />}
      {credentials && (
        <CredentialsDialog d={credentials.d} password={credentials.password} reset={credentials.reset} onClose={() => setCredentials(null)} />
      )}
    </>
  );
}

function SourceCard({ sys, d, onEdit, onPassword }: { sys: System; d: DataSource; onEdit: () => void; onPassword: (d: DataSource, password: string) => void }) {
  const { t } = useTranslation();
  const K = kindOf(d.kind);
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const path = { path: { systemId: sys.id, dataSourceId: d.id } };
  const update = useMutation(updateDataSourceMutation());
  const del = useMutation(deleteDataSourceMutation());
  const run = useMutation(runDataSourceMutation());
  const [label, value] = K.summary(d.config, d);

  return (
    <Card as="article" className="grid gap-4" aria-labelledby={`ds-${d.id}`}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="grid gap-1">
          <h3 id={`ds-${d.id}`}>{d.name}</h3>
          <div className="flex flex-wrap gap-2">
            <Badge tone="outline">{K.label}</Badge>
            {d.last_error ? <Badge tone="danger" dot>{t('sources.lastRunFailed')}</Badge> : d.last_success_at ? <Badge tone="success" dot>{t('sources.healthy')}</Badge> : <Badge tone="outline">{t('sources.neverRun')}</Badge>}
          </div>
        </div>
        <Switch label={d.enabled ? t('sources.enabled') : t('sources.disabled')} checked={d.enabled} disabled={update.isPending} onCheckedChange={async (enabled) => {
          try {
            await update.mutateAsync({ ...path, body: { enabled } });
            toast(enabled ? t('sources.enabledToast', { name: d.name }) : t('sources.disabledToast', { name: d.name }));
            await invalidate(...refresh);
          } catch (e) { toastError(e); }
        }} />
      </div>
      <div className="ds-meta">
        {!K.hosted && <Stat label={label}><span className="mono block truncate font-normal" title={value}>{value}</span></Stat>}
        {/* Hosted sources are imported after each upload, not on an interval. */}
        {K.hosted
          ? <Stat label={t('sources.interval')}><span className="font-normal">{t('sources.hosted.onUpload')}</span></Stat>
          : <Stat label={t('sources.interval')}><span className="font-normal">{every(d.poll_interval_s)}</span></Stat>}
        <Stat label={t('sources.lastRun')}><span className="font-normal">{fmtRel(d.last_run_at)}</span></Stat>
        <Stat label={t('sources.lastSuccess')}><span className="font-normal">{fmtRel(d.last_success_at)}</span></Stat>
        {d.kind === 'pvoutput_export' && (
          <Stat label={t('sources.uploadedUntil')}><span className="font-normal" title={d.uploaded_until ? fmtDateTime(d.uploaded_until, sys.timezone) : undefined}>{d.uploaded_until ? fmtRel(d.uploaded_until) : t('common.never')}</span></Stat>
        )}
      </div>
      {K.hosted && <HostedPanel sys={sys} d={d} onPassword={onPassword} />}
      {/* A full upload account has its own banner in HostedPanel. */}
      {d.last_error && !d.upload?.locked && <Banner tone="danger" title={t('sources.lastError')}><span className="mono break-all">{d.last_error}</span></Banner>}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="danger-ghost" size="sm" icon="trash" onClick={async () => {
          const out = K.direction === 'out';
          const ok = await confirm({ title: t('sources.deleteConfirm.title', { name: d.name }), danger: true, confirmLabel: t(out ? 'pvoutput.deleteLabel' : 'sources.deleteConfirm.label'),
            message: K.hosted ? t('sources.hosted.deleteMessage') : out ? t('pvoutput.deleteMessage') : t('sources.deleteConfirm.message'), onConfirm: () => del.mutateAsync(path) });
          if (ok) { toast(t('sources.deleted', { name: d.name })); await invalidate(...refresh); }
        }}>{t('common.delete')}</Button>
        <Button size="sm" icon="edit" onClick={onEdit}>{t('common.edit')}</Button>
        <Button size="sm" icon="play" busy={run.isPending} onClick={async () => {
          try {
            const r = await run.mutateAsync(path);
            if (r.last_error) toastError(new Error(`${r.name}: ${r.last_error}`)); else toast(t('sources.ranOk', { name: r.name }));
            await invalidate(...refresh);
          } catch (e) { toastError(e); }
        }}>{t('sources.runNow')}</Button>
      </div>
    </Card>
  );
}

function initialConfig(K: KindInfo, c: DataSourceConfig | undefined): ConfigValues {
  const out: ConfigValues = {};
  for (const f of K.fields) {
    if (f.type === 'secret') { out[f.key] = ''; continue; }
    const v = c?.[f.key as keyof DataSourceConfig] ?? f.def; // secrets (write-only) are skipped above
    out[f.key] = f.type === 'bool' ? !!v : v == null ? '' : String(v);
  }
  return out;
}

function SourceDialog({ sys, direction, ds, onClose, onCreatedHosted }: {
  sys: System; direction: Direction; ds: DataSource | null; onClose: () => void; onCreatedHosted: (d: DataSource, password: string) => void;
}) {
  const { t } = useTranslation();
  const hostedFtp = useMeData().features.hosted_ftp;
  const out = direction === 'out';
  // Hosted FTP is the easiest way to connect a Solar-Log, so it is preselected.
  const available = kinds().filter((k) => k.direction === direction && (!k.hosted || hostedFtp));
  const defaultKind = out ? 'pvoutput_export' : hostedFtp ? 'solarlog_hosted' : 'solarlog_remote';
  const invalidate = useInvalidate();
  const create = useMutation(createDataSourceMutation());
  const update = useMutation(updateDataSourceMutation());
  const [K, setK] = useState<KindInfo>(kindOf(ds?.kind ?? defaultKind));
  const [name, setName] = useState(ds?.name ?? K.label);
  const [pollInterval, setPollInterval] = useState(String(ds?.poll_interval_s ?? K.interval));
  const [enabled, setEnabled] = useState(ds?.enabled ?? true);
  const [config, setConfig] = useState<ConfigValues>(() => initialConfig(K, ds?.config));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [serverError, setServerError] = useState<ServerError | null>(null);

  const fields = visibleFields(K, config);
  /** An edited source whose secret is stored: an empty field keeps it. */
  const hasStored = (f: KindField) => !!ds && f.type === 'secret' && !!ds.config[`has_${f.key}` as keyof DataSourceConfig];
  const clearError = (key: string) => setErrors(({ [key]: _, ...rest }) => rest);
  const set = (key: string, v: string | boolean) => {
    clearError(key);
    const next = { ...config, [key]: v };
    setConfig(K.adjust ? K.adjust(config, next) : next);
  };

  const pickKind = (k: KindInfo) => {
    if (name === K.label || !name) setName(k.label); // keep a name the user typed
    if (Number(pollInterval) === K.interval || !pollInterval) setPollInterval(String(k.interval));
    setConfig(initialConfig(k, undefined));
    setErrors({});
    setK(k);
  };

  const submit = async () => {
    setServerError(null);
    const errs: Record<string, string> = {};
    if (!name.trim()) errs.name = t('validation.nameRequired');
    const iv = Number(pollInterval);
    if (!pollInterval || Number.isNaN(iv) || iv < 30) errs.interval = t('sources.intervalMin');
    const cfg: DataSourceConfigWritable = {};
    for (const f of fields) {
      const v = config[f.key];
      if (f.type === 'bool') { (cfg as Record<string, unknown>)[f.key] = !!v; continue; }
      const s = String(v ?? '').trim();
      if (f.required && !s && !hasStored(f)) { errs[f.key] = t('validation.required', { label: f.label }); continue; }
      if (f.type === 'secret' && !s) continue; // empty keeps the stored secret
      if (f.type === 'number' && s) {
        const n = Number(s);
        if (!Number.isInteger(n) || (f.min != null && n < f.min) || (f.max != null && n > f.max)) { errs[f.key] = t('validation.between', { label: f.label, min: f.min, max: f.max }); continue; }
        (cfg as Record<string, unknown>)[f.key] = n;
      } else if (s || ds) (cfg as Record<string, unknown>)[f.key] = s;
    }
    setErrors(errs);
    if (Object.keys(errs).length) return;
    try {
      const body = { name: name.trim(), enabled, poll_interval_s: iv, config: cfg };
      const r = ds
        ? await update.mutateAsync({ path: { systemId: sys.id, dataSourceId: ds.id }, body })
        : await create.mutateAsync({ path: { systemId: sys.id }, body: { ...body, kind: K.kind as DataSourceKind } });
      await invalidate(...refresh);
      if (!ds && r.upload?.password) { onCreatedHosted(r, r.upload.password); return; }
      toast(ds ? t('sources.saved', { name: r.name }) : t('sources.added', { name: r.name }));
      onClose();
    } catch (err) {
      const msg = err instanceof Error ? err.message : '';
      const field = /^config\.(\w+)/.exec(msg)?.[1] ?? (msg.startsWith('poll_interval') ? 'interval' : msg.startsWith('name') ? 'name' : null);
      if (field) setErrors({ [field]: `${msg.charAt(0).toUpperCase()}${msg.slice(1)}.` }); else setServerError(serverErrorOf(err));
    }
  };

  const field = (f: KindField) => {
    const has = hasStored(f);
    if (f.type === 'bool') {
      return (
        <div key={f.key} className={f.span2 ? 'field span-2' : 'field'} style={{ alignContent: 'end' }}>
          <Checkbox label={f.label} help={f.help} checked={!!config[f.key]} onChange={(e) => set(f.key, e.target.checked)} />
        </div>
      );
    }
    if (f.type === 'select') {
      return (
        <Field key={f.key} label={f.label} required={f.required} optional={!f.required} span2={f.span2} error={errors[f.key]} help={f.help}>
          <Select options={f.options ?? []} value={String(config[f.key] ?? '')} onChange={(e) => set(f.key, e.target.value)} />
        </Field>
      );
    }
    if (f.type === 'multiline') {
      return (
        <Field key={f.key} label={f.label} required={f.required} optional={!f.required} span2={f.span2} error={errors[f.key]} help={f.help}>
          <Textarea className="mono" rows={3} spellCheck={false} placeholder={f.placeholder}
            value={String(config[f.key] ?? '')} onChange={(e) => set(f.key, e.target.value)} />
        </Field>
      );
    }
    return (
      <Field key={f.key} label={f.label} required={f.required && !has} optional={!f.required} span2={f.span2} error={errors[f.key]}
        help={f.type === 'secret' ? [f.help, has ? t('sources.secretKeep') : t('sources.secretNew')].filter(Boolean).join(' ') : f.help}>
        <Input type={f.type === 'secret' ? 'password' : f.type === 'number' ? 'number' : 'text'} min={f.min} max={f.max}
          autoComplete={f.type === 'secret' ? 'new-password' : 'off'} placeholder={has ? '••••••••' : f.placeholder}
          value={String(config[f.key] ?? '')} onChange={(e) => set(f.key, e.target.value)} />
      </Field>
    );
  };

  return (
    <FormDialog open onOpenChange={(o) => !o && onClose()} wide title={ds ? t('sources.editTitle', { name: ds.name }) : t(out ? 'pvoutput.add' : 'sources.add')}
      subtitle={t(out ? 'pvoutput.dialogSubtitle' : 'sources.dialogSubtitle')}
      submitLabel={ds ? t(out ? 'pvoutput.save' : 'sources.save') : t(out ? 'pvoutput.add' : 'sources.add')} busy={create.isPending || update.isPending} serverError={serverError} onSubmit={submit}>
      {/* No choice to make for an existing source or a single kind (PVOutput). */}
      {ds || available.length === 1 ? (
        <p className="sm">{K.label} <span className="muted">· {K.transport}</span></p>
      ) : (
        <Fieldset legend={t('sources.loggerType')}>
          <div className="kind-grid">
            {available.map((k) => (
              <label key={k.kind} className="kind-option">
                <input type="radio" name="kind" value={k.kind} checked={k.kind === K.kind} disabled={!k.supported} onChange={() => pickKind(k)} />
                <span className="kind-text">
                  <span className="kind-title">{k.label}</span>
                  <span className="xs muted">{k.transport}</span>
                  <span className="sm muted">{k.desc}</span>
                </span>
                {k.supported ? <Badge tone="success" className="kind-badge">{t('sources.supported')}</Badge> : <span className="soon kind-badge">{t('sources.comingSoon')}</span>}
              </label>
            ))}
          </div>
          <p className="help">{t('sources.notListed')} <Link to={`/s/${sys.id}/settings/push`} onClick={onClose}>{t('sources.apiKey')}</Link>.</p>
        </Fieldset>
      )}
      <FormGrid>
        <Field label={t('common.name')} required error={errors.name}><Input maxLength={200} value={name} onChange={(e) => { setName(e.target.value); clearError('name'); }} /></Field>
        {/* Hosted sources are imported after each upload. */}
        {!K.hosted && (
          <Field label={t(out ? 'pvoutput.interval' : 'sources.pollInterval')} required error={errors.interval} help={`${t('sources.pollHelp')} ${K.intervalHelp ?? ''}`}>
            <Input type="number" min={30} step={1} addon={t('sources.seconds')} value={pollInterval} onChange={(e) => { setPollInterval(e.target.value); clearError('interval'); }} />
          </Field>
        )}
      </FormGrid>
      <Fieldset legend={t('sources.connection')}>
        {K.hosted && !ds && <p className="sm muted">{t('sources.hosted.noConfig')}</p>}
        <FormGrid>{fields.map(field)}</FormGrid>
      </Fieldset>
      <Switch label={t('sources.enabled')} checked={enabled} onCheckedChange={setEnabled} />
    </FormDialog>
  );
}

function DailyImport({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const invalidate = useInvalidate();
  const ingest = useMutation(ingestDailyMutation());
  const [csv, setCsv] = useState('');
  const [createInv, setCreateInv] = useState(false);
  const [error, setError] = useState<string>();
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const [result, setResult] = useState<{ accepted: number; inverters_created: number } | null>(null);
  const file = useRef<HTMLInputElement>(null);

  return (
    <section className="section card" aria-labelledby="imp-h">
      <form className="form" noValidate onSubmit={async (e) => {
        e.preventDefault();
        setServerError(null); setResult(null);
        const parsed = parseDailyCsv(csv);
        if ('error' in parsed) { setError(parsed.error); return; }
        try {
          setResult(await ingest.mutateAsync({ path: { systemId: sys.id }, body: { days: parsed.rows, create_inverters: createInv } }));
          await invalidate('getOverview', 'listInverters', 'getDashboard');
        } catch (err) { setServerError(serverErrorOf(err)); }
      }}>
        {serverError && <Banner tone="danger" role="alert" title={serverError.title}>{serverError.message}</Banner>}
        <div className="grid gap-1">
          <h2 id="imp-h">{t('sources.import.title')}</h2>
          <p className="sm muted">{t('sources.import.intro')}</p>
        </div>
        <Field label={t('sources.import.rows')} required error={error} help={t('sources.import.help')}>
          <Textarea value={csv} onChange={(e) => { setCsv(e.target.value); setError(undefined); setServerError(null); }}
            placeholder={'inverter,date,energy_wh,pac_max_w\n1,2024-06-01,48210,8120\n2,2024-06-01,47655'} />
        </Field>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap items-center gap-4">
            <Button size="sm" icon="upload" onClick={() => file.current?.click()}>{t('sources.import.loadFile')}</Button>
            <input ref={file} type="file" accept=".csv,text/csv,text/plain" className="sr-only" tabIndex={-1} onChange={(e) => {
              const f = e.target.files?.[0];
              if (!f) return;
              f.text().then((txt) => { setCsv(txt); setError(undefined); setServerError(null); toast(t('sources.import.loaded', { name: f.name })); });
              e.target.value = '';
            }} />
            <Checkbox label={t('sources.import.createInverters')} checked={createInv} onChange={(e) => setCreateInv(e.target.checked)} />
          </div>
          <Button type="submit" variant="primary" busy={ingest.isPending}>{t('sources.import.submit')}</Button>
        </div>
        <div aria-live="polite">
          {result && (
            <Banner tone="info" icon="check" title={t('sources.import.imported', { days: t('counts.day', { count: result.accepted }) })}>
              {result.inverters_created
                ? t('sources.import.createdInverters', { inverters: t('counts.inverter', { count: result.inverters_created }) })
                : t('sources.import.includes')}
            </Banner>
          )}
        </div>
      </form>
    </section>
  );
}
