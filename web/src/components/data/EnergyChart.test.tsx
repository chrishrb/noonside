import { act, fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { EnergyChart } from './EnergyChart';
import { TOUCH_TIP_MS } from './useTouchTip';
import { fmtKWh } from '@/lib/format';

globalThis.ResizeObserver ??= class { observe() {} disconnect() {} unobserve() {} } as unknown as typeof ResizeObserver;

const base = {
  kind: 'bar' as const, unit: 'kWh' as const, labels: ['Jan', 'Feb', 'Mar'], tick: (i: number) => ['J', 'F', 'M'][i], fmt: fmtKWh, ariaLabel: 'Energy chart',
};

describe('EnergyChart', () => {
  it('draws stacked bars and target ticks', () => {
    const { container } = render(<EnergyChart {...base}
      series={[{ key: 'a', name: 'WR 1', color: '#f00', values: [10, 20, 30] }, { key: 'b', name: 'WR 2', color: '#0f0', values: [5, 5, 5] }]}
      total={[15, 25, 35]} target={[20, 20, 20]} />);
    expect(container.querySelectorAll('rect')).toHaveLength(6);
    expect(container.querySelectorAll('line.target')).toHaveLength(3);
  });
  it('draws the expected curve beyond the data on day charts', () => {
    const { container } = render(<EnergyChart {...base} kind="area" unit="W" fmt={(v) => `${v ?? '—'} W`}
      series={[{ key: 'total', name: 'Total', color: '#00f', values: [100, 200, null] }]} total={[100, 200, null]} expected={[null, 400, 800]} />);
    const path = container.querySelector('path.target');
    expect(path?.getAttribute('d')?.match(/[ML]/g)).toEqual(['M', 'L']);
    const svg = screen.getByRole('img', { name: 'Energy chart' });
    fireEvent.focus(svg); // the last bucket with data
    expect(screen.getByText('Expected')).toBeInTheDocument();
    expect(screen.getByText('400 W · 50 %')).toBeInTheDocument();
  });
  it('draws one area per run of buckets with data', () => {
    const { container } = render(<EnergyChart {...base} kind="area" unit="W" labels={['a', 'b', 'c', 'd', 'e']} tick={() => null}
      series={[{ key: 'total', name: 'Total', color: '#00f', values: [null, 100, null, 200, 300] }]} total={[null, 100, null, 200, 300]} />);
    const line = container.querySelector('path[stroke="#00f"]');
    expect(line?.getAttribute('d')?.match(/[ML]/g)).toEqual(['M', 'M', 'L']);
  });
  it('hides the tooltip shortly after a touch ends', () => {
    vi.useFakeTimers();
    try {
      render(<EnergyChart {...base} series={[{ key: 'a', name: 'WR 1', color: '#f00', values: [10, 20, 30] }]} total={[10, 20, 30]} />);
      const svg = screen.getByRole('img', { name: 'Energy chart' });
      svg.getBoundingClientRect = () => ({ left: 0, top: 0, width: 600, height: 240 }) as DOMRect; // jsdom has no layout
      const touch = (type: string) => svg.dispatchEvent(Object.assign(new MouseEvent(type, { bubbles: true, clientX: 100 }), { pointerType: 'touch' }));
      act(() => { touch('pointerdown'); });
      expect(screen.getByText('Jan')).toBeInTheDocument();
      act(() => { touch('pointerup'); });
      expect(screen.getByText('Jan')).toBeInTheDocument();
      act(() => { vi.advanceTimersByTime(TOUCH_TIP_MS); });
      expect(screen.queryByText('Jan')).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
  it('is interactive with only an expected curve', () => {
    render(<EnergyChart {...base} kind="area" unit="W" series={[{ key: 'total', name: 'Total', color: '#00f', values: [] }]}
      total={[null, null, null]} expected={[0, 500, 0]} emptyText="No data yet" />);
    expect(screen.queryByText('No data yet')).not.toBeInTheDocument();
  });
  it('shows a tooltip from the keyboard', () => {
    render(<EnergyChart {...base} series={[{ key: 'total', name: 'Total', color: '#00f', values: [1, 2, 3] }]} total={[1, 2, 3]} />);
    const svg = screen.getByRole('img', { name: 'Energy chart' });
    fireEvent.focus(svg);
    expect(screen.getByText('Mar')).toBeInTheDocument();
    fireEvent.keyDown(svg, { key: 'Home' });
    expect(screen.getByText('Jan')).toBeInTheDocument();
    expect(screen.getByText('1.00 kWh')).toBeInTheDocument();
  });
  it('opens a bucket with Enter and a click', () => {
    const onSelect = vi.fn();
    render(<EnergyChart {...base} series={[{ key: 'total', name: 'Total', color: '#00f', values: [1, null, 3] }]} total={[1, null, 3]}
      onSelect={onSelect} selectHint="Click for the month view" />);
    const svg = screen.getByRole('img', { name: 'Energy chart' });
    fireEvent.focus(svg);
    expect(screen.getByText('Click for the month view')).toBeInTheDocument();
    fireEvent.keyDown(svg, { key: 'Enter' });
    expect(onSelect).toHaveBeenLastCalledWith(2);
    fireEvent.keyDown(svg, { key: 'ArrowLeft' });
    fireEvent.keyDown(svg, { key: 'Enter' }); // no data in this bucket
    expect(onSelect).toHaveBeenCalledTimes(1);
    // jsdom has no layout: give the chart its 600px viewBox width, then click left of the plot (first bucket).
    svg.getBoundingClientRect = () => ({ left: 0, top: 0, width: 600, height: 300 }) as DOMRect;
    fireEvent.pointerDown(svg, { clientX: 10 });
    fireEvent.click(svg, { clientX: 10 });
    expect(onSelect).toHaveBeenLastCalledWith(0);
  });
  it('says when there is no data', () => {
    render(<EnergyChart {...base} series={[]} total={[0, 0, 0]} emptyText="Nothing yet" />);
    expect(screen.getByText('Nothing yet')).toBeInTheDocument();
  });
});
