import type { Me } from '@/api/types';
import { can } from '@/auth/scopes';
import type { IconName } from '@/components/ui/Icon';
import type { en } from '@/i18n/en';

type NavKey = `nav.${keyof typeof en.nav}`;

export interface SystemNavItem {
  to: string;
  icon: IconName;
  /** i18n key for the sidebar label. */
  label: NavKey;
  /** Shorter i18n key for the tab bar, if the label is too long. */
  short?: NavKey;
}

/** Pages of one system the caller may open, shared by the sidebar and the tab bar. */
export function systemNavItems(me: Me, sid: string): SystemNavItem[] {
  const items: SystemNavItem[] = [
    { to: `/s/${sid}/overview`, icon: 'chart', label: 'nav.yield' },
    { to: `/s/${sid}/status`, icon: 'activity', label: 'nav.status', short: 'nav.statusShort' },
  ];
  if (can(me, sid, 'data:export')) items.push({ to: `/s/${sid}/export`, icon: 'download', label: 'nav.export' });
  if (can(me, sid, 'system:write') || can(me, sid, 'inverter:write') || can(me, sid, 'members:read')) {
    items.push({ to: `/s/${sid}/settings`, icon: 'sliders', label: 'nav.settings' });
  }
  return items;
}
