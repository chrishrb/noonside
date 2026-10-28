import i18n from '@/i18n';
import type { Me, Role } from '@/api/types';

export type Scope =
  | 'system:read' | 'system:write' | 'system:delete' | 'inverter:write'
  | 'data:read' | 'data:export' | 'data:ingest' | 'data:edit' | 'events:read'
  | 'members:read' | 'members:invite' | 'members:manage'
  | 'datasource:manage' | 'apikey:manage' | 'alerts:manage' | 'share:manage' | 'webhook:manage';

// Mirrors internal/auth/scopes.go (shown in the members tab).
const VIEWER: Scope[] = ['system:read', 'data:read', 'data:export', 'events:read'];
const ADMIN: Scope[] = [...VIEWER, 'system:write', 'inverter:write', 'data:ingest', 'data:edit', 'members:read', 'members:invite', 'members:manage', 'datasource:manage', 'apikey:manage', 'alerts:manage', 'share:manage', 'webhook:manage'];
export const ROLE_SCOPES: Record<Role, Scope[]> = { viewer: VIEWER, admin: ADMIN, owner: [...ADMIN, 'system:delete'] };

export const ROLES: Role[] = ['owner', 'admin', 'viewer'];
export const roleLabel = (r: Role) => i18n.t(`roles.${r}.label`);
export const roleHelp = (r: Role) => i18n.t(`roles.${r}.help`);

export const membership = (me: Me | undefined, sid: string | undefined) => me?.systems.find((s) => s.system_id === sid);

/** Whether the caller has `scope` on the system. The server enforces this too. */
export function can(me: Me | undefined, sid: string | undefined, scope: Scope): boolean {
  if (!me) return false;
  if (me.is_superadmin) return true;
  return !!membership(me, sid)?.scopes.includes(scope);
}

/** Effective role for role-assignment rules; superadmins act as owner. */
export const actorRole = (me: Me | undefined, sid: string | undefined): Role | null =>
  me?.is_superadmin ? 'owner' : membership(me, sid)?.role ?? null;

/** Only owners can grant (or revoke) owner. */
export const assignableRoles = (actor: Role | null): Role[] =>
  actor === 'owner' ? ROLES : actor === 'admin' ? ['admin', 'viewer'] : [];
