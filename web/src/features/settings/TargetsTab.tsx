import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { estimateTargetsMutation, getTargetsOptions, setTargetsMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { MonthlyTargets, System } from '@/api/types';
import { Button } from '@/components/ui/Button';
import { useUnsavedChanges } from '@/components/ui/Confirm';
import { Field, Input } from '@/components/ui/form';
import { Banner, Card, Skeleton } from '@/components/ui/primitives';
import { toast } from '@/components/ui/toast';
import { fmtKWh, fmtKwp, monthNames } from '@/lib/format';
import { serverErrorOf, type ServerError } from '@/lib/forms';
import { estimateTargets } from '@/lib/period';

export function TargetsTab({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const q = useQuery(getTargetsOptions({ path: { systemId: sys.id } }));
  if (q.isError) return <Banner tone="danger" title={t('targets.loadFailed')}>{q.error.message}</Banner>;
  if (!q.data) return <Skeleton style={{ height: 320, maxWidth: 880 }} />;
  return <TargetsForm sys={sys} initial={q.data.target_kwh} />;
}

function TargetsForm({ sys, initial }: { sys: System; initial: number[] }) {
  const { t } = useTranslation();
  const MONTHS = monthNames();
  const MON = monthNames('short');
  const save = useMutation(setTargetsMutation());
  const pvgis = useMutation(estimateTargetsMutation());
  const located = sys.latitude != null && sys.longitude != null;
  const invalidate = useInvalidate();
  const [vals, setVals] = useState<string[]>(() => initial.map(String));
  const [errors, setErrors] = useState<Record<number, string>>({});
  const [serverError, setServerError] = useState<ServerError | null>(null);

  const nums = vals.map((v) => Number(v) || 0);
  useUnsavedChanges(vals.some((v, i) => v !== String(initial[i])));
  const max = Math.max(1, ...nums);
  const set = (i: number, v: string) => { setVals(vals.map((x, k) => (k === i ? v : x))); setErrors((e) => ({ ...e, [i]: '' })); };

  return (
    <div className="settings-grid">
      <Card as="section">
        <form className="form" noValidate onSubmit={async (e) => {
          e.preventDefault();
          setServerError(null);
          const errs: Record<number, string> = {};
          vals.forEach((v, i) => {
            if (v.trim() === '') errs[i] = t('targets.required', { month: MONTHS[i] });
            else if (Number.isNaN(Number(v)) || Number(v) < 0) errs[i] = t('targets.nonNegative');
          });
          setErrors(errs);
          if (Object.values(errs).some(Boolean)) return;
          try {
            await save.mutateAsync({ path: { systemId: sys.id }, body: { target_kwh: nums as MonthlyTargets['target_kwh'] } });
            await invalidate('getTargets', 'getOverview', 'getDashboard');
            toast(t('targets.saved'));
          } catch (err) { setServerError(serverErrorOf(err)); }
        }}>
          {serverError && <Banner tone="danger" role="alert" title={serverError.title}>{serverError.message}</Banner>}
          <div className="grid gap-1">
            <h2>{t('targets.title')}</h2>
            <p className="sm muted">{t('targets.intro')}</p>
          </div>
          <div className="targets-grid">
            {MONTHS.map((m, i) => (
              <Field key={m} label={MON[i]} error={errors[i] || undefined}>
                <Input type="number" min={0} step="any" aria-label={t('targets.aria', { month: m })} value={vals[i]} onChange={(e) => set(i, e.target.value)} />
              </Field>
            ))}
          </div>
          <div>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="sm muted">{t('targets.allKwh')}</span>
              <strong className="tnum">{t('targets.perYear', { value: fmtKWh(nums.reduce((a, x) => a + x, 0)) })}</strong>
            </div>
            <div className="target-bars" aria-hidden="true">
              {nums.map((x, i) => <div key={i}><span className="b" style={{ height: `${(x / max) * 100}%` }} /><span className="m">{MON[i][0]}</span></div>)}
            </div>
          </div>
          <p className="xs muted">
            {located ? t(sys.planes.length ? 'targets.pvgisPlanes' : 'targets.pvgisOptimal') : t('targets.pvgisNoLocation')}
            {' · '}<a href="https://joint-research-centre.ec.europa.eu/photovoltaic-geographical-information-system-pvgis_en" target="_blank" rel="noreferrer">{t('targets.pvgisAttribution')}</a>
          </p>
          <div className="form-actions">
            <Button onClick={() => { setVals(estimateTargets(sys.peak_power_kwp).map(String)); setErrors({}); toast(t('targets.estimated')); }}>
              {t('targets.estimate', { kwp: fmtKwp(sys.peak_power_kwp) })}
            </Button>
            <Button icon="sun" disabled={!located} busy={pvgis.isPending} onClick={async () => {
              setServerError(null);
              try {
                const r = await pvgis.mutateAsync({ path: { systemId: sys.id } });
                setVals(r.target_kwh.map(String));
                setErrors({});
                toast(t('targets.pvgisDone'));
              } catch (err) { setServerError(serverErrorOf(err)); }
            }}>{t('targets.pvgis')}</Button>
            <Button type="submit" variant="primary" busy={save.isPending}>{t('targets.save')}</Button>
          </div>
        </form>
      </Card>
    </div>
  );
}
