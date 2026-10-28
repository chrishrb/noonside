import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Freshness } from './Freshness';

globalThis.ResizeObserver ??= class { observe() {} disconnect() {} unobserve() {} } as unknown as typeof ResizeObserver;

const now = new Date('2026-06-15T12:00:00Z');
const ago = (min: number) => new Date(now.getTime() - min * 60_000).toISOString();

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
  vi.setSystemTime(now);
});
afterEach(() => vi.useRealTimers());

describe('Freshness', () => {
  it('shows the relative time and refreshes it every 30 s', () => {
    render(<Freshness time={ago(3)} />);
    expect(screen.getByText('Updated 3 min ago')).toBeInTheDocument();
    expect(screen.queryByRole('button')).toBeNull();
    act(() => { vi.advanceTimersByTime(2 * 60_000); });
    expect(screen.getByText('Updated 5 min ago')).toBeInTheDocument();
  });

  it('uses a custom format', () => {
    render(<Freshness time={ago(10)} format={(when) => `seen ${when}`} />);
    expect(screen.getByText('seen 10 min ago')).toBeInTheDocument();
  });

  it('turns into a warning with an explanation when stale', () => {
    render(<Freshness time={ago(45)} stale tz="UTC" />);
    const btn = screen.getByRole('button', { name: /Data delayed:\s*Updated 45 min ago/ });
    expect(btn).toHaveClass('stale');
    fireEvent.click(btn);
    expect(screen.getByText(/The last reading is from .*11:15/)).toBeInTheDocument();
  });
});
