package systemsapi

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/chrishrb/noonside/internal/platform/gotrue"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

const invitationPrefix = "s0i_"

func (h *Handler) ListMembers(ctx context.Context, params generated.ListMembersParams) ([]generated.Member, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeMembersRead); err != nil {
		return nil, err
	}
	ms, err := h.systems.ListMembers(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	out := make([]generated.Member, len(ms))
	for i := range ms {
		out[i] = *toMember(&ms[i])
	}
	return out, nil
}

func (h *Handler) UpdateMember(ctx context.Context, req *generated.UpdateMemberRequest, params generated.UpdateMemberParams) (*generated.Member, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeMembersManage)
	if err != nil {
		return nil, err
	}
	newRole := auth.Role(req.Role)
	var out *systems.Member
	err = h.systems.InTx(ctx, func(ctx context.Context) error {
		actor, err := h.authz.RoleOf(ctx, p, params.SystemId)
		if err != nil {
			return err
		}
		target, err := h.systems.GetMember(ctx, params.SystemId, params.UserId)
		if err != nil {
			return err
		}
		// Changing from or to owner requires owner.
		if !auth.CanAssignRole(actor, newRole) || !auth.CanAssignRole(actor, target.Role) {
			return httpapi.Forbidden("only owners can grant or revoke the owner role")
		}
		if target.Role == auth.RoleOwner && newRole != auth.RoleOwner {
			if err := h.ensureAnotherOwner(ctx, params.SystemId); err != nil {
				return err
			}
		}
		if err := h.systems.UpsertMember(ctx, params.SystemId, params.UserId, newRole); err != nil {
			return err
		}
		out, err = h.systems.GetMember(ctx, params.SystemId, params.UserId)
		return err
	})
	if err != nil {
		return nil, err
	}
	return toMember(out), nil
}

func (h *Handler) RemoveMember(ctx context.Context, params generated.RemoveMemberParams) error {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return err
	}
	// Members may always leave; the lookup below returns 404 for non-members.
	self := p.UserID == params.UserId
	if !self {
		if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeMembersManage); err != nil {
			return err
		}
	}
	return h.systems.InTx(ctx, func(ctx context.Context) error {
		target, err := h.systems.GetMember(ctx, params.SystemId, params.UserId)
		if err != nil {
			return err
		}
		if !self {
			actor, err := h.authz.RoleOf(ctx, p, params.SystemId)
			if err != nil {
				return err
			}
			if !auth.CanAssignRole(actor, target.Role) {
				return httpapi.Forbidden("only owners can remove owners")
			}
		}
		if target.Role == auth.RoleOwner {
			if err := h.ensureAnotherOwner(ctx, params.SystemId); err != nil {
				return err
			}
		}
		return h.systems.DeleteMember(ctx, params.SystemId, params.UserId)
	})
}

func (h *Handler) ensureAnotherOwner(ctx context.Context, systemID uuid.UUID) error {
	n, err := h.systems.CountOwners(ctx, systemID)
	if err != nil {
		return err
	}
	if n <= 1 {
		return httpapi.Conflict("a system must keep at least one owner")
	}
	return nil
}

func (h *Handler) ListInvitations(ctx context.Context, params generated.ListInvitationsParams) ([]generated.Invitation, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeMembersRead); err != nil {
		return nil, err
	}
	invs, err := h.systems.ListInvitations(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	out := make([]generated.Invitation, len(invs))
	for i := range invs {
		out[i] = toInvitation(&invs[i])
	}
	return out, nil
}

func (h *Handler) CreateInvitation(ctx context.Context, req *generated.CreateInvitationRequest, params generated.CreateInvitationParams) (*generated.CreatedInvitation, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeMembersInvite)
	if err != nil {
		return nil, err
	}
	role := auth.Role(req.Role)
	actor, err := h.authz.RoleOf(ctx, p, params.SystemId)
	if err != nil {
		return nil, err
	}
	if !auth.CanAssignRole(actor, role) {
		return nil, httpapi.Forbidden("only owners can invite owners")
	}
	// Opening the link creates a pre-confirmed account for the invited
	// address, so the link must only reach its owner: by email, or from a
	// superadmin, who can create accounts anyway.
	if !h.mailer.Enabled() && !p.Superadmin {
		return nil, httpapi.BadRequest("email is not configured on this server; ask a superadmin to invite")
	}
	invitee := identity.NormalizeEmail(req.Email)
	if u, err := h.identity.GetProfileByEmail(ctx, invitee); err == nil {
		if _, err := h.systems.GetMember(ctx, params.SystemId, u.ID); err == nil {
			return nil, httpapi.Conflict("user is already a member")
		}
	}
	sys, err := h.systems.GetSystem(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	token, hash, err := auth.NewOpaqueToken(invitationPrefix)
	if err != nil {
		return nil, err
	}
	inv, err := h.systems.CreateInvitation(ctx, &systems.Invitation{
		SystemID: params.SystemId, Email: invitee, Role: role, TokenHash: hash,
		InvitedBy: &p.UserID, ExpiresAt: h.now().Add(h.cfg.InvitationTTL),
	})
	if err != nil {
		return nil, err
	}
	acceptURL := strings.TrimRight(h.cfg.PublicURL, "/") + "/invitations/" + url.PathEscape(token)
	out := &generated.CreatedInvitation{Invitation: toInvitation(inv)}
	switch {
	case h.sendInvitationEmail(ctx, p, sys, inv, acceptURL):
		out.EmailSent = true
	case p.Superadmin:
		out.Token = generated.NewOptString(token)
		out.AcceptURL = generated.NewOptString(acceptURL)
	default:
		// Nobody can use it: drop it so it doesn't linger as pending.
		if err := h.systems.RevokeInvitation(ctx, params.SystemId, inv.ID); err != nil {
			h.logger.WarnContext(ctx, "revoke unsent invitation", "invitation_id", inv.ID, "error", err)
		}
		return nil, httpapi.BadGateway("the invitation email could not be sent; try again later")
	}
	return out, nil
}

