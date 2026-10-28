// Package auth verifies Supabase access tokens and defines the role/scope model.
package auth

import (
	"slices"
)

// Scope is a single permission on a system.
type Scope string

const (
	ScopeSystemRead       Scope = "system:read"
	ScopeSystemWrite      Scope = "system:write"
	ScopeSystemDelete     Scope = "system:delete"
	ScopeInverterWrite    Scope = "inverter:write"
	ScopeDataRead         Scope = "data:read"
	ScopeDataIngest       Scope = "data:ingest"
	ScopeDataEdit         Scope = "data:edit"
	ScopeDataExport       Scope = "data:export"
	ScopeEventsRead       Scope = "events:read"
	ScopeMembersRead      Scope = "members:read"
	ScopeMembersInvite    Scope = "members:invite"
	ScopeMembersManage    Scope = "members:manage"
	ScopeDataSourceManage Scope = "datasource:manage"
	ScopeAPIKeyManage     Scope = "apikey:manage"
	ScopeAlertsManage     Scope = "alerts:manage"
	ScopeShareManage      Scope = "share:manage"
	ScopeWebhookManage    Scope = "webhook:manage"
)

var viewerScopes = []Scope{
	ScopeSystemRead, ScopeDataRead, ScopeDataExport, ScopeEventsRead,
}

var adminScopes = append(slices.Clone(viewerScopes),
	ScopeSystemWrite, ScopeInverterWrite, ScopeDataIngest, ScopeDataEdit,
	ScopeMembersRead, ScopeMembersInvite, ScopeMembersManage,
	ScopeDataSourceManage, ScopeAPIKeyManage, ScopeAlertsManage, ScopeShareManage, ScopeWebhookManage,
)

var ownerScopes = append(slices.Clone(adminScopes), ScopeSystemDelete)

// DeviceScopes are what an API key carries unless others are chosen.
var DeviceScopes = []Scope{ScopeDataIngest}

// KeyScopes are the only scopes an API key can carry: pushing data
// (devices) and reading it (integrations such as Home Assistant).
var KeyScopes = []Scope{ScopeDataIngest, ScopeDataRead}

// RoleScopes returns the scopes granted by a system role.
func RoleScopes(r Role) []Scope {
	switch r {
	case RoleOwner:
		return ownerScopes
	case RoleAdmin:
		return adminScopes
	case RoleViewer:
		return viewerScopes
	}
	return nil
}

// CanAssignRole reports whether a member with role actor may grant or change
// target. Only owners can create or demote owners.
func CanAssignRole(actor, target Role) bool {
	if target == RoleOwner {
		return actor == RoleOwner
	}
	return actor == RoleOwner || actor == RoleAdmin
}
