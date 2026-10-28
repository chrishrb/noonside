import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactElement } from 'react';
import type { ChartSeries } from './EnergyChart';
import { useTouchTip } from './useTouchTip';

/**
 * Plain line chart (one line per series, not stacked) for readings like
 * temperature or DC voltage on the day axis. Hover, touch or ←/→ shows the
 * values; on touch they hide shortly after the finger lifts.
 */
export function LineChart({ series, labels, tick, fmt, axis, ariaLabel }: {
  series: ChartSeries[];
  /** Tooltip heading per bucket. */
  labels: string[];
  tick: (i: number) => string | null;
  fmt: (v: number | null | undefined) => string;
  /** Y-axis label; `digits` is how many decimals the grid step needs. */
  axis: (v: number, digits: number) => string;
  ariaLabel: string;
}) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const tipRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [idx, setIdx] = useState(-1);
  const [tipLeft, setTipLeft] = useState(0);
  const touch = useTouchTip(() => setIdx(-1));
  const n = labels.length;

  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setWidth(Math.round(el.clientWidth)));
    ro.observe(el);
    setWidth(Math.round(el.clientWidth));
    return () => ro.disconnect();
  }, []);

  const g = useMemo(() => {
    const W = Math.max(280, width || 600), H = W < 560 ? 160 : 200;
    const m = { l: 48, r: 8, t: 10, b: 26 };
    const iw = W - m.l - m.r, ih = H - m.t - m.b;
    let lo = Infinity, hi = -Infinity;
    series.forEach((s) => s.values.forEach((v) => { if (v != null) { lo = Math.min(lo, v); hi = Math.max(hi, v); } }));
    if (!Number.isFinite(lo)) { lo = 0; hi = 1; }
    // Round the range to a step of 1, 2, 5 × 10^k with some headroom.
    const span = Math.max(hi - lo, 1);
    const mag = Math.pow(10, Math.floor(Math.log10(span / 4)));
    const step = [1, 2, 5, 10].map((f) => f * mag).find((s) => span / s <= 5) ?? 10 * mag;
    const min = Math.floor(lo / step) * step, max = Math.ceil(hi / step) * step || min + step;
    const y = (v: number) => m.t + ih - ((v - min) / (max - min)) * ih;
    const cx = (i: number) => m.l + (n <= 1 ? iw / 2 : (iw * i) / (n - 1));
    return { W, H, m, iw, ih, min, max, step, y, cx };
  }, [width, series, n]);

  const body = useMemo(() => {
    const { W, H, m, min, max, step, y, cx } = g;
    const grid: ReactElement[] = [];
    for (let v = min; v <= max + 1e-9; v += step) {
      grid.push(<line key={`g${v}`} x1={m.l} x2={W - m.r} y1={y(v).toFixed(1)} y2={y(v).toFixed(1)} />);
    }
    const ylab = [];
    const digits = step < 1 ? Math.ceil(-Math.log10(step)) : 0;
    for (let v = min; v <= max + 1e-9; v += step) ylab.push(<text key={v} x={m.l - 8} y={(y(v) + 4).toFixed(1)} textAnchor="end">{axis(v, digits)}</text>);
    const xlab: ReactElement[] = [];
    let lastX = -1e9;
    for (let i = 0; i < n; i++) {
      const label = tick(i);
      if (label == null || cx(i) - lastX < 44) continue;
      lastX = cx(i);
      xlab.push(<text key={i} x={cx(i).toFixed(1)} y={H - 8} textAnchor="middle">{label}</text>);
    }
    const lines = series.map((s) => {
      let d = '', open = false;
      s.values.forEach((v, i) => {
        if (v == null) { open = false; return; }
        d += `${open ? 'L' : 'M'}${cx(i).toFixed(1)},${y(v).toFixed(1)}`;
        open = true;
      });
      return <path key={s.key} d={d} fill="none" stroke={s.color} strokeWidth={1.75} strokeLinejoin="round" />;
    });
    return <><g className="chart-grid">{grid}</g><g className="chart-axis">{ylab}{xlab}</g><g>{lines}</g></>;
  }, [g, series, n, tick, axis]);

  useLayoutEffect(() => {
    if (idx < 0 || !wrapRef.current || !tipRef.current) return;
    const scale = wrapRef.current.clientWidth / g.W;
    const x = g.cx(idx) * scale;
    let left = x + 14;
    if (left + tipRef.current.offsetWidth > wrapRef.current.clientWidth) left = x - tipRef.current.offsetWidth - 14;
    setTipLeft(Math.max(0, left));
  }, [idx, g]);

  const at = (clientX: number, el: SVGSVGElement) => {
    const r = el.getBoundingClientRect();
    const px = ((clientX - r.left) * g.W) / r.width;
    return Math.max(0, Math.min(n - 1, Math.round(((px - g.m.l) / g.iw) * (n - 1))));
  };
  const shown = idx >= 0 && series.some((s) => s.values[idx] != null);

  return (
    <div className="chart" ref={wrapRef}>
      <svg tabIndex={0} role="img" aria-label={ariaLabel} viewBox={`0 0 ${g.W} ${g.H}`} width={g.W} height={g.H} style={{ touchAction: 'pan-y' }}
        onPointerMove={(e) => setIdx(at(e.clientX, e.currentTarget))} onPointerDown={(e) => { touch.hold(); setIdx(at(e.clientX, e.currentTarget)); }}
        onPointerUp={touch.release} onPointerCancel={touch.release}
        onPointerLeave={(e) => e.pointerType === 'mouse' && setIdx(-1)} onBlur={() => setIdx(-1)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowRight') setIdx((i) => Math.min(n - 1, i + (e.shiftKey ? 12 : 1)));
          else if (e.key === 'ArrowLeft') setIdx((i) => Math.max(0, (i < 0 ? n : i) - (e.shiftKey ? 12 : 1)));
          else if (e.key === 'Escape') setIdx(-1);
          else return;
          e.preventDefault();
        }}>
        {body}
        {shown && (
          <g>
            <line className="cross" x1={g.cx(idx)} x2={g.cx(idx)} y1={g.m.t} y2={g.m.t + g.ih} />
            {series.map((s) => s.values[idx] != null && <circle key={s.key} cx={g.cx(idx)} cy={g.y(s.values[idx]!)} r={3.5} fill={s.color} stroke="var(--surface)" strokeWidth={1.5} />)}
          </g>
        )}
      </svg>
      {shown && (
        <div className="chart-tip" ref={tipRef} aria-live="polite" style={{ transform: `translate(${tipLeft}px, ${g.m.t}px)` }}>
          <div className="t">{labels[idx]}</div>
          <div className="rows">
            {series.map((s) => (
              <div className="row" key={s.key}>
                <span><span className="swatch" style={{ background: s.color }} /><span className="truncate">{s.name}</span></span>
                <span>{fmt(s.values[idx])}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
