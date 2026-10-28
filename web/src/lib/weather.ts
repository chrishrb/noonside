import type { IconName } from '@/components/ui/Icon';
import type { ExpectedPoint } from '@/api/types';

export type WeatherKind = 'clear' | 'mainlyClear' | 'partlyCloudy' | 'overcast' | 'fog' | 'drizzle' | 'rain' | 'showers' | 'snow' | 'thunder';

/** WMO weather interpretation code (Open-Meteo) → condition. */
export function weatherKind(code: number): WeatherKind {
  if (code === 0) return 'clear';
  if (code === 1) return 'mainlyClear';
  if (code === 2) return 'partlyCloudy';
  if (code === 3) return 'overcast';
  if (code === 45 || code === 48) return 'fog';
  if (code >= 51 && code <= 57) return 'drizzle';
  if (code >= 61 && code <= 67) return 'rain';
  if (code >= 80 && code <= 82) return 'showers';
  if ((code >= 71 && code <= 77) || code === 85 || code === 86) return 'snow';
  if (code >= 95) return 'thunder';
  return 'overcast';
}

const ICON: Record<WeatherKind, [day: IconName, night: IconName]> = {
  clear: ['sun', 'moon'], mainlyClear: ['cloudSun', 'cloudMoon'], partlyCloudy: ['cloudSun', 'cloudMoon'], overcast: ['cloud', 'cloud'],
  fog: ['fog', 'fog'], drizzle: ['rain', 'rain'], rain: ['rain', 'rain'], showers: ['rain', 'rain'], snow: ['snow', 'snow'], thunder: ['storm', 'storm'],
};

export const weatherIcon = (code: number, isDay = true): IconName => ICON[weatherKind(code)][isDay ? 0 : 1];

const BUCKET_MID_MS = 150_000; // 5-minute buckets are compared at their middle

/**
 * Expected power at each 5-minute bucket start, interpolated linearly between
 * the 15-minute points and scaled (e.g. to the selected inverters' kWp);
 * null outside the forecast.
 */
export function expectedAt(points: ExpectedPoint[], timestamps: string[], scale = 1): (number | null)[] {
  const xs = points.map((p) => Date.parse(p.time));
  let j = 0;
  return timestamps.map((ts) => {
    const t = Date.parse(ts) + BUCKET_MID_MS;
    if (!xs.length || t < xs[0] - 450_000 || t > xs[xs.length - 1] + 450_000) return null;
    while (j + 1 < xs.length && xs[j + 1] <= t) j++;
    if (t <= xs[0]) return points[0].w * scale;
    if (j + 1 >= xs.length) return points[j].w * scale;
    const f = (t - xs[j]) / (xs[j + 1] - xs[j]);
    return Math.round((points[j].w + (points[j + 1].w - points[j].w) * f) * scale);
  });
}
