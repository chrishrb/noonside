import { act, fireEvent, render, screen } from '@testing-library/react';
import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { systemDefaults, type SystemForm, type SystemValues } from './SystemFields';
import { AddressField } from './AddressField';

const places = [
  { label: 'Hauptstraße 1, 86199 Augsburg, Deutschland', latitude: 48.3194, longitude: 10.8223, kind: 'house' },
  { label: 'Hauptstraße, 86830 Schwabmünchen, Deutschland', latitude: 48.18, longitude: 10.69, kind: 'street' },
];
const useQuery = vi.fn();
const reverseGeocode = vi.fn();
vi.mock('@tanstack/react-query', async (orig) => ({ ...(await orig<object>()), useQuery: (o: unknown) => useQuery(o) }));
vi.mock('@/api/gen/sdk.gen', () => ({ reverseGeocode: (o: unknown) => reverseGeocode(o) }));
// Leaflet doesn't run in jsdom; the stub "drops the pin" on click.
vi.mock('@/components/data/LazyMap', () => ({
  PinMap: ({ onChange }: { onChange: (v: { lat: number; lon: number }) => void }) => <button type="button" onClick={() => onChange({ lat: 48.1, lon: 11.5 })}>drop pin</button>,
}));

const ref: { values?: () => SystemForm } = {};
const values = () => ref.values!();
function Harness() {
  const form = useForm<SystemForm, unknown, SystemValues>({ defaultValues: { ...systemDefaults(), latitude: 50, longitude: 8 } });
  useEffect(() => { ref.values = form.getValues; }, [form]);
  return <AddressField form={form} />;
}

describe('AddressField', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useQuery.mockImplementation((opts: { queryKey: [{ query: { q: string } }]; enabled: boolean }) => ({
      data: opts.enabled && opts.queryKey[0].query.q.length >= 3 ? { available: true, places } : undefined, isSuccess: opts.enabled, isPlaceholderData: false,
    }));
  });
  afterEach(() => vi.useRealTimers());

  it('suggests addresses and fills in the coordinates of the picked one', () => {
    render(<Harness />);
    const input = screen.getByRole('combobox');
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'Hauptstr' } });
    // Typing clears the old coordinates so the server looks the address up.
    expect(values().latitude).toBe('');
    act(() => { vi.advanceTimersByTime(300); });
    expect(screen.getAllByRole('option')).toHaveLength(2);
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(values()).toMatchObject({ location: places[0].label, latitude: 48.3194, longitude: 10.8223 });
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    expect(screen.getByText('Address found.')).toBeInTheDocument();
  });

  it('does not search for fewer than 3 characters', () => {
    render(<Harness />);
    const input = screen.getByRole('combobox');
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: 'Ha' } });
    act(() => { vi.advanceTimersByTime(300); });
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('fills in the address nearest to a dropped pin', async () => {
    reverseGeocode.mockResolvedValue({ data: { available: true, places: [{ label: 'Marienplatz 8, 80331 München, Deutschland', latitude: 48.13, longitude: 11.57, kind: 'house' }] } });
    render(<Harness />);
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'drop pin' })); });
    expect(reverseGeocode).toHaveBeenCalledWith({ query: { lat: 48.1, lon: 11.5, lang: 'en' } });
    // The pin's position is kept, not the address's.
    expect(values()).toMatchObject({ location: 'Marienplatz 8, 80331 München, Deutschland', latitude: 48.1, longitude: 11.5 });
    expect(screen.getByText('Address taken from the pin.')).toBeInTheDocument();
  });

  it('ignores a late address lookup after the address was edited', async () => {
    let resolve!: (v: unknown) => void;
    reverseGeocode.mockReturnValue(new Promise((r) => { resolve = r; }));
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: 'drop pin' }));
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'Typed street 1' } });
    await act(async () => { resolve({ data: { available: true, places: [{ label: 'Somewhere else', latitude: 1, longitude: 1, kind: 'house' }] } }); });
    expect(values().location).toBe('Typed street 1');
  });
});
