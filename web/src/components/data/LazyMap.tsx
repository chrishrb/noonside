import { lazy, Suspense, type ComponentProps } from 'react';

// Leaflet (~150 kB) loads only when a map is shown.
const Pin = lazy(() => import('./Map').then((m) => ({ default: m.PinMap })));
const Fleet = lazy(() => import('./Map').then((m) => ({ default: m.FleetMap })));

export type { MapPoint, PinTone } from './Map';

export const PinMap = (props: ComponentProps<typeof Pin>) => (
  <Suspense fallback={<div className="map map-pin-picker map-loading" />}><Pin {...props} /></Suspense>
);

export const FleetMap = (props: ComponentProps<typeof Fleet>) => (
  <Suspense fallback={<div className="map map-fleet map-loading" />}><Fleet {...props} /></Suspense>
);