// sendInvitationEmail mails the link. Failures are logged and reported as
// false; CreateInvitation decides what the caller gets instead.
func (h *Handler) sendInvitationEmail(ctx context.Context, p *auth.Principal, sys *systems.System, inv *systems.Invitation, acceptURL string) bool {
	if !h.mailer.Enabled() {
		return false
	}
	inviter := ""
	if u, err := h.identity.GetProfile(ctx, p.UserID); err == nil {
		inviter = u.Name
		if inviter == "" {
			inviter = u.Email
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	err := h.mailer.Send(ctx, inv.Email, email.Invitation{
		SystemName:  sys.Name,
		InviterName: inviter,
		Role:        string(inv.Role),
		AcceptURL:   acceptURL,
		ExpiresAt:   inv.ExpiresAt.In(sys.Loc()),
	})
	if err != nil {
		h.logger.WarnContext(ctx, "send invitation email", "invitation_id", inv.ID, "error", err)
		return false
	}
	return true
}

func (h *Handler) RevokeInvitation(ctx context.Context, params generated.RevokeInvitationParams) error {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeMembersInvite); err != nil {
		return err
	}
	return h.systems.RevokeInvitation(ctx, params.SystemId, params.InvitationId)
}

// validInvitation looks up a pending invitation by its token.
func (h *Handler) validInvitation(ctx context.Context, token string) (*systems.Invitation, error) {
	if !strings.HasPrefix(token, invitationPrefix) {
		return nil, httpapi.NotFound("invitation not found")
	}
	inv, err := h.systems.GetInvitationByHash(ctx, auth.HashToken(token))
	if errors.Is(err, db.ErrNotFound) {
		return nil, httpapi.NotFound("invitation not found")
	}
	if err != nil {
		return nil, err
	}
	if inv.AcceptedAt != nil || inv.RevokedAt != nil || h.now().After(inv.ExpiresAt) {
		return nil, httpapi.NotFound("invitation not found")
	}
	return inv, nil
}

func (h *Handler) GetInvitation(ctx context.Context, params generated.GetInvitationParams) (*generated.InvitationInfo, error) {
	inv, err := h.validInvitation(ctx, params.Token)
	if err != nil {
		return nil, err
	}
	sys, err := h.systems.GetSystem(ctx, inv.SystemID)
	if err != nil {
		return nil, err
	}
	_, err = h.identity.GetProfileByEmail(ctx, inv.Email)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	return &generated.InvitationInfo{
		SystemName: sys.Name, Email: inv.Email, Role: generated.Role(inv.Role),
		ExpiresAt: inv.ExpiresAt, UserExists: err == nil,
	}, nil
}

func (h *Handler) AcceptInvitation(ctx context.Context, req generated.OptAcceptInvitationRequest, params generated.AcceptInvitationParams) (*generated.AcceptInvitationResult, error) {
	inv, err := h.validInvitation(ctx, params.Token)
	if err != nil {
		return nil, err
	}

	var userID uuid.UUID
	created := false
	if p := auth.FromContext(ctx); p != nil && p.IsUser() {
		// GoTrue only issues tokens for confirmed addresses (sign-up is
		// disabled; accounts are created pre-confirmed below or by an admin).
		if p.Email != inv.Email {
			return nil, httpapi.Forbidden("invitation was sent to a different email address")
		}
		userID = p.UserID
	} else {
		if _, err := h.identity.GetProfileByEmail(ctx, inv.Email); err == nil {
			return nil, httpapi.Unauthorized("an account for this email exists; sign in to accept the invitation")
		} else if !errors.Is(err, db.ErrNotFound) {
			return nil, err
		}
		body, _ := req.Get()
		name, hasName := body.Name.Get()
		password, hasPassword := body.Password.Get()
		if !hasName || !hasPassword {
			return nil, httpapi.BadRequest("name and password are required to create an account")
		}
		// The invitation link proves ownership of the address.
		id, err := h.identity.CreateUser(ctx, identity.NewUser{Email: inv.Email, Password: password, Name: name})
		var ae *gotrue.APIError
		switch {
		case errors.Is(err, gotrue.ErrUserExists):
			return nil, httpapi.Unauthorized("an account for this email exists; sign in to accept the invitation")
		case errors.As(err, &ae) && ae.WeakPassword():
			return nil, httpapi.BadRequest(ae.Msg)
		case err != nil:
			return nil, err
		}
		userID, created = id, true
	}

	var role auth.Role
	err = h.systems.InTx(ctx, func(ctx context.Context) error {
		inv, err := h.validInvitation(ctx, params.Token)
		if err != nil {
			return err
		}
		role = inv.Role
		// Never downgrade an existing membership through an invitation.
		if m, err := h.systems.GetMember(ctx, inv.SystemID, userID); err == nil && roleRank(m.Role) >= roleRank(inv.Role) {
			role = m.Role
		} else if err := h.systems.UpsertMember(ctx, inv.SystemID, userID, inv.Role); err != nil {
			return err
		}
		return h.systems.MarkInvitationAccepted(ctx, inv.ID)
	})
	if err != nil {
		return nil, err
	}
	return &generated.AcceptInvitationResult{SystemID: inv.SystemID, Role: generated.Role(role), AccountCreated: created}, nil
}

func roleRank(r auth.Role) int {
	switch r {
	case auth.RoleOwner:
		return 3
	case auth.RoleAdmin:
		return 2
	case auth.RoleViewer:
		return 1
	}
	return 0
}
