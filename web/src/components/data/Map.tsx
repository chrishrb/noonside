import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import { DEFAULT_VIEW, PIN_ZOOM, TILE_ATTRIBUTION, TILE_MAX_ZOOM, TILE_URL, roundCoord, type LatLon } from '@/lib/map';

// Leaflet maps with OpenStreetMap tiles. Loaded lazily (see LazyMap.tsx), so
// Leaflet stays out of the main bundle.

export type PinTone = 'accent' | 'ok' | 'err' | 'idle';

const PIN_SVG = '<svg viewBox="0 0 28 36" aria-hidden="true"><path d="M14 1.5C7.1 1.5 2 6.6 2 13c0 8.3 12 21.5 12 21.5S26 21.3 26 13c0-6.4-5.1-11.5-12-11.5z"/><circle cx="14" cy="13" r="4.5"/></svg>';
const pinIcon = (tone: PinTone) => L.divIcon({ className: `map-pin tone-${tone}`, html: PIN_SVG, iconSize: [28, 36], iconAnchor: [14, 34], popupAnchor: [0, -30] });

function createMap(el: HTMLElement, center: LatLon, zoom: number): L.Map {
  // No wheel zoom: the maps sit in scrolling pages (buttons, double-click and pinch still zoom).
  const map = L.map(el, { center: [center.lat, center.lon], zoom, scrollWheelZoom: false, maxZoom: TILE_MAX_ZOOM });
  L.tileLayer(TILE_URL, { maxZoom: TILE_MAX_ZOOM, attribution: TILE_ATTRIBUTION }).addTo(map);
  return map;
}

/** Keeps Leaflet's size in sync with its box (dialogs, sidebar toggles, rotation). */
function useInvalidateOnResize(el: React.RefObject<HTMLDivElement | null>, map: React.RefObject<L.Map | null>) {
  useEffect(() => {
    if (!el.current || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => map.current?.invalidateSize());
    ro.observe(el.current);
    return () => ro.disconnect();
  }, [el, map]);
}

/**
 * A map with one draggable pin. Tapping the map moves the pin there. When the
 * value changes from outside (a picked address), the map centers on it.
 */
