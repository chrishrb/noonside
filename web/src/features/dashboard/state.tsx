import { useTranslation } from 'react-i18next';
import type { DashboardSystem } from '@/api/types';
import { Badge } from '@/components/ui/primitives';

export type SystemState = 'err' | 'ok' | 'idle';

/** Error beats producing; no power (e.g. at night) is off. */
export const systemState = (s: DashboardSystem): SystemState =>
  s.inverters_with_error ? 'err' : s.pac_w > 0 ? 'ok' : 'idle';

export function StateBadge({ s }: { s: DashboardSystem }) {
  const { t } = useTranslation();
  switch (systemState(s)) {
    case 'err': return <Badge tone="danger" dot>{t('counts.error', { count: s.inverters_with_error })}</Badge>;
    case 'ok': return <Badge tone="success" dot>{t('dashboard.producing')}</Badge>;
    default: return <Badge tone="outline">{s.inverters_total ? t('dashboard.idle') : t('dashboard.noInverters')}</Badge>;
  }
}
