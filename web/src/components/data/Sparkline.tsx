import i18n from '@/i18n';

/** Small area sparkline for dashboard cards. */
export function Sparkline({ values, label }: { values: (number | null | undefined)[]; label: string }) {
  const W = 240, H = 56;
  const n = values.length;
  const last = values.reduce<number>((l, x, i) => (x != null ? i : l), -1);
  if (last < 0 || n < 2) return <svg className="spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={i18n.t('chart.sparkNoData', { label })} />;
  const max = Math.max(1, ...values.map((x) => x || 0));
  let d = '';
  for (let i = 0; i <= last; i++) d += `${i ? 'L' : 'M'}${((i / (n - 1)) * W).toFixed(1)},${(H - 2 - ((values[i] || 0) / max) * (H - 6)).toFixed(1)}`;
  return (
    <svg className="spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={label}>
      <path d={`${d}L${((last / (n - 1)) * W).toFixed(1)},${H}L0,${H}Z`} fill="var(--fg)" fillOpacity={0.07} />
      <path d={d} fill="none" stroke="var(--fg)" strokeWidth={1.5} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  );
}
