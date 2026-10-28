// Package identityapi serves the identity module's API operations: the
// caller's profile and superadmins' user management.
package identityapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/chrishrb/noonside/internal/platform/media"
	"github.com/chrishrb/noonside/internal/platform/supabase"
	"github.com/google/uuid"
)

// Systems lists the systems a user sees (implemented by the systems module).
type Systems interface {
	// UserSystems returns all systems (as owner) for nil, else the user's
	// memberships, ordered by name.
	UserSystems(ctx context.Context, userID *uuid.UUID) ([]identity.AccountSystem, error)
}

// Config is what /me reports besides the profile.
type Config struct {
	// Features are the optional features this server offers.
	Features generated.Features
	// PublicURL is the web app's origin (password reset links lead there).
	PublicURL string
}

type Handler struct {
	identity *identity.Module
	systems  Systems
	cfg      Config
}

func New(m *identity.Module, systems Systems, cfg Config) *Handler {
	return &Handler{identity: m, systems: systems, cfg: cfg}
}

// Sign-in, sessions and passwords are handled by Supabase Auth. /me only
// serves the caller's profile and memberships.

func (h *Handler) GetMe(ctx context.Context) (*generated.Me, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	prof, err := h.profile(ctx, p)
	if err != nil {
		return nil, err
	}
	return h.me(ctx, p, prof)
}

func (h *Handler) UpdateMe(ctx context.Context, req *generated.UpdateMeRequest) (*generated.Me, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	prof, err := h.profile(ctx, p)
	if err != nil {
		return nil, err
	}
	if v, ok := req.Name.Get(); ok {
		if prof, err = h.identity.RenameUser(ctx, p.UserID, v); err != nil {
			return nil, err
		}
	}
	if v, ok := req.Locale.Get(); ok {
		if prof, err = h.identity.UpdateProfileLocale(ctx, p.UserID, string(v)); err != nil {
			return nil, err
		}
	}
	return h.me(ctx, p, prof)
}

func (h *Handler) profile(ctx context.Context, p *auth.Principal) (*identity.Profile, error) {
	prof, err := h.identity.GetProfile(ctx, p.UserID)
	if errors.Is(err, db.ErrNotFound) {
		// The profile trigger runs with the account insert, so this means the
		// account was deleted after the token was issued.
		return nil, httpapi.Unauthorized("unknown user")
	}
	return prof, err
}

func (h *Handler) me(ctx context.Context, p *auth.Principal, prof *identity.Profile) (*generated.Me, error) {
	var filter *uuid.UUID
	if !p.Superadmin {
		filter = &p.UserID
	}
	systems, err := h.systems.UserSystems(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := &generated.Me{
		ID: prof.ID, Email: prof.Email, Name: prof.Name, Locale: generated.Locale(prof.Locale), IsSuperadmin: p.Superadmin,
		AvatarURL: httpapi.OptMediaURL(prof.AvatarKey),
		Systems:   make([]generated.SystemMembership, 0, len(systems)),
		Features:  h.cfg.Features,
	}
	out.Features.Uploads = h.identity.UploadsEnabled()
	for _, s := range systems {
		out.Systems = append(out.Systems, generated.SystemMembership{
			SystemID: s.SystemID, Name: s.SystemName, Role: generated.Role(s.Role), Scopes: scopeStrings(auth.RoleScopes(s.Role)),
			ImageURL: httpapi.OptMediaURL(s.ImageKey),
		})
	}
	return out, nil
}

func scopeStrings(s []auth.Scope) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = string(v)
	}
	return out
}

// User management for superadmins. It mirrors `noonside user create` and
// `noonside user promote`; adding people to systems goes through invitations.

func requireSuperadmin(ctx context.Context) (*auth.Principal, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if !p.Superadmin {
		return nil, httpapi.Forbidden("superadmin only")
	}
	return p, nil
}

func (h *Handler) ListUsers(ctx context.Context) ([]generated.AdminUser, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	accounts, err := h.identity.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]generated.AdminUser, len(accounts))
	for i := range accounts {
		out[i] = toAdminUser(&accounts[i])
	}
	return out, nil
}

func (h *Handler) CreateUser(ctx context.Context, req *generated.CreateUserRequest) (*generated.AdminUser, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, httpapi.BadRequest("name is required")
	}
	id, err := h.identity.CreateUser(ctx, identity.NewUser{
		Email: req.Email, Password: req.Password, Name: name, Superadmin: req.Superadmin.Or(false),
	})
	if err != nil {
		return nil, CreateUserError(err)
	}
	return h.adminUser(ctx, id)
}

// CreateUserError maps the errors of identity.Module.CreateUser.
func CreateUserError(err error) error {
	var ae *supabase.APIError
	switch {
	case errors.Is(err, supabase.ErrUserExists):
		return httpapi.Conflict("a user with this email already exists")
	case errors.As(err, &ae) && ae.WeakPassword():
		return httpapi.BadRequest(ae.Msg)
	}
	return err
}

func (h *Handler) GetUser(ctx context.Context, params generated.GetUserParams) (*generated.AdminUser, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	return h.adminUser(ctx, params.UserId)
}

