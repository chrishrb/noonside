import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useWatch, type UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { geocodeOptions } from '@/api/gen/@tanstack/react-query.gen';
import { reverseGeocode } from '@/api/gen/sdk.gen';
import type { Place } from '@/api/types';
import { PinMap } from '@/components/data/LazyMap';
import { Button } from '@/components/ui/Button';
import { Field, Input } from '@/components/ui/form';
import { Icon } from '@/components/ui/Icon';
import { latLon, type LatLon } from '@/lib/map';
import type { SystemForm, SystemValues } from './SystemFields';

const DEBOUNCE_MS = 300;

/** Waits until `value` stopped changing for `ms`. */
function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return v;
}

/**
 * Address input with suggestions (Photon / OpenStreetMap). Picking one fills
 * in the coordinates; typing clears them, so the server looks the address up
 * when saving and rejects one it cannot find. The map below shows the
 * position; moving its pin sets the coordinates and fills in the nearest
 * address (the pin's coordinates are kept, so they win over the address).
 */
export function AddressField({ form }: { form: UseFormReturn<SystemForm, unknown, SystemValues> }) {
  const { t, i18n } = useTranslation();
  const listId = useId();
  const [location, lat, lon] = useWatch({ control: form.control, name: ['location', 'latitude', 'longitude'] });
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [mapOpen, setMapOpen] = useState(false);
  // Coordinates from the map: the address lookup for the pin's position.
  const [pin, setPin] = useState<'looking' | 'found' | 'none' | null>(null);
  const lookup = useRef(0);
  const q = useDebounced(location.trim(), DEBOUNCE_MS);
  const search = useQuery({ ...geocodeOptions({ query: { q, lang: i18n.language } }), enabled: open && q.length >= 3, staleTime: 5 * 60_000, placeholderData: keepPreviousData });
  const places = open && q.length >= 3 ? search.data?.places ?? [] : [];
  const reg = form.register('location');
  const coords = latLon(lat, lon);
  const hasCoords = coords != null;

  const pick = (p: Place) => {
    const opts = { shouldDirty: true, shouldValidate: true };
    form.setValue('location', p.label, opts);
    form.setValue('latitude', p.latitude, opts);
    form.setValue('longitude', p.longitude, opts);
    form.clearErrors('location');
    lookup.current++;
    setPin(null);
    setOpen(false);
    setActive(-1);
  };
  const place = (v: LatLon) => {
    const opts = { shouldDirty: true, shouldValidate: true };
    form.setValue('latitude', v.lat, opts);
    form.setValue('longitude', v.lon, opts);
    form.clearErrors('location');
    setOpen(false);
    setPin('looking');
    const n = ++lookup.current;
    void reverseGeocode({ query: { lat: v.lat, lon: v.lon, lang: i18n.language } })
      .then(({ data }) => {
        if (n !== lookup.current) return; // moved again, or the address was edited
        const found = data.places[0];
        if (found) form.setValue('location', found.label, opts);
        setPin(found ? 'found' : 'none');
      })
      .catch(() => { if (n === lookup.current) setPin('none'); });
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' && places.length) { setOpen(true); setActive((a) => (a + 1) % places.length); }
    else if (e.key === 'ArrowUp' && places.length) setActive((a) => (a <= 0 ? places.length - 1 : a - 1));
    else if (e.key === 'Enter' && open && active >= 0 && places[active]) pick(places[active]);
    else if (e.key === 'Escape' && open) setOpen(false);
    else return;
    e.preventDefault();
  };

  const noMatch = open && q.length >= 3 && search.isSuccess && !search.isPlaceholderData && search.data.available && !places.length;
  const showMap = hasCoords || mapOpen;
  const help = hasCoords
    ? <span className="inline-flex flex-wrap items-center gap-1"><Icon name="check" size="sm" /><span>{t(pin === 'found' ? 'address.fromPin' : pin ? 'address.pinned' : 'address.found')}.</span><span>{t('address.dragPin')}</span></span>
    : noMatch ? t('address.noMatch')
      : mapOpen ? t('address.tapMap')
        : location.trim() ? t('address.lookupOnSave') : t('address.help');
  const expanded = open && places.length > 0;
  const err = form.formState.errors.location?.message as string | undefined;

  return (
    <Field label={t('system.location')} optional span2 error={err} help={help}>
      <div className="addr">
        <Input
          {...reg}
          placeholder={t('system.locationPlaceholder')}
          autoComplete="off"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={expanded}
          aria-controls={listId}
          aria-activedescendant={expanded && active >= 0 ? `${listId}-${active}` : undefined}
          onChange={(e) => {
            void reg.onChange(e);
            // A typed address has no coordinates yet; the server looks it up.
            form.setValue('latitude', '', { shouldDirty: true });
            form.setValue('longitude', '', { shouldDirty: true });
            lookup.current++;
            setPin(null);
            setOpen(true);
            setActive(-1);
          }}
          onFocus={() => setOpen(true)}
          onBlur={(e) => { void reg.onBlur(e); setOpen(false); }}
          onKeyDown={onKey}
        />
        {expanded && (
          <ul className="addr-list" id={listId} role="listbox" aria-label={t('address.suggestions')}>
            {places.map((p, i) => (
              <li key={`${p.label}-${i}`} id={`${listId}-${i}`} role="option" aria-selected={i === active}
                onMouseDown={(e) => e.preventDefault()} onClick={() => pick(p)} onMouseEnter={() => setActive(i)}>
                <Icon name={p.kind === 'house' ? 'home' : 'search'} size="sm" />
                <span className="truncate">{p.label}</span>
              </li>
            ))}
            <li className="addr-foot" role="presentation">© OpenStreetMap contributors · Photon</li>
          </ul>
        )}
      </div>
      {showMap
        ? <PinMap value={coords} onChange={place} label={t('address.mapLabel')} pinLabel={t('address.pin')} />
        : <Button variant="secondary" size="sm" icon="pin" className="justify-self-start" onClick={() => setMapOpen(true)}>{t('address.setOnMap')}</Button>}
    </Field>
  );
}
