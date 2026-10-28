import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { resetDataSourcePasswordMutation } from '@/api/gen/@tanstack/react-query.gen';
import type { DataSource, System } from '@/api/types';
import { Button } from '@/components/ui/Button';
import { useConfirm } from '@/components/ui/Confirm';
import { FormDialog } from '@/components/ui/Dialog';
import { copyText, SecretReveal } from '@/components/ui/misc';
import { Badge, Banner, ProgressBar, Stat } from '@/components/ui/primitives';
import { fmtBytes } from '@/lib/format';

type Upload = NonNullable<DataSource['upload']>;

/** The FTP account of a hosted (solarlog_hosted) source on its card. */
export function HostedPanel({ sys, d, onPassword }: { sys: System; d: DataSource; onPassword: (d: DataSource, password: string) => void }) {
  const { t } = useTranslation();
  const confirm = useConfirm();
  const reset = useMutation(resetDataSourcePasswordMutation());
  const up = d.upload;
  if (!up) return null;
  const pct = up.quota_bytes ? (up.used_bytes / up.quota_bytes) * 100 : 0;
  return (
    <div className="grid gap-4">
      <div className="grid gap-3 sm:grid-cols-2">
        <Stat label={t('sources.hosted.server')}><Copyable value={up.host} /></Stat>
        <Stat label={t('sources.hosted.username')}><Copyable value={up.username} /></Stat>
      </div>
      <div className="ds-meta">
        <Stat label={t('sources.hosted.port')}><span className="mono font-normal">{up.port}</span></Stat>
        <Stat label={t('sources.hosted.directory')}><span className="mono font-normal">{up.directory}</span></Stat>
      </div>
      {up.quota_bytes > 0 && (
        <div className="grid gap-1">
          <div className="flex flex-wrap items-center justify-between gap-2 sm">
            <span className="muted">{t('sources.hosted.usage')}</span>
            <span className="flex items-center gap-2">
              {up.locked && <Badge tone="danger" dot>{t('sources.hosted.locked')}</Badge>}
              {t('sources.hosted.usageOf', { used: fmtBytes(up.used_bytes), quota: fmtBytes(up.quota_bytes) })}
            </span>
          </div>
          <ProgressBar value={pct} tone={up.locked || pct >= 80 ? 'low' : 'accent'} />
        </div>
      )}
      {up.locked && (
        <Banner tone="danger" title={t('sources.hosted.lockedTitle')}>{t('sources.hosted.lockedText', { days: up.retention_days })}</Banner>
      )}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="xs muted" style={{ maxWidth: '60ch' }}>{t('sources.hosted.cleartext')}</p>
        <Button size="sm" icon="key" busy={reset.isPending} onClick={async () => {
          let fresh: DataSource | undefined;
          const ok = await confirm({
            title: t('sources.hosted.resetConfirm.title'), danger: true, confirmLabel: t('sources.hosted.resetConfirm.label'),
            message: t('sources.hosted.resetConfirm.message'),
            onConfirm: async () => { fresh = await reset.mutateAsync({ path: { systemId: sys.id, dataSourceId: d.id } }); },
          });
          if (ok && fresh?.upload?.password) onPassword(fresh, fresh.upload.password);
        }}>{t('sources.hosted.reset')}</Button>
      </div>
    </div>
  );
}

function Copyable({ value }: { value: string }) {
  const { t } = useTranslation();
  return (
    <span className="flex min-w-0 items-center gap-1">
      <span className="mono block truncate font-normal" title={value}>{value}</span>
      <Button variant="ghost" size="sm" iconOnly icon="copy" label={`${t('common.copy')}: ${value}`} onClick={() => copyText(value)} />
    </span>
  );
}

/** Shows the account with its password once, after creating or resetting it. */
export function CredentialsDialog({ d, password, reset, onClose }: { d: DataSource; password: string; reset?: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const up = d.upload as Upload;
  return (
    <FormDialog open onOpenChange={(o) => !o && onClose()} wide hideCancel submitLabel={t('sources.hosted.done')}
      title={reset ? t('sources.hosted.resetTitle') : t('sources.hosted.createdTitle')} subtitle={d.name}>
      <Banner tone="warn" title={t('sources.hosted.onceTitle')}>{t('sources.hosted.onceText')}</Banner>
      <SecretReveal label={t('sources.hosted.server')} value={up.host} />
      <SecretReveal label={t('sources.hosted.username')} value={up.username} />
      <SecretReveal label={t('sources.hosted.password')} value={password} />
      <div className="ds-meta">
        <Stat label={t('sources.hosted.port')}><span className="mono font-normal">{up.port}</span></Stat>
        <Stat label={t('sources.hosted.directory')}><span className="mono font-normal">{up.directory}</span></Stat>
      </div>
      <Banner tone="info" title={t('sources.hosted.setupTitle')}>
        <p>{t('sources.hosted.setup', { port: up.port })}</p>
        <p className="xs muted">{t('sources.hosted.limits', { days: up.retention_days, quota: fmtBytes(up.quota_bytes) })} {t('sources.hosted.cleartext')}</p>
      </Banner>
    </FormDialog>
  );
}
