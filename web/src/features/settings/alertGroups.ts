/** Groups of alert rules in the settings. Kinds not listed go to "other". */
export const ALERT_GROUPS = {
  availability: ['no_data', 'inverter_offline', 'inverter_error', 'data_source_failing'],
  yield: ['underperformance', 'zero_day', 'low_yield_vs_expected'],
  limits: ['clipping', 'feed_in_cap'],
  data: ['implausible_data'],
} as const;

export type AlertGroup = keyof typeof ALERT_GROUPS | 'other';

export const GROUP_KEYS: AlertGroup[] = [...(Object.keys(ALERT_GROUPS) as AlertGroup[]), 'other'];

export function groupOf(kind: string): AlertGroup {
  for (const [g, kinds] of Object.entries(ALERT_GROUPS)) {
    if ((kinds as readonly string[]).includes(kind)) return g as AlertGroup;
  }
  return 'other';
}
