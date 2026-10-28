import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ackAlertMutation, listAlertsOptions, snoozeAlertMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { Alert, System } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { can } from '@/auth/scopes';
import { SectionHead } from '@/components/layout/Page';
import { Button } from '@/components/ui/Button';
import { Icon } from '@/components/ui/Icon';
import { Badge, Card } from '@/components/ui/primitives';
import { toast, toastError } from '@/components/ui/toast';
import { fmtDateTime, fmtRel } from '@/lib/format';
import { SeverityIcon } from './NotificationList';

const DAY = 24 * 60 * 60 * 1000;

/** Open alerts of a system, with acknowledge/snooze for alerts:manage. */
export function AlertsSection({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const q = useQuery({ ...listAlertsOptions({ path: { systemId: sys.id } }), refetchInterval: 60_000 });
  const items = q.data?.items ?? [];
  return (
    <section className="section" aria-labelledby="al-h">
      <SectionHead title={t('alerts.open')} id="al-h" />
      <Card flush>
        {items.length ? items.map((a) => <AlertRow key={a.id} sys={sys} a={a} />) : (
          <div className="attention-item">
            <span className="status ok"><Icon name="check" /></span>
            <div className="body"><strong>{t('alerts.none')}</strong><span className="muted">{t('alerts.noneText')}</span></div>
          </div>
        )}
      </Card>
    </section>
  );
}

function AlertRow({ sys, a }: { sys: System; a: Alert }) {
  const { t } = useTranslation();
  const me = useMeData();
  const invalidate = useInvalidate();
  const path = { path: { systemId: sys.id, alertId: a.id } };
  const ack = useMutation(ackAlertMutation());
  const snooze = useMutation(snoozeAlertMutation());
  const manage = can(me, sys.id, 'alerts:manage');
  const [now] = useState(Date.now);
  const snoozed = a.snoozed_until && new Date(a.snoozed_until).getTime() > now;
  const refresh = () => invalidate('listAlerts');
  return (
    <div className="attention-item">
      <SeverityIcon severity={a.severity} />
      <div className="body">
        <strong>{a.title}</strong>
        <span className="muted">{a.body}</span>
        <span className="xs muted flex flex-wrap items-center gap-2">
          {t('alerts.since', { when: fmtRel(a.opened_at) })}
          {a.acked_at && <Badge tone="outline">{t('alerts.acked')}</Badge>}
          {snoozed && <Badge tone="outline">{t('alerts.snoozed', { when: fmtDateTime(a.snoozed_until, sys.timezone) })}</Badge>}
        </span>
      </div>
      {manage && (
        <div className="attention-actions">
          {!a.acked_at && <Button size="sm" busy={ack.isPending} onClick={() => ack.mutateAsync(path).then(refresh).then(() => toast(t('alerts.ackedToast')), toastError)}>{t('alerts.ack')}</Button>}
          <Button size="sm" variant="ghost" busy={snooze.isPending} onClick={() => snooze.mutateAsync({ ...path, body: { until: snoozed ? null : new Date(Date.now() + DAY).toISOString() } })
            .then(refresh).then(() => toast(snoozed ? t('alerts.unsnoozedToast') : t('alerts.snoozedToast')), toastError)}>
            {snoozed ? t('alerts.unsnooze') : t('alerts.snooze')}
          </Button>
        </div>
      )}
    </div>
  );
}
