package app

import (
	"context"
	"errors"

	"github.com/chrishrb/noonside/internal/modules/datasources/hostedftp"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/errtrack"
	"github.com/chrishrb/noonside/internal/platform/httpapi"
	"github.com/chrishrb/noonside/internal/platform/httpapi/generated"
	"github.com/google/uuid"
)

// apiHandler serves every operation of api.yaml: each module's handler
// implements its own operations, and embedding them here satisfies the
// generated interface. A missing or doubly implemented operation fails to
// compile.
type apiHandler struct {
	*identityAPI
	*systemsAPI
	*telemetryAPI
	*weatherAPI
	*pricesAPI
	*adviceAPI
	*dataSourcesAPI
	*notificationsAPI
	*alertingAPI
	*webhooksAPI
	*waitlistAPI
	*analyticsAPI
	*security
}

var (
	_ generated.Handler         = (*apiHandler)(nil)
	_ generated.SecurityHandler = (*apiHandler)(nil)
)

// NewError maps handler and ogen errors to responses.
func (*apiHandler) NewError(ctx context.Context, err error) *generated.ErrorStatusCode {
	return httpapi.NewError(ctx, err)
}

// TokenVerifier validates Supabase access tokens (implemented by
// auth.Verifier).
type TokenVerifier interface {
	Verify(token string) (*auth.Principal, error)
}

// security authenticates requests: users by their Supabase access token,
// devices and integrations by a system's API key, the hosted FTP server by
// its hook token.
type security struct {
	tokens  TokenVerifier
	systems *systems.Module
	hosted  *hostedftp.Service // nil without hosted FTP
}

func (s *security) HandleHostedFtpHookAuth(ctx context.Context, _ generated.OperationName, t generated.HostedFtpHookAuth) (context.Context, error) {
	if s.hosted == nil || !s.hosted.ValidHookToken(t.Token) {
		return nil, httpapi.Unauthorized("invalid hook token")
	}
	return ctx, nil
}

func (s *security) HandleBearerAuth(ctx context.Context, _ generated.OperationName, t generated.BearerAuth) (context.Context, error) {
	p, err := s.tokens.Verify(t.Token)
	if err != nil {
		return nil, httpapi.Unauthorized("invalid or expired token")
	}
	p.Systems = map[uuid.UUID][]auth.Scope{}
	errtrack.SetUser(ctx, p.UserID.String())
	return auth.WithPrincipal(ctx, p), nil
}

func (s *security) HandleApiKeyAuth(ctx context.Context, _ generated.OperationName, t generated.ApiKeyAuth) (context.Context, error) {
	p, err := s.systems.AuthenticateAPIKey(ctx, t.APIKey)
	if errors.Is(err, db.ErrNotFound) {
		return nil, httpapi.Unauthorized("invalid api key")
	}
	if err != nil {
		return nil, err
	}
	errtrack.SetTag(ctx, "api_key", p.APIKeyID.String())
	for id := range p.Systems {
		errtrack.SetTag(ctx, "system_id", id.String())
	}
	return auth.WithPrincipal(ctx, p), nil
}
