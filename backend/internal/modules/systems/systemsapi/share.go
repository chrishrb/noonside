package systemsapi

import (
	"context"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
)

// Public share links give read-only access to a system's yield without an
// account: /public/{token}/... Only the token's hash is stored.

func (h *Handler) ListShareLinks(ctx context.Context, params generated.ListShareLinksParams) ([]generated.ShareLink, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeShareManage); err != nil {
		return nil, err
	}
	ls, err := h.systems.ListShareLinks(ctx, params.SystemId, h.now())
	if err != nil {
		return nil, err
	}
	out := make([]generated.ShareLink, len(ls))
	for i := range ls {
		out[i] = toShareLink(&ls[i])
	}
	return out, nil
}

func (h *Handler) CreateShareLink(ctx context.Context, req *generated.CreateShareLinkRequest, params generated.CreateShareLinkParams) (*generated.CreatedShareLink, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeShareManage)
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		return nil, httpapi.BadRequest("label is required")
	}
	expires := httpapi.PtrOf[time.Time](req.ExpiresAt)
	if expires != nil && !expires.After(h.now()) {
		return nil, httpapi.BadRequest("expires_at must be in the future")
	}
	token, hash, err := auth.NewOpaqueToken(systems.ShareLinkPrefix)
	if err != nil {
		return nil, err
	}
	l, err := h.systems.CreateShareLink(ctx, &systems.ShareLink{
		SystemID:       params.SystemId,
		Label:          label,
		Prefix:         token[:len(systems.ShareLinkPrefix)+6],
		TokenHash:      hash,
		ShowFinancials: req.ShowFinancials.Or(false),
		ExpiresAt:      expires,
		CreatedBy:      &p.UserID,
	})
	if err != nil {
		return nil, err
	}
	return &generated.CreatedShareLink{
		ShareLink: toShareLink(l),
		Token:     token,
		URL:       strings.TrimRight(h.cfg.PublicURL, "/") + "/p/" + token,
	}, nil
}

func (h *Handler) RevokeShareLink(ctx context.Context, params generated.RevokeShareLinkParams) error {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeShareManage); err != nil {
		return err
	}
	return h.systems.RevokeShareLink(ctx, params.SystemId, params.ShareLinkId)
}

func toShareLink(l *systems.ShareLink) generated.ShareLink {
	out := generated.ShareLink{
		ID: l.ID, Label: l.Label, Prefix: l.Prefix, ShowFinancials: l.ShowFinancials,
		ExpiresAt: httpapi.OptTime(l.ExpiresAt), CreatedAt: l.CreatedAt,
	}
	if l.CreatedByName != "" {
		out.CreatedBy = generated.NewOptString(l.CreatedByName)
	}
	return out
}
