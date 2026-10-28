import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { getAdviceOptions } from '@/api/gen/@tanstack/react-query.gen';
import type { Advice, AdviceWindow, System } from '@/api/types';
import { Icon } from '@/components/ui/Icon';
import { Badge } from '@/components/ui/primitives';
import { fmtKWh, fmtTime, fmtW } from '@/lib/format';
import { fmtCt } from '@/lib/prices';

/** Advice is cached for 15 minutes; it only changes with the forecast and the prices. */
export const ADVICE_STALE = 15 * 60_000;

/** Whether the server can give advice: a forecast needs coordinates, prices a zone. */
export const adviceEnabled = (sys?: System) => !!sys && ((sys.latitude != null && sys.longitude != null) || !!sys.price_zone);

export const adviceQuery = (systemId: string) => getAdviceOptions({ path: { systemId }, query: { hours: 3 } });

export const fmtWindow = (w: { start: string; end: string }, tz: string) => `${fmtTime(w.start, tz)}–${fmtTime(w.end, tz)}`;

const hasAdvice = (a?: Advice): a is Advice => !!a?.available && !!(a.solar_window || a.cheap_window);

/** One or two short lines for the dashboard card: the best solar and the cheapest window of today. */
export function AdviceHint({ a, tz }: { a?: Advice; tz: string }) {
  const { t } = useTranslation();
  if (!hasAdvice(a)) return null;
  return (
    <div className="grid gap-1 sm">
      {a.solar_window && (
        <span className="flex min-w-0 items-start gap-2">
          <Icon name="sun" size="sm" className="mt-0.5 flex-none" />
          <span className="min-w-0">
            {t('advice.solarShort', { window: fmtWindow(a.solar_window, tz) })}
            {a.solar_window.expected_kwh != null && <span className="muted"> · {fmtKWh(a.solar_window.expected_kwh)}</span>}
          </span>
        </span>
      )}
      {a.cheap_window && (
        <span className="flex min-w-0 items-start gap-2">
          <Icon name="coins" size="sm" className="mt-0.5 flex-none" />
          <span className="min-w-0">
            {t('advice.cheapShort', { window: fmtWindow(a.cheap_window, tz) })}
            {a.cheap_window.mean_eur_mwh != null && <span className="muted"> · {fmtCt(a.cheap_window.mean_eur_mwh)}</span>}
          </span>
        </span>
      )}
    </div>
  );
}

/** The status page's block: both windows with their details and the negative-price hours left today. */
export function AdviceSection({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const q = useQuery({ ...getAdviceOptions({ path: { systemId: sys.id }, query: { hours: 3 } }), enabled: adviceEnabled(sys), staleTime: ADVICE_STALE, refetchInterval: ADVICE_STALE });
  const a = q.data;
  if (!a?.available || !(a.solar_window || a.cheap_window || a.negative_prices.length)) return null;
  const tz = sys.timezone;
  return (
    <section className="section card" aria-labelledby="advice-h">
      <div className="grid gap-4">
        <div className="grid gap-1">
          <h2 id="advice-h">{t('advice.title')}</h2>
          <p className="sm muted">
            {t('advice.intro', { hours: a.hours })}
            {a.remaining_kwh != null && <> {t('advice.remaining', { energy: fmtKWh(a.remaining_kwh) })}</>}
          </p>
        </div>
        <div className="grid gap-4" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }}>
          {a.solar_window && <WindowStat icon="sun" label={t('advice.solar')} w={a.solar_window} tz={tz} kind="solar" />}
          {a.cheap_window && <WindowStat icon="coins" label={t('advice.cheap')} w={a.cheap_window} tz={tz} kind="cheap" />}
        </div>
        {a.negative_prices.length > 0 && (
          <div className="grid gap-1">
            <span className="sm font-medium">{t('advice.negative')}</span>
            <ul className="flex flex-wrap gap-2" style={{ listStyle: 'none', margin: 0, padding: 0 }}>
              {a.negative_prices.map((p) => (
                <li key={p.start}><Badge tone="danger">{fmtWindow(p, tz)} · {fmtCt(p.eur_mwh)}</Badge></li>
              ))}
            </ul>
            <span className="xs muted">{t('advice.negativeHelp')}</span>
          </div>
        )}
        <p className="xs muted">{t('advice.note')}</p>
      </div>
    </section>
  );
}

function WindowStat({ icon, label, w, tz, kind }: { icon: 'sun' | 'coins'; label: string; w: AdviceWindow; tz: string; kind: 'solar' | 'cheap' }) {
  const { t } = useTranslation();
  const details = [
    w.expected_kwh != null && t('advice.expected', { energy: fmtKWh(w.expected_kwh) }),
    w.mean_w != null && kind === 'solar' && t('advice.meanPower', { power: fmtW(w.mean_w) }),
    w.mean_eur_mwh != null && t('advice.meanPrice', { price: fmtCt(w.mean_eur_mwh) }),
  ].filter(Boolean).join(' · ');
  return (
    <div className="flex items-start gap-3">
      <span className="mt-0.5 flex-none text-warn-text"><Icon name={icon} /></span>
      <div className="grid min-w-0 gap-0.5">
        <span className="sm muted">{label}</span>
        <span className="text-lg font-semibold tabular-nums">{fmtWindow(w, tz)}</span>
        {details && <span className="sm muted">{details}</span>}
      </div>
    </div>
  );
}
