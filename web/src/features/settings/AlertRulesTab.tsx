import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Navigate, useParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import { listAlertRulesOptions, setAlertRulesMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { AlertRule, System } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { Button } from '@/components/ui/Button';
import { BackLink, NavList, NavRow } from '@/components/ui/NavList';
import { useUnsavedChanges } from '@/components/ui/Confirm';
import { Field, Input, Switch } from '@/components/ui/form';
import { Badge, Banner, Card, Skeleton } from '@/components/ui/primitives';
import { toast } from '@/components/ui/toast';
import { num } from '@/lib/format';
import { serverErrorOf, type ServerError } from '@/lib/forms';
import { GROUP_KEYS, groupOf, type AlertGroup } from './alertGroups';

/** Alert rules in groups: a list of groups, each opening its rules. */
export function AlertRulesTab({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const { '*': group = '' } = useParams();
  const q = useQuery(listAlertRulesOptions({ path: { systemId: sys.id } }));
  if (q.isError) return <Banner tone="danger" title={t('alerts.rules.loadFailed')}>{q.error.message}</Banner>;
  if (!q.data) return <Skeleton style={{ height: 320, maxWidth: 880 }} />;
  const base = `/s/${sys.id}/settings/alerts`;
  if (!group) return <Groups sys={sys} rules={q.data} base={base} />;
  if (!GROUP_KEYS.includes(group as AlertGroup) || !q.data.some((r) => groupOf(r.kind) === group)) return <Navigate to={base} replace />;
  return <RulesForm key={group} sys={sys} initial={q.data} group={group as AlertGroup} base={base} />;
}

function Groups({ sys, rules, base }: { sys: System; rules: AlertRule[]; base: string }) {
  const { t } = useTranslation();
  const me = useMeData();
  const member = me.systems.some((m) => m.system_id === sys.id);
  return (
    <div className="settings-grid">
      <NavList title={t('alerts.rules.title')} intro={t('alerts.rules.intro')}>
        {GROUP_KEYS.map((g) => {
          const rs = rules.filter((r) => groupOf(r.kind) === g);
          if (!rs.length) return null;
          return <NavRow key={g} to={`${base}/${g}`} title={t(`alerts.rules.groups.${g}`)}
            sub={t('alerts.rules.enabledCount', { on: rs.filter((r) => r.enabled).length, count: rs.length })} />;
        })}
      </NavList>
      {member && (
        <NavList>
          <NavRow to={`/profile/notifications/systems/${sys.id}`} title={t('alerts.rules.yourNotifications')} sub={t('alerts.rules.yourNotificationsHelp')} />
        </NavList>
      )}
    </div>
  );
}

type Row = { kind: string; enabled: boolean; params: Record<string, string> };
// Stable field order (the API returns maps).
const PARAM_ORDER = ['minutes', 'hours', 'percent', 'percent_of_expected', 'min_sun_elevation', 'min_kwh_per_kwp', 'min_expected_kwh_per_kwp', 'minutes_per_day', 'days'];
const byOrder = ([a]: [string, unknown], [b]: [string, unknown]) => PARAM_ORDER.indexOf(a) - PARAM_ORDER.indexOf(b);
const toRows = (rules: AlertRule[], from: 'params' | 'defaults' = 'params'): Row[] =>
  rules.map((r) => ({ kind: r.kind, enabled: from === 'defaults' || r.enabled, params: Object.fromEntries(Object.entries(r[from]).sort(byOrder).map(([k, v]) => [k, String(v)])) }));
const sevTone = { error: 'danger', warning: 'warn', info: 'accent' } as const;

function RulesForm({ sys, initial, group, base }: { sys: System; initial: AlertRule[]; group: AlertGroup; base: string }) {
  const { t } = useTranslation();
  const save = useMutation(setAlertRulesMutation());
  const invalidate = useInvalidate();
  const [rows, setRows] = useState<Row[]>(() => toRows(initial));
  const [dirty, setDirty] = useState(false);
  const [errors, setErrors] = useState<Record<string, boolean>>({});
  const [serverError, setServerError] = useState<ServerError | null>(null);
  useUnsavedChanges(dirty);
  const update = (i: number, r: Row) => { setRows(rows.map((x, k) => (k === i ? r : x))); setDirty(true); };

  return (
    <div className="settings-grid">
      <Card flush as="section">
        <form noValidate onSubmit={async (e) => {
          e.preventDefault();
          setServerError(null);
          const errs: Record<string, boolean> = {};
          rows.forEach((r) => Object.entries(r.params).forEach(([k, v]) => {
            if (v.trim() === '' || Number.isNaN(Number(v)) || Number(v) < 0) errs[`${r.kind}.${k}`] = true;
          }));
          setErrors(errs);
          if (Object.keys(errs).length) return;
          try {
            await save.mutateAsync({
              path: { systemId: sys.id },
              body: rows.map((r) => ({ kind: r.kind, enabled: r.enabled, params: Object.fromEntries(Object.entries(r.params).map(([k, v]) => [k, Number(v)])) })),
            });
            await invalidate('listAlertRules');
            setDirty(false);
            toast(t('alerts.rules.saved'));
          } catch (err) { setServerError(serverErrorOf(err)); }
        }}>
          <div className="grid gap-1 p-5">
            <BackLink to={base}>{t('alerts.rules.title')}</BackLink>
            {serverError && <Banner tone="danger" role="alert" title={serverError.title}>{serverError.message}</Banner>}
            <h2>{t(`alerts.rules.groups.${group}`)}</h2>
          </div>
          {rows.map((r, i) => {
            if (groupOf(r.kind) !== group) return null;
            const rule = initial.find((x) => x.kind === r.kind)!;
            return (
              <div key={r.kind} className="rule-row">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="grid gap-1">
                    <span className="flex flex-wrap items-center gap-2">
                      <strong>{t(`alerts.rules.kinds.${r.kind}.title`, { defaultValue: r.kind })}</strong>
                      <Badge tone={sevTone[rule.severity]}>{t(`alerts.severity.${rule.severity}`)}</Badge>
                    </span>
                    <span className="sm muted">{t(`alerts.rules.kinds.${r.kind}.help`, { defaultValue: '' })}</span>
                  </div>
                  <Switch label={<span className="sr-only">{t(`alerts.rules.kinds.${r.kind}.title`, { defaultValue: r.kind })}</span>}
                    checked={r.enabled} onCheckedChange={(v) => update(i, { ...r, enabled: v })} />
                </div>
                {r.enabled && Object.keys(r.params).length > 0 && (
                  <div className="rule-params">
                    {Object.entries(r.params).map(([k, v]) => (
                      <Field key={k} label={t(`alerts.rules.params.${k}`, { defaultValue: k })} help={t('alerts.rules.default', { value: num(rule.defaults[k], Number.isInteger(rule.defaults[k]) ? 0 : 1) })}
                        error={errors[`${r.kind}.${k}`] ? t('alerts.rules.invalid') : undefined}>
                        <Input type="number" min={0} step="any" inputMode="decimal" value={v}
                          onChange={(e) => update(i, { ...r, params: { ...r.params, [k]: e.target.value } })} />
                      </Field>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
          <div className="form-actions p-5">
            <Button onClick={() => {
              const defaults = toRows(initial, 'defaults');
              setRows(rows.map((r, i) => (groupOf(r.kind) === group ? defaults[i] : r)));
              setErrors({}); setDirty(true);
            }}>{t('alerts.rules.reset')}</Button>
            <Button type="submit" variant="primary" busy={save.isPending} disabled={!dirty}>{t('common.saveChanges')}</Button>
          </div>
        </form>
      </Card>
    </div>
  );
}
