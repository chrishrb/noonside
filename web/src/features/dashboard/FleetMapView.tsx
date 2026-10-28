import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import type { DashboardSystem, System } from '@/api/types';
import { useMeData } from '@/auth/useMe';
import { can } from '@/auth/scopes';
import { FleetMap, type MapPoint } from '@/components/data/LazyMap';
import { fmtKWh, fmtKwp, fmtW } from '@/lib/format';
import { latLon } from '@/lib/map';
import { StateBadge, systemState } from './state';
import { Freshness } from '@/components/data/Freshness';

/** The systems on a map, colored by state; systems without coordinates are listed below it. */
export function FleetMapView({ rows, byId }: { rows: DashboardSystem[]; byId: Map<string, System> }) {
  const { t } = useTranslation();
  const me = useMeData();
  const points: MapPoint[] = [];
  const missing: DashboardSystem[] = [];
  for (const s of rows) {
    const sys = byId.get(s.system_id);
    const pos = latLon(sys?.latitude, sys?.longitude);
    if (!pos) { missing.push(s); continue; }
    points.push({ ...pos, id: s.system_id, name: s.name, tone: systemState(s), popup: <Popup s={s} sys={sys} /> });
  }
  return (
    <div className="card card-flush fleet-map">
      <FleetMap points={points} label={t('dashboard.fleet.mapLabel')} />
      <div className="fleet-map-foot">
        <ul className="map-legend" aria-label={t('dashboard.fleet.legend')}>
          <li><span className="map-dot tone-ok" />{t('dashboard.producing')}</li>
          <li><span className="map-dot tone-idle" />{t('dashboard.idle')}</li>
          <li><span className="map-dot tone-err" />{t('dashboard.fleet.error')}</li>
        </ul>
        {missing.length > 0 && (
          <p className="sm muted">
            {t('dashboard.fleet.notOnMap', { count: missing.length })}{' '}
            {missing.map((s, i) => (
              <span key={s.system_id}>
                {i > 0 && ', '}
                {can(me, s.system_id, 'system:write')
                  ? <Link to={`/s/${s.system_id}/settings/general`}>{s.name}</Link>
                  : s.name}
              </span>
            ))}
          </p>
        )}
      </div>
    </div>
  );
}

function Popup({ s, sys }: { s: DashboardSystem; sys?: System }) {
  const { t } = useTranslation();
  return (
    <div className="map-popup">
      <Link className="map-popup-title" to={`/s/${s.system_id}/overview`}>{s.name}</Link>
      {sys?.location && <span className="xs muted">{sys.location}</span>}
      <div className="flex flex-wrap items-center gap-2"><StateBadge s={s} /><span className="xs muted">{fmtKwp(s.peak_power_kwp)}</span></div>
      <dl className="map-popup-stats">
        <div><dt>{t('kpi.currentPower')}</dt><dd>{fmtW(s.pac_w)}</dd></div>
        <div><dt>{t('period.current.day')}</dt><dd>{fmtKWh(s.today_kwh)}</dd></div>
      </dl>
      <Freshness className="xs muted" time={s.last_update} stale={s.stale} tz={sys?.timezone} format={(when) => t('dashboard.fleet.popupUpdated', { when })} />
    </div>
  );
}
