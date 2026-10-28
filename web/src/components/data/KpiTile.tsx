import type { ReactNode } from 'react';
import { Icon, type IconName } from '@/components/ui/Icon';
import { ProgressBar } from '@/components/ui/primitives';
import { splitUnit } from '@/lib/format';

/** "12.3 kWh" with the unit styled smaller. */
export function ValueUnit({ value }: { value: string }) {
  const [v, u] = splitUnit(value);
  return <>{v}{u && <span className="unit">{u}</span>}</>;
}

export interface KpiProps {
  label: string;
  icon?: IconName;
  /** A formatted value like "12.3 kWh"; the unit is split off automatically. */
  value: string | ReactNode;
  sub?: ReactNode;
  progress?: number | null;
  progressTone?: 'accent' | 'ok' | 'low';
}

export function KpiTile({ label, icon, value, sub, progress, progressTone }: KpiProps) {
  return (
    <div className="kpi">
      <span className="label">{icon && <Icon name={icon} size="sm" />}{label}</span>
      <span className="value">{typeof value === 'string' ? <ValueUnit value={value} /> : value}</span>
      {sub && <span className="sub">{sub}</span>}
      {progress != null && <ProgressBar value={progress} tone={progressTone} />}
    </div>
  );
}

export const KpiRow = ({ children }: { children: ReactNode }) => <div className="kpis">{children}</div>;
