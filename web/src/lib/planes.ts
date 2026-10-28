import type { Inverter, Plane } from '@/api/types';

/** Name of a plane, or "Plane n" (1-based) if it has none. */
export function planeName(plane: Plane, index: number, fallback: (n: number) => string): string {
  return plane.name?.trim() || fallback(index + 1);
}

/** Inverters on the plane, in inverter order. */
export function planeMembers(plane: Plane, invs: Inverter[]): string[] {
  return plane.id ? invs.filter((i) => i.plane_id === plane.id).map((i) => i.id) : [];
}

/** Whether the current selection (empty = all) shows exactly the plane's inverters. */
export function isPlaneSelected(plane: Plane, invs: Inverter[], sel: string[]): boolean {
  const ids = planeMembers(plane, invs);
  if (!ids.length) return false;
  const shown = sel.length ? sel : invs.map((i) => i.id);
  return shown.length === ids.length && ids.every((id) => shown.includes(id));
}

/**
 * Display names by inverter id. When the inverters are spread over more than
 * one plane, every inverter on a plane is prefixed with its name ("Süd · WR 7"),
 * so the grouping shows wherever the name does. Otherwise only names that
 * repeat within the system get the prefix.
 */
export function inverterLabels(invs: Inverter[], planeNames: Map<string, string>): Map<string, string> {
  const count = new Map<string, number>();
  for (const i of invs) count.set(i.name, (count.get(i.name) ?? 0) + 1);
  const planes = new Set(invs.flatMap((i) => (i.plane_id && planeNames.has(i.plane_id) ? [i.plane_id] : [])));
  return new Map(invs.map((i) => {
    const p = i.plane_id ? planeNames.get(i.plane_id) : undefined;
    return [i.id, p && (planes.size > 1 || (count.get(i.name) ?? 0) > 1) ? `${p} · ${i.name}` : i.name];
  }));
}
