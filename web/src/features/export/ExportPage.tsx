import { useState } from 'react';
import { useParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';
import { fetchText } from '@/api/client';
import { useSystem } from '@/api/queries';
import type { System } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { can } from '@/auth/scopes';
import { ApiErrorPanel, Content, PageHeader, QueryView } from '@/components/layout/Page';
import { Button } from '@/components/ui/Button';
import { Chip, Field, Fieldset, FormGrid, Input, Segmented } from '@/components/ui/form';
import { Banner, Card, CardHead, EmptyState } from '@/components/ui/primitives';
import { Table } from '@/components/ui/Table';
import { toast } from '@/components/ui/toast';
import { ApiError } from '@/api/client';
import { csvSplit } from '@/lib/csv';
import { addDays, parseYmd, todayIn, ymd } from '@/lib/format';
import { serverErrorOf, type ServerError } from '@/lib/forms';
import { commissioned } from '@/lib/system';

type Gran = 'day' | 'month' | 'year' | '5min' | 'minute';
// Power exports (5min, minute) cover at most this many days (server limit).
const MAX_POWER_DAYS = 31;
const isPower = (g: Gran) => g === '5min' || g === 'minute';
interface Preset { key: string; label: string; gran: Gran; from: string; to: string }
const PREVIEW_ROWS = 12;

function presets(sys: System): Preset[] {
  const t = parseYmd(todayIn(sys.timezone));
  const month = (y: number, m: number) => ymd(m < 1 ? { year: y - 1, month: m + 12, day: 1 } : m > 12 ? { year: y + 1, month: m - 12, day: 1 } : { year: y, month: m, day: 1 });
  const jan1 = (y: number) => ymd({ year: y, month: 1, day: 1 });
  return [
    { key: 'this-month', label: i18n.t('period.current.month'), gran: 'day', from: month(t.year, t.month), to: month(t.year, t.month + 1) },
    { key: 'last-month', label: i18n.t('export.lastMonth'), gran: 'day', from: month(t.year, t.month - 1), to: month(t.year, t.month) },
    { key: 'this-year', label: i18n.t('period.current.year'), gran: 'month', from: jan1(t.year), to: jan1(t.year + 1) },
    { key: 'last-year', label: i18n.t('export.lastYear'), gran: 'month', from: jan1(t.year - 1), to: jan1(t.year) },
    { key: 'all', label: i18n.t('export.sinceCommissioning'), gran: 'year', from: commissioned(sys), to: jan1(t.year + 1) },
  ];
}

const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'system';

export function ExportPage() {
  const sid = useParams().sid!;
  const sys = useSystem(sid);
  const me = useMeData();
  if (!can(me, sid, 'data:export')) return <ApiErrorPanel error={new ApiError(403, 'missing scope data:export')} />;
  return <QueryView queries={[sys]}>{() => <Export sys={sys.data!} />}</QueryView>;
}

function Export({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const all = presets(sys);
  const [preset, setPreset] = useState<string | null>('this-month');
  const [gran, setGran] = useState<Gran>(all[0].gran);
  const [from, setFrom] = useState(all[0].from);
  const [to, setTo] = useState(all[0].to);
  const [fieldErr, setFieldErr] = useState<{ from?: string; to?: string }>({});
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const [busy, setBusy] = useState<'preview' | 'download' | null>(null);
  const [preview, setPreview] = useState<{ header: string[]; rows: string[][]; count: number; filename: string } | null>(null);

  const choose = (p: Preset) => { setPreset(p.key); setGran(p.gran); setFrom(p.from); setTo(p.to); setFieldErr({}); };

  const run = async (mode: 'preview' | 'download') => {
    setServerError(null);
    const errs: typeof fieldErr = {};
    if (!from) errs.from = t('export.fromRequired');
    if (!to) errs.to = t('export.toRequired');
    else if (from && to <= from) errs.to = t('export.toAfterFrom');
    else if (from && isPower(gran) && to > addDays(from, MAX_POWER_DAYS)) errs.to = t('export.powerRange', { days: MAX_POWER_DAYS });
    setFieldErr(errs);
    if (errs.from || errs.to) return;
    setBusy(mode);
    try {
      const { text } = await fetchText(`/systems/${sys.id}/export`, { granularity: gran, from, to });
      const lines = text.trim().split('\n').filter(Boolean);
      const filename = `${slug(sys.name)}_${gran}_${from}_${addDays(to, -1)}.csv`;
      setPreview({ header: csvSplit(lines[0] ?? ''), rows: lines.slice(1, PREVIEW_ROWS + 1).map(csvSplit), count: Math.max(0, lines.length - 1), filename });
      if (mode === 'download') {
        const url = URL.createObjectURL(new Blob([text], { type: 'text/csv' }));
        const a = document.createElement('a');
        a.href = url; a.download = filename;
        document.body.appendChild(a); a.click(); a.remove();
        setTimeout(() => URL.revokeObjectURL(url), 1000);
        toast(t('export.downloaded', { filename }));
      }
    } catch (e) {
      setServerError(serverErrorOf(e));
    } finally {
      setBusy(null);
    }
  };

  return (
    <Content>
      <PageHeader eyebrow={sys.name} title={t('export.title')} sub={t('export.intro')} />
      <div className="grid-2 export-grid items-start">
        <form className="card form" noValidate onSubmit={(e) => { e.preventDefault(); void run('download'); }}>
          {serverError && <Banner tone="danger" role="alert" title={serverError.title}>{serverError.message}</Banner>}
          <Fieldset legend={t('export.granularity')}>
            <Segmented label={t('export.granularity')} value={gran} onChange={(g) => { setGran(g); setPreset(null); }}
              options={[['day', t('period.name.day')], ['month', t('period.name.month')], ['year', t('period.name.year')]]} />
            <Segmented label={t('export.power')} value={gran} onChange={(g) => { setGran(g); setPreset(null); }}
              options={[['5min', t('export.fiveMin')], ['minute', t('export.minute')]]} />
            <p className="help">{isPower(gran) ? t('export.powerHelp', { days: MAX_POWER_DAYS }) : t('export.energyHelp')}</p>
          </Fieldset>
          <Fieldset legend={t('export.range')}>
            <div className="chips">{all.map((p) => <Chip key={p.key} pressed={preset === p.key} onClick={() => choose(p)}>{p.label}</Chip>)}</div>
            <FormGrid>
              <Field label={t('status.filters.from')} required error={fieldErr.from}>
                <Input type="date" value={from} onChange={(e) => { setFrom(e.target.value); setPreset(null); }} />
              </Field>
              <Field label={t('export.toExclusive')} required help={t('export.toHelp')} error={fieldErr.to}>
                <Input type="date" value={to} onChange={(e) => { setTo(e.target.value); setPreset(null); }} />
              </Field>
            </FormGrid>
          </Fieldset>
          <div className="form-actions start">
            <Button type="submit" variant="primary" icon="download" busy={busy === 'download'}>{t('export.download')}</Button>
            <Button busy={busy === 'preview'} onClick={() => run('preview')}>{t('export.preview')}</Button>
          </div>
        </form>
        <Card flush as="section" aria-labelledby="prev-h">
          <CardHead title={t('export.preview')} id="prev-h">
            <span className="sm muted">{preview ? `${t('yield.table.rows', { count: preview.count })} · ${preview.filename}` : t('export.chooseRange')}</span>
          </CardHead>
          {!preview ? (
            <EmptyState icon="download" title={t('export.nothingYet')} text={t('export.nothingYetText')} />
          ) : preview.count === 0 ? (
            <EmptyState icon="info" title={t('export.noData')} text={t('export.noDataText')} />
          ) : (
            <>
              <Table>
                <thead><tr>{preview.header.map((h, i) => <th scope="col" key={i} className={i === preview.header.length - 1 ? 'num' : undefined}>{h}</th>)}</tr></thead>
                <tbody>
                  {preview.rows.map((r, n) => (
                    <tr key={n}>{r.map((c, i) => <td key={i} className={i === r.length - 1 ? 'num' : i === 1 ? 'mono xs whitespace-nowrap' : 'whitespace-nowrap'}>{c}</td>)}</tr>
                  ))}
                </tbody>
              </Table>
              {preview.count > PREVIEW_ROWS && <p className="sm muted" style={{ padding: '12px 16px' }}>{t('export.moreRows', { count: preview.count - PREVIEW_ROWS })}</p>}
            </>
          )}
        </Card>
      </div>
    </Content>
  );
}
