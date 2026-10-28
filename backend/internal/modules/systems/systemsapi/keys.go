package systemsapi

import (
	"context"
	"slices"
	"strings"

	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
)

func (h *Handler) ListAPIKeys(ctx context.Context, params generated.ListAPIKeysParams) ([]generated.APIKey, error) {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeAPIKeyManage); err != nil {
		return nil, err
	}
	ks, err := h.systems.ListAPIKeys(ctx, params.SystemId)
	if err != nil {
		return nil, err
	}
	out := make([]generated.APIKey, len(ks))
	for i := range ks {
		out[i] = toAPIKey(&ks[i])
	}
	return out, nil
}

func (h *Handler) CreateAPIKey(ctx context.Context, req *generated.CreateAPIKeyRequest, params generated.CreateAPIKeyParams) (*generated.CreatedAPIKey, error) {
	p, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeAPIKeyManage)
	if err != nil {
		return nil, err
	}
	scopes := scopeStrings(auth.DeviceScopes)
	if len(req.Scopes) > 0 {
		scopes = scopes[:0]
		for _, s := range req.Scopes {
			if !slices.Contains(auth.KeyScopes, auth.Scope(s)) {
				return nil, httpapi.BadRequest("scopes: an API key cannot carry " + string(s))
			}
			if !slices.Contains(scopes, string(s)) {
				scopes = append(scopes, string(s))
			}
		}
	}
	key, hash, err := auth.NewOpaqueToken(systems.APIKeyPrefix)
	if err != nil {
		return nil, err
	}
	k, err := h.systems.CreateAPIKey(ctx, &systems.APIKey{
		SystemID:  params.SystemId,
		Name:      strings.TrimSpace(req.Name),
		Prefix:    key[:len(systems.APIKeyPrefix)+6],
		KeyHash:   hash,
		Scopes:    scopes,
		CreatedBy: &p.UserID,
	})
	if err != nil {
		return nil, err
	}
	return &generated.CreatedAPIKey{APIKey: toAPIKey(k), Key: key}, nil
}

func (h *Handler) RevokeAPIKey(ctx context.Context, params generated.RevokeAPIKeyParams) error {
	if _, err := h.authz.Authorize(ctx, params.SystemId, auth.ScopeAPIKeyManage); err != nil {
		return err
	}
	return h.systems.RevokeAPIKey(ctx, params.SystemId, params.ApiKeyId)
}