export function PinMap({ value, onChange, label, pinLabel }: { value: LatLon | null; onChange: (v: LatLon) => void; label: string; pinLabel: string }) {
  const el = useRef<HTMLDivElement>(null);
  const map = useRef<L.Map | null>(null);
  const marker = useRef<L.Marker | null>(null);
  const emitted = useRef<LatLon | null>(null);
  const change = useRef(onChange);
  useLayoutEffect(() => { change.current = onChange; });
  useInvalidateOnResize(el, map);

  const emit = (ll: L.LatLng) => {
    const v = { lat: roundCoord(ll.lat), lon: roundCoord(ll.lng) };
    emitted.current = v;
    change.current(v);
  };

  useEffect(() => {
    const start = value ?? DEFAULT_VIEW;
    const m = createMap(el.current!, start, value ? PIN_ZOOM : DEFAULT_VIEW.zoom);
    m.on('click', (e: L.LeafletMouseEvent) => {
      emit(e.latlng);
      // Placed from far out: zoom in so the pin can be refined.
      if (m.getZoom() < PIN_ZOOM - 4) m.setView(e.latlng, m.getZoom() + 4);
    });
    map.current = m;
    return () => { m.remove(); map.current = null; marker.current = null; };
    // Created once; the effect below follows the value.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const lat = value?.lat, lon = value?.lon;
  useEffect(() => {
    const m = map.current;
    if (!m) return;
    if (lat == null || lon == null) {
      marker.current?.remove();
      marker.current = null;
      return;
    }
    if (marker.current) marker.current.setLatLng([lat, lon]);
    else {
      marker.current = L.marker([lat, lon], { icon: pinIcon('accent'), draggable: true, autoPan: true, keyboard: false, title: pinLabel })
        .on('dragend', (e) => emit((e.target as L.Marker).getLatLng()))
        .addTo(m);
    }
    const own = emitted.current && emitted.current.lat === lat && emitted.current.lon === lon;
    if (!own) m.setView([lat, lon], Math.max(m.getZoom(), PIN_ZOOM - 2));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lat, lon]);

  return <div ref={el} className="map map-pin-picker" role="application" aria-label={label} />;
}

export type MapPoint = LatLon & { id: string; name: string; tone: PinTone; popup: ReactNode };

/** Systems as pins; tapping one opens its popup (React content, rendered through a portal). */
export function FleetMap({ points, label }: { points: MapPoint[]; label: string }) {
  const el = useRef<HTMLDivElement>(null);
  const map = useRef<L.Map | null>(null);
  const pins = useRef<L.LayerGroup | null>(null);
  const popup = useRef<L.Popup | null>(null);
  const content = useMemo(() => document.createElement('div'), []);
  const [open, setOpen] = useState<string | null>(null);
  useInvalidateOnResize(el, map);

  useEffect(() => {
    const m = createMap(el.current!, DEFAULT_VIEW, DEFAULT_VIEW.zoom);
    pins.current = L.layerGroup().addTo(m);
    map.current = m;
    return () => { m.remove(); map.current = null; pins.current = null; popup.current = null; };
  }, []);

  // Rebuild the pins when positions, names or tones change (not on every refresh).
  const pinKey = points.map((p) => `${p.id}:${p.lat},${p.lon}:${p.tone}:${p.name}`).join('|');
  useEffect(() => {
    const layer = pins.current;
    if (!layer) return;
    layer.clearLayers();
    for (const p of points) {
      // No autoPanOnFocus: a tap focuses first, and panning then moved the pin away from the finger (no click).
      const mk = L.marker([p.lat, p.lon], { icon: pinIcon(p.tone), title: p.name, riseOnHover: true, autoPanOnFocus: false })
        .on('click', () => setOpen(p.id))
        // Markers are focusable buttons, but Leaflet 1.9 doesn't click them on Enter.
        .on('keypress', (e) => {
          const ev = (e as L.LeafletKeyboardEvent).originalEvent;
          if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); setOpen(p.id); }
        })
        .addTo(layer);
      mk.getElement()?.setAttribute('aria-label', p.name);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pinKey]);

  // Fit the view when the set of positions changes.
  const posKey = points.map((p) => `${p.lat},${p.lon}`).sort().join('|');
  useEffect(() => {
    const m = map.current;
    if (!m || !points.length) return;
    if (points.length === 1) m.setView([points[0].lat, points[0].lon], 13);
    else m.fitBounds(L.latLngBounds(points.map((p) => [p.lat, p.lon])), { paddingTopLeft: [40, 56], paddingBottomRight: [40, 32], maxZoom: 14 });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [posKey]);

  const openPoint = points.find((p) => p.id === open);
  const olat = openPoint?.lat, olon = openPoint?.lon;
  useEffect(() => {
    const m = map.current;
    if (!m || !open || olat == null || olon == null) return;
    // A fresh popup per pin: opening another one closes this one without clearing `open`.
    const p = L.popup({ minWidth: 200, maxWidth: 280, autoPanPadding: [16, 16] }).setLatLng([olat, olon]).setContent(content);
    p.on('remove', () => setOpen((cur) => (cur === open ? null : cur)));
    popup.current = p;
    p.openOn(m);
    return () => { if (popup.current === p) popup.current = null; };
  }, [open, olat, olon, content]);

  // Content changes size with live data; let Leaflet re-measure.
  useLayoutEffect(() => { popup.current?.update(); });

  return (
    <>
      <div ref={el} className="map map-fleet" role="region" aria-label={label} />
      {openPoint && createPortal(openPoint.popup, content)}
    </>
  );
}
