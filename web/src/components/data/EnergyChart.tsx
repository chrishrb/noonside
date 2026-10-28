import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactElement } from "react";
import { useTranslation } from 'react-i18next';
import { num } from '@/lib/format';
import { useTouchTip } from './useTouchTip';

/** `key` identifies the series (names may repeat, e.g. "WR 1" in two groups). */
export interface ChartSeries { key: string; name: string; color: string; values: (number | null | undefined)[] }

/** An earlier period drawn over bar charts (year-over-year), newest first. */
export interface CompareSeries { key: string; name: string; values: (number | null | undefined)[] }

/** Stroke opacity of the i-th of n comparison lines: the newest is the strongest. */
export const compareOpacity = (i: number, n: number) => (n <= 1 ? 0.7 : 0.7 - (0.5 * i) / (n - 1));

export interface EnergyChartProps {
  kind: 'area' | 'bar';
  unit: 'W' | 'kWh';
  series: ChartSeries[];
  total: (number | null | undefined)[];
  target?: (number | null | undefined)[];
  /** Tooltip label of `target` (default "Target"). */
  targetLabel?: string;
  /** Expected power per bucket (area charts), drawn as a dashed line; may extend past the data. */
  expected?: (number | null | undefined)[];
  /** Area charts: day-ahead price per bucket (ct/kWh), drawn as a step line on a right-hand axis; negative intervals are shaded. */
  price?: (number | null | undefined)[];
  /** Tooltip value of `price`. */
  priceFmt?: (v: number) => string;
  /** Bar charts: the same buckets in earlier periods, drawn as lines with dots. */
  compare?: CompareSeries[];
  /** Tooltip heading per bucket. */
  labels: string[];
  /** X-axis label for bucket i (null skips it); `band` is the bucket width in px. */
  tick: (i: number, band: number) => string | null;
  fmt: (v: number | null | undefined) => string;
  ariaLabel: string;
  emptyText?: string;
  /** Called when a bucket with data is clicked or activated with Enter/Space (drill-down). */
  onSelect?: (i: number) => void;
  /** Tooltip hint for `onSelect`, e.g. "Click for the month view". */
  selectHint?: string;
}

const TIP_ONE_COLUMN_MAX = 8;

function niceScale(max: number) {
  if (!(max > 0)) return { max: 1, step: 0.25 };
  const raw = max / 4, mag = Math.pow(10, Math.floor(Math.log10(raw))), n = raw / mag;
  const step = (n <= 1 ? 1 : n <= 2 ? 2 : n <= 2.5 ? 2.5 : n <= 5 ? 5 : 10) * mag;
  return { max: Math.ceil((max * 1.04) / step) * step, step };
}

export const axisFmt = (unit: 'W' | 'kWh') => (v: number) => {
  if (unit === 'W') return v >= 1000 ? `${num(v / 1000, v % 1000 ? 1 : 0)} kW` : `${num(v, 0)} W`;
  return v >= 1000 ? `${num(v / 1000, v % 1000 ? 1 : 0)} MWh` : num(v, v % 1 ? 1 : 0);
};

/**
 * Stacked area (day power curve) or stacked bars (energy per bucket) with an
 * optional dashed target per bar or expected curve. Hover, touch and keyboard show a tooltip:
 * ←/→ move (Shift: 1 h on day curves), Home/End, Esc hides. With `onSelect`,
 * a click, a tap or Enter/Space opens the bucket; on touch, dragging a
 * finger across the chart shows the tooltip without opening anything, and it
 * hides shortly after the finger lifts.
 */
