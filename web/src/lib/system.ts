import type { System } from '@/api/types';
import { partsIn, ymd } from './format';

/** Commissioning date, falling back to the day the system was created. */
export const commissioned = (s: System) => s.commissioned_at ?? ymd(partsIn(new Date(s.created_at), s.timezone));
