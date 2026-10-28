// OpenStreetMap tiles for the maps (Leaflet). The tile host must be allowed
// in img-src: the CSP in web/nginx/headers.inc.template. The public tile server is fair-use (no heavy or
// commercial use) and requires the attribution.
export const TILE_URL = 'https://tile.openstreetmap.org/{z}/{x}/{y}.png';
export const TILE_ATTRIBUTION = '© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer">OpenStreetMap</a>';
export const TILE_MAX_ZOOM = 19;

/** Shown before a position is known (central Europe). */
export const DEFAULT_VIEW = { lat: 50.5, lon: 10, zoom: 5 };
/** Zoom for a single building. */
export const PIN_ZOOM = 17;

export type LatLon = { lat: number; lon: number };

/** Coordinates as numbers, or null when either is missing (form fields hold '' when empty). */
export function latLon(lat: unknown, lon: unknown): LatLon | null {
  if (lat === '' || lat == null || lon === '' || lon == null) return null;
  const a = Number(lat), o = Number(lon);
  return Number.isFinite(a) && Number.isFinite(o) ? { lat: a, lon: o } : null;
}

/** Rounded to about 1 m, so a dragged pin doesn't store 15 decimals. */
export const roundCoord = (v: number) => Math.round(v * 1e5) / 1e5;
