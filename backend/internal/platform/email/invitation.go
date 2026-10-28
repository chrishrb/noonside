package email

import (
	"fmt"
	"time"
)

// Invitation invites someone to a system.
type Invitation struct {
	SystemName  string
	InviterName string
	Role        string
	AcceptURL   string
	ExpiresAt   time.Time
}

func (Invitation) Name() string { return "invitation" }

func (e Invitation) Subject() string {
	return fmt.Sprintf("You're invited to %s", e.SystemName)
}

func (e Invitation) Preheader() string {
	return fmt.Sprintf("%s invited you to view the PV system %s.", e.inviter(), e.SystemName)
}

func (e Invitation) inviter() string {
	if e.InviterName == "" {
		return "Someone"
	}
	return e.InviterName
}

// Inviter is the inviter's name for templates.
func (e Invitation) Inviter() string { return e.inviter() }

// RoleDescription explains what the role allows.
func (e Invitation) RoleDescription() string {
	switch e.Role {
	case "owner":
		return "full control, including members and deleting the system"
	case "admin":
		return "manage the system, inverters, data sources and members"
	default:
		return "view dashboards, yields and the event log"
	}
}

func init() {
	Register(func() Email {
		return Invitation{
			SystemName:  "PV-Anlage Muster",
			InviterName: "Alex",
			Role:        "viewer",
			AcceptURL:   "http://localhost:5173/invitations/s0i_example",
			ExpiresAt:   time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC),
		}
	})
}
