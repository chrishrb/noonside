import { useTranslation } from 'react-i18next';
import type { Weather } from '@/api/types';
import { Icon } from '@/components/ui/Icon';
import { num } from '@/lib/format';
import { weatherIcon, weatherKind } from '@/lib/weather';

export const fmtTemp = (c: number) => `${num(c, 0)} °C`;

/** Compact current conditions: icon + temperature, condition in the tooltip. */
export function WeatherBadge({ w }: { w: Weather }) {
  const { t } = useTranslation();
  const c = w.current;
  if (!c) return null;
  const label = `${t(`weather.kind.${weatherKind(c.weather_code)}`)}, ${fmtTemp(c.temperature_c)}`;
  return (
    <span className="badge wx-badge" title={`${label} · ${t('weather.cloudCover', { pct: num(c.cloud_cover_pct, 0) })}`} aria-label={t('weather.nowAria', { label })}>
      <Icon name={weatherIcon(c.weather_code, c.is_day)} size="sm" />{fmtTemp(c.temperature_c)}
    </span>
  );
}