func (h *Handler) UpdateUser(ctx context.Context, req *generated.UpdateUserRequest, params generated.UpdateUserParams) (*generated.AdminUser, error) {
	p, err := requireSuperadmin(ctx)
	if err != nil {
		return nil, err
	}
	self := params.UserId == p.UserID
	var c identity.UserChanges
	if v, ok := req.IsSuperadmin.Get(); ok {
		if self && !v {
			return nil, httpapi.BadRequest("you cannot revoke your own superadmin flag")
		}
		c.Superadmin = &v
	}
	if v, ok := req.Disabled.Get(); ok {
		if self && v {
			return nil, httpapi.BadRequest("you cannot disable your own account")
		}
		c.Disabled = &v
	}
	if v, ok := req.Name.Get(); ok {
		if strings.TrimSpace(v) == "" {
			return nil, httpapi.BadRequest("name is required")
		}
		c.Name = &v
	}
	if v, ok := req.Email.Get(); ok {
		c.Email = &v
	}
	if err := h.identity.UpdateUser(ctx, params.UserId, c); err != nil {
		return nil, userError(err)
	}
	return h.adminUser(ctx, params.UserId)
}

func (h *Handler) DeleteUser(ctx context.Context, params generated.DeleteUserParams) error {
	p, err := requireSuperadmin(ctx)
	if err != nil {
		return err
	}
	if params.UserId == p.UserID {
		return httpapi.BadRequest("you cannot delete your own account")
	}
	err = h.identity.DeleteUser(ctx, params.UserId)
	if soe := (*identity.SoleOwnerError)(nil); errors.As(err, &soe) {
		names := make([]string, len(soe.Systems))
		for i, s := range soe.Systems {
			names[i] = s.SystemName
		}
		return httpapi.Conflict("the user is the only owner of: " + strings.Join(names, ", ") +
			"; make someone else owner or delete the systems first")
	}
	return err
}

func (h *Handler) SendUserPasswordReset(ctx context.Context, params generated.SendUserPasswordResetParams) error {
	if _, err := requireSuperadmin(ctx); err != nil {
		return err
	}
	err := h.identity.SendPasswordReset(ctx, params.UserId, strings.TrimRight(h.cfg.PublicURL, "/")+"/reset-password")
	var ae *supabase.APIError
	switch {
	case errors.As(err, &ae) && ae.Status == http.StatusTooManyRequests:
		return &httpapi.Error{Status: http.StatusTooManyRequests, Msg: "too many emails were sent; try again later"}
	case errors.As(err, &ae) && ae.Status >= 500:
		// Supabase Auth sends the email itself; this is its SMTP setup.
		slog.ErrorContext(ctx, "password reset email", "error", err)
		return httpapi.BadGateway("the sign-in service could not send the email")
	}
	return err
}

// Profile pictures.

func (h *Handler) UploadMyAvatar(ctx context.Context, req generated.UploadMyAvatarReq) (*generated.Me, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.identity.SetAvatar(ctx, p.UserID, req.Data); err != nil {
		return nil, UploadError(err)
	}
	return h.GetMe(ctx)
}

func (h *Handler) DeleteMyAvatar(ctx context.Context) (*generated.Me, error) {
	p, err := httpapi.CurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.identity.ClearAvatar(ctx, p.UserID); err != nil {
		return nil, err
	}
	return h.GetMe(ctx)
}

func (h *Handler) UploadUserAvatar(ctx context.Context, req generated.UploadUserAvatarReq, params generated.UploadUserAvatarParams) (*generated.AdminUser, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	if err := h.identity.SetAvatar(ctx, params.UserId, req.Data); err != nil {
		return nil, UploadError(err)
	}
	return h.adminUser(ctx, params.UserId)
}

func (h *Handler) DeleteUserAvatar(ctx context.Context, params generated.DeleteUserAvatarParams) (*generated.AdminUser, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	if err := h.identity.ClearAvatar(ctx, params.UserId); err != nil {
		return nil, err
	}
	return h.adminUser(ctx, params.UserId)
}

// UploadError maps the errors of picture uploads.
func UploadError(err error) error {
	switch {
	case errors.Is(err, media.ErrInvalid):
		return httpapi.BadRequest(err.Error())
	case errors.Is(err, identity.ErrUploadsDisabled):
		return httpapi.NotFound("uploads are not enabled")
	}
	return err
}

// userError maps the errors of identity.Module.UpdateUser.
func userError(err error) error {
	if errors.Is(err, supabase.ErrUserExists) {
		return httpapi.Conflict("a user with this email already exists")
	}
	return err
}

func (h *Handler) adminUser(ctx context.Context, id uuid.UUID) (*generated.AdminUser, error) {
	a, err := h.identity.GetAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	out := toAdminUser(a)
	return &out, nil
}

func toAdminUser(a *identity.Account) generated.AdminUser {
	u := generated.AdminUser{
		ID: a.ID, Email: a.Email, Name: a.Name, IsSuperadmin: a.Superadmin, Disabled: a.Disabled, CreatedAt: a.CreatedAt,
		AvatarURL:    httpapi.OptMediaURL(a.AvatarKey),
		LastSignInAt: httpapi.OptTime(a.LastSignInAt),
		Systems:      make([]generated.UserSystemRole, len(a.Systems)),
	}
	for i, s := range a.Systems {
		u.Systems[i] = generated.UserSystemRole{SystemID: s.SystemID, Name: s.SystemName, Role: generated.Role(s.Role)}
	}
	return u
}