export function EnergyChart(p: EnergyChartProps) {
  const { t } = useTranslation();
  const wrapRef = useRef<HTMLDivElement>(null);
  const tipRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [idx, setIdx] = useState(-1);
  const [tipLeft, setTipLeft] = useState(0);
  // Whether the pointer pressed on the chart may open the bucket: not after a
  // touch drag, which only inspects. `downX` is where the touch started.
  const armed = useRef(false);
  const downX = useRef(0);
  const touch = useTouchTip(() => setIdx(-1));

  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setWidth(Math.round(el.clientWidth)));
    ro.observe(el);
    setWidth(Math.round(el.clientWidth));
    return () => ro.disconnect();
  }, []);

  const n = p.total.length;
  const hasExpected = p.kind === 'area' && !!p.expected?.some((v) => v != null && v > 0);
  const price = p.kind === 'area' && p.price?.some((v) => v != null) ? p.price : undefined;
  const hasData = p.total.some((v) => v != null && v > 0);
  const interactive = hasData || hasExpected;
  const selectable = (i: number) => !!p.onSelect && i >= 0 && i < n && p.total[i] != null;
  const lastIdx = useMemo(() => p.total.reduce<number>((l, v, i) => (v != null ? i : l), -1), [p.total]);

  const g = useMemo(() => {
    const W = Math.max(280, width || 600);
    const H = W < 560 ? 220 : 300;
    const m = { l: p.unit === 'W' ? 56 : 48, r: price ? 40 : 8, t: 12, b: 28 };
    const iw = W - m.l - m.r, ih = H - m.t - m.b;
    let maxV = 0;
    p.total.forEach((v) => { if (v != null) maxV = Math.max(maxV, v); });
    (p.target ?? []).forEach((v) => { if (v != null) maxV = Math.max(maxV, v); });
    if (p.kind === 'area') (p.expected ?? []).forEach((v) => { if (v != null) maxV = Math.max(maxV, v); });
    if (p.kind === 'bar') (p.compare ?? []).forEach((c) => c.values.forEach((v) => { if (v != null) maxV = Math.max(maxV, v); }));
    const sc = niceScale(maxV);
    const y = (v: number) => m.t + ih - (v / sc.max) * ih;
    const band = iw / Math.max(1, n);
    const cx = p.kind === 'bar' ? (i: number) => m.l + band * (i + 0.5) : (i: number) => m.l + (n <= 1 ? iw / 2 : (iw * i) / (n - 1));
    // Price axis: includes 0, so negative prices sit below the zero line.
    let lo = 0, hi = 0;
    price?.forEach((v) => { if (v != null) { lo = Math.min(lo, v); hi = Math.max(hi, v); } });
    const pStep = niceScale(hi - lo || 1).step;
    const pMin = Math.floor(lo / pStep) * pStep, pMax = Math.max(pMin + pStep, Math.ceil(hi / pStep) * pStep);
    const yp = (v: number) => m.t + ih - ((v - pMin) / (pMax - pMin)) * ih;
    return { W, H, m, iw, ih, sc, y, band, cx, pStep, pMin, pMax, yp };
  }, [width, p.unit, p.total, p.target, p.expected, p.compare, p.kind, n, price]);

  const svgBody = useMemo(() => {
    const { W, H, m, sc, y, band, cx, yp } = g;
    const fmtA = axisFmt(p.unit);
    const grid: ReactElement[] = [], ylab: ReactElement[] = [];
    for (let v = 0; v <= sc.max + 1e-9; v += sc.step) {
      grid.push(<line key={v} x1={m.l} x2={W - m.r} y1={y(v).toFixed(1)} y2={y(v).toFixed(1)} />);
      ylab.push(<text key={v} x={m.l - 8} y={(y(v) + 4).toFixed(1)} textAnchor="end">{fmtA(v)}</text>);
    }
    const xlab: ReactElement[] = [];
    const minGap = p.kind === 'bar' ? 26 : 44;
    let lastX = -1e9;
    for (let i = 0; i < n; i++) {
      const t = p.tick(i, band);
      if (t == null) continue;
      const x = cx(i);
      if (x - lastX < minGap) continue;
      lastX = x;
      xlab.push(<text key={i} x={x.toFixed(1)} y={H - 8} textAnchor="middle">{t}</text>);
    }
    const data: ReactElement[] = [];
    const priceMarks: ReactElement[] = [];
    if (price) {
      const end = (i: number) => cx(Math.min(i + 1, n - 1));
      // Shade runs of negative prices behind the curve.
      let from = -1;
      for (let i = 0; i <= n; i++) {
        const neg = i < n && price[i] != null && price[i]! < 0;
        if (neg && from < 0) from = i;
        if (!neg && from >= 0) {
          priceMarks.push(<rect key={`n${from}`} className="neg-price" x={cx(from).toFixed(1)} y={m.t} width={(end(i - 1) - cx(from)).toFixed(1)} height={g.ih} />);
          from = -1;
        }
      }
      // Step line: each value holds until the next bucket.
      let d = '';
      for (let i = 0; i < n; i++) {
        const v = price[i];
        if (v == null) continue;
        const prev = i > 0 ? price[i - 1] : null;
        if (prev == null) d += `M${cx(i).toFixed(1)},${yp(v).toFixed(1)}`;
        else if (prev !== v) d += `H${cx(i).toFixed(1)}V${yp(v).toFixed(1)}`;
        if (i === n - 1 || price[i + 1] == null) d += `H${end(i).toFixed(1)}`;
      }
      if (d) data.push(<path key="price" className="price" d={d} />);
      for (let v = g.pMin; v <= g.pMax + 1e-9; v += g.pStep) {
        ylab.push(<text key={`p${v}`} className="price-axis" x={W - m.r + 6} y={(yp(v) + 4).toFixed(1)} textAnchor="start">{num(v, g.pStep % 1 ? 1 : 0)}</text>);
      }
    }
    const S = p.series;
    if (p.kind === 'area') {
      const cum: (number | null)[][] = [];
      S.forEach((s, k) => { cum[k] = s.values.map((v, i) => (v == null ? null : (v || 0) + (k ? cum[k - 1][i] || 0 : 0))); });
      // One area per run of buckets with data (the axis may start before the first reading).
      const runs: [number, number][] = [];
      for (let i = 0; i < n; i++) {
        if (p.total[i] == null) continue;
        const r = runs[runs.length - 1];
        if (r && r[1] === i - 1) r[1] = i; else runs.push([i, i]);
      }
      S.forEach((s, k) => {
        let fill = '', line = '';
        for (const [from, to] of runs) {
          let top = '', bot = '';
          for (let i = from; i <= to; i++) top += `${i > from ? 'L' : 'M'}${cx(i).toFixed(1)},${y(cum[k][i] || 0).toFixed(1)}`;
          for (let i = to; i >= from; i--) bot += `L${cx(i).toFixed(1)},${y(k ? cum[k - 1][i] || 0 : 0).toFixed(1)}`;
          fill += `${top}${bot}Z`;
          line += top;
        }
        if (!line) return;
        data.push(<path key={`f${k}`} d={fill} fill={s.color} fillOpacity={S.length > 1 ? 0.22 : 0.14} stroke="none" />);
        data.push(<path key={`l${k}`} d={line} fill="none" stroke={s.color} strokeWidth={1.75} strokeLinejoin="round" />);
      });
      if (p.expected) {
        // One segment per run of known values.
        let d = '', open = false;
        for (let i = 0; i < n; i++) {
          const v = p.expected[i];
          if (v == null) { open = false; continue; }
          d += `${open ? 'L' : 'M'}${cx(i).toFixed(1)},${y(v).toFixed(1)}`;
          open = true;
        }
        if (d) data.push(<path key="expected" className="target" d={d} fill="none" strokeLinejoin="round" />);
      }
    } else {
      const bw = Math.max(2, Math.min(band * 0.7, 44));
      for (let i = 0; i < n; i++) {
        let acc = 0;
        S.forEach((s, k) => {
          const v = s.values[i];
          if (v == null || v <= 0) return;
          const y0 = y(acc), y1 = y(acc + v);
          acc += v;
          data.push(<rect key={`${i}-${k}`} x={(cx(i) - bw / 2).toFixed(1)} y={y1.toFixed(1)} width={bw.toFixed(1)} height={Math.max(0.5, y0 - y1).toFixed(1)} fill={s.color} />);
        });
      }
      p.target?.forEach((t, i) => {
        if (t == null) return;
        const hw = Math.min(band * 0.45, 28);
        data.push(<line key={`t${i}`} className="target" x1={(cx(i) - hw).toFixed(1)} x2={(cx(i) + hw).toFixed(1)} y1={y(t).toFixed(1)} y2={y(t).toFixed(1)} />);
      });
      // Oldest first, so the newest comparison is drawn on top.
      const cmp = p.compare ?? [];
      [...cmp].reverse().forEach((c) => {
        const k = cmp.indexOf(c);
        const op = compareOpacity(k, cmp.length);
        let d = '', open = false;
        const dots: ReactElement[] = [];
        c.values.forEach((v, i) => {
          if (v == null) { open = false; return; }
          d += `${open ? 'L' : 'M'}${cx(i).toFixed(1)},${y(v).toFixed(1)}`;
          open = true;
          dots.push(<circle key={i} cx={cx(i).toFixed(1)} cy={y(v).toFixed(1)} r={2.5} />);
        });
        data.push(
          <g key={`c${c.key}`} className="compare" style={{ opacity: op }}>
            <path d={d} fill="none" strokeLinejoin="round" />{dots}
          </g>,
        );
      });
    }
    return (
      <>
        <g className="chart-grid">{grid}</g>
        <g>{priceMarks}</g>
        <g className="chart-axis">{ylab}{xlab}</g>
        <g>{data}</g>
        <line className="baseline" x1={m.l} x2={W - m.r} y1={y(0)} y2={y(0)} />
      </>
    );
  }, [g, p, n, price]);

  // Position the tooltip beside the hovered bucket, flipping at the right edge.
  useLayoutEffect(() => {
    if (idx < 0 || !wrapRef.current || !tipRef.current) return;
    const scale = wrapRef.current.clientWidth / g.W;
    const tw = tipRef.current.offsetWidth;
    const x = g.cx(idx) * scale;
    let left = x + 14;
    if (left + tw > wrapRef.current.clientWidth) left = x - tw - 14;
    setTipLeft(Math.max(0, left));
  }, [idx, g]);

  const nearest = (px: number) => {
    const i = p.kind === 'bar' ? Math.floor((px - g.m.l) / g.band) : Math.round(((px - g.m.l) / g.iw) * (n - 1));
    return Math.max(0, Math.min(n - 1, i));
  };
  const fromEvent = (e: PointerEvent<SVGSVGElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    return ((e.clientX - r.left) * g.W) / r.width;
  };
  const onKey = (e: KeyboardEvent<SVGSVGElement>) => {
    if (!interactive) return;
    const step = p.kind === 'area' && e.shiftKey ? 12 : 1;
    const cur = idx < 0 ? 0 : idx;
    if (e.key === 'ArrowRight') setIdx(Math.min(n - 1, cur + step));
    else if (e.key === 'ArrowLeft') setIdx(Math.max(0, cur - step));
    else if (e.key === 'Home') setIdx(0);
    else if (e.key === 'End') setIdx(Math.max(0, lastIdx));
    else if (e.key === 'Escape') setIdx(-1);
    else if ((e.key === 'Enter' || e.key === ' ') && selectable(idx)) p.onSelect!(idx);
    else return;
    e.preventDefault();
  };

  let hover = null, tip = null;
  if (idx >= 0 && idx < n) {
    const x = g.cx(idx);
    if (p.kind === 'bar') {
      hover = <rect className="band" x={(x - g.band / 2).toFixed(1)} y={g.m.t} width={g.band.toFixed(1)} height={g.ih} />;
    } else {
      const dots: ReactElement[] = [];
      let acc = 0;
      p.series.forEach((s, k) => {
        const v = s.values[idx];
        if (v == null) return;
        acc += v;
        dots.push(<circle key={k} cx={x} cy={g.y(acc)} r={3.5} fill={s.color} stroke="var(--surface)" strokeWidth={1.5} />);
      });
      hover = <><line className="cross" x1={x} x2={x} y1={g.m.t} y2={g.m.t + g.ih} />{dots}</>;
    }
    const tot = p.total[idx];
    const tg = p.target?.[idx];
    const ex = p.kind === 'area' ? p.expected?.[idx] : undefined;
    const multi = p.series.length > 1;
    // Many series: two columns, so the tooltip fits inside the chart card (which clips it).
    const cols = p.series.length > TIP_ONE_COLUMN_MAX;
    tip = (
      <div className={cols ? 'chart-tip cols' : 'chart-tip'} ref={tipRef} aria-live="polite" style={{ transform: `translate(${tipLeft}px, ${g.m.t}px)` }}>
        <div className="t">{p.labels[idx]}</div>
        {multi && (
          <div className="rows" style={cols ? { gridTemplateRows: `repeat(${Math.ceil(p.series.length / 2)}, auto)` } : undefined}>
            {p.series.map((s) => (
              <div className="row" key={s.key}>
                <span><span className="swatch" style={{ background: s.color }} /><span className="truncate">{s.name}</span></span>
                <span>{p.fmt(s.values[idx])}</span>
              </div>
            ))}
          </div>
        )}
        <div className={multi ? 'row total' : 'row'}><span>{t('common.total')}</span><span>{p.fmt(tot)}</span></div>
        {tg != null && (
          <div className="row"><span>{p.targetLabel ?? t('common.target')}</span><span>{p.fmt(tg)}{tot != null && tg > 0 ? ` · ${num((tot / tg) * 100, 0)} %` : ''}</span></div>
        )}
        {ex != null && (
          <div className="row"><span>{t('common.expected')}</span><span>{p.fmt(ex)}{tot != null && ex > 0 ? ` · ${num((tot / ex) * 100, 0)} %` : ''}</span></div>
        )}
        {price?.[idx] != null && (
          <div className="row"><span><span className="price-key" />{t('chart.price')}</span><span>{p.priceFmt ? p.priceFmt(price[idx]!) : num(price[idx]!, 1)}</span></div>
        )}
        {p.kind === 'bar' && p.compare?.map((c, k) => {
          const v = c.values[idx];
          if (v == null) return null;
          const delta = tot != null && v > 0 ? (tot / v - 1) * 100 : null;
          return (
            <div className="row" key={c.key}>
              <span><span className="compare-key" style={{ opacity: compareOpacity(k, p.compare!.length) }} />{c.name}</span>
              <span>{p.fmt(v)}{delta != null ? ` · ${delta >= 0 ? '+' : '−'}${num(Math.abs(delta), 0)} %` : ''}</span>
            </div>
          );
        })}
        {p.selectHint && selectable(idx) && <div className="hint">{p.selectHint}</div>}
      </div>
    );
  }

  return (
    <div className="chart" ref={wrapRef}>
      <svg
        tabIndex={0}
        role="img"
        aria-label={p.ariaLabel}
        style={{ touchAction: 'pan-y', cursor: selectable(idx) ? 'pointer' : undefined }}
        viewBox={`0 0 ${g.W} ${g.H}`}
        width={g.W}
        height={g.H}
        onPointerMove={(e) => {
          if (!interactive) return;
          if (e.pointerType === 'touch' && Math.abs(e.clientX - downX.current) > 8) armed.current = false;
          setIdx(nearest(fromEvent(e)));
        }}
        onPointerDown={(e) => {
          if (!interactive) return;
          touch.hold();
          armed.current = true;
          downX.current = e.clientX;
          setIdx(nearest(fromEvent(e)));
        }}
        // Don't focus on mouse/touch, so a click shows no focus ring; Tab still focuses.
        onMouseDown={(e) => e.preventDefault()}
        onClick={(e) => {
          if (!interactive || !armed.current) return;
          armed.current = false;
          const r = e.currentTarget.getBoundingClientRect();
          const i = nearest(((e.clientX - r.left) * g.W) / r.width);
          if (selectable(i)) p.onSelect!(i);
        }}
        onPointerUp={touch.release}
        onPointerCancel={touch.release}
        onPointerLeave={(e) => e.pointerType === 'mouse' && setIdx(-1)}
        onFocus={() => interactive && idx < 0 && setIdx(Math.max(0, lastIdx))}
        onBlur={() => setIdx(-1)}
        onKeyDown={onKey}
      >
        {svgBody}
        <g>{hover}</g>
      </svg>
      {tip}
      {!interactive && <div className="chart-empty">{p.emptyText ?? t('chart.noData')}</div>}
    </div>
  );
}

export function ChartLegend({ series, target, targetLabel, expected, compare, price, negativePrice }: {
  series: ChartSeries[]; target?: boolean; targetLabel?: string; expected?: boolean; compare?: CompareSeries[];
  /** Day-ahead price line (ct/kWh, right axis) and whether negative intervals are shaded. */
  price?: boolean; negativePrice?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="legend">
      {series.map((s) => <span key={s.key}><span className="swatch" style={{ background: s.color }} />{s.name}</span>)}
      {target && <span><span className="target-key" />{targetLabel ?? t('common.target')}</span>}
      {expected && <span><span className="target-key" />{t('common.expected')}</span>}
      {price && <span><span className="price-key" />{t('chart.priceAxis')}</span>}
      {price && negativePrice && <span><span className="neg-price-key" />{t('chart.negativePrice')}</span>}
      {compare?.map((c, k) => <span key={c.key}><span className="compare-key" style={{ opacity: compareOpacity(k, compare.length) }} />{c.name}</span>)}
    </div>
  );
}
