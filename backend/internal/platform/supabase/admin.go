// Package supabase is a minimal client for the Supabase Auth admin API.
package supabase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"

	"github.com/google/uuid"
)

var (
	// ErrUserExists is returned when an account with the email already exists.
	ErrUserExists = errors.New("user already exists")
)

// APIError is an error response from Supabase Auth.
type APIError struct {
	Status int
	Code   string `json:"error_code"`
	Msg    string `json:"msg"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("supabase auth: %d %s: %s", e.Status, e.Code, e.Msg)
}

// WeakPassword reports whether the password was rejected by the policy.
func (e *APIError) WeakPassword() bool { return e.Code == "weak_password" }

type User struct {
	ID          uuid.UUID      `json:"id"`
	Email       string         `json:"email"`
	AppMetadata map[string]any `json:"app_metadata"`
}

// Admin calls the Auth admin API with the service role key. Never expose
// this key to clients.
type Admin struct {
	baseURL string // <auth>/admin
	authURL string // <auth>
	key     string
	http    *http.Client
}

// NewAdmin creates the client for the Auth server at authURL: the GoTrue
// base, i.e. <project>/auth/v1 behind the gateway or GoTrue's own address
// (http://auth:9999) from inside the cluster.
func NewAdmin(authURL, secretKey string) (*Admin, error) {
	if authURL == "" || secretKey == "" {
		return nil, errors.New("auth url and secret key are required")
	}
	return &Admin{
		baseURL: strings.TrimRight(authURL, "/") + "/admin",
		authURL: strings.TrimRight(authURL, "/"),
		key:     secretKey,
		http:    errtrack.HTTPClient(15 * time.Second),
	}, nil
}

type CreateUserRequest struct {
	Email    string
	Password string
	Name     string
	// EmailConfirmed marks the address as verified (e.g. it was proven by an
	// invitation link).
	EmailConfirmed bool
}

func (a *Admin) CreateUser(ctx context.Context, r CreateUserRequest) (*User, error) {
	body := map[string]any{
		"email":         r.Email,
		"password":      r.Password,
		"email_confirm": r.EmailConfirmed,
	}
	if r.Name != "" {
		body["user_metadata"] = map[string]any{"name": r.Name}
	}
	var u User
	if err := a.do(ctx, http.MethodPost, "/users", body, &u); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Code == "email_exists" {
			return nil, ErrUserExists
		}
		return nil, err
	}
	return &u, nil
}

// SetAppMetadata merges values into the user's app_metadata. It is included
// in the user's next access token.
func (a *Admin) SetAppMetadata(ctx context.Context, id uuid.UUID, values map[string]any) (*User, error) {
	var u User
	if err := a.do(ctx, http.MethodPut, "/users/"+id.String(), map[string]any{"app_metadata": values}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// UserUpdate changes an account; nil fields stay as they are.
type UserUpdate struct {
	// Email is set confirmed (no confirmation mail).
	Email *string
	// UserMetadata replaces these keys of user_metadata.
	UserMetadata map[string]any
	// BanDuration bans the user for this long (e.g. "876000h"); "none"
	// lifts a ban. Banned users cannot sign in or refresh their session.
	BanDuration *string
}

// UpdateUser changes the account. ErrUserExists means the new email is
// taken.
func (a *Admin) UpdateUser(ctx context.Context, id uuid.UUID, r UserUpdate) (*User, error) {
	body := map[string]any{}
	if r.Email != nil {
		body["email"] = *r.Email
		body["email_confirm"] = true
	}
	if r.UserMetadata != nil {
		body["user_metadata"] = r.UserMetadata
	}
	if r.BanDuration != nil {
		body["ban_duration"] = *r.BanDuration
	}
	var u User
	if err := a.do(ctx, http.MethodPut, "/users/"+id.String(), body, &u); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Code == "email_exists" {
			return nil, ErrUserExists
		}
		return nil, err
	}
	return &u, nil
}

// DeleteUser deletes the account (public.profiles and what references it
// cascade).
func (a *Admin) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return a.do(ctx, http.MethodDelete, "/users/"+id.String(), map[string]any{}, nil)
}

// SendRecovery emails the user a password recovery link that leads to
// redirectTo (which must be allowed in the auth settings).
func (a *Admin) SendRecovery(ctx context.Context, email, redirectTo string) error {
	return a.request(ctx, http.MethodPost, a.authURL+"/recover?redirect_to="+url.QueryEscape(redirectTo),
		map[string]any{"email": email}, nil)
}

func (a *Admin) do(ctx context.Context, method, path string, body, out any) error {
	return a.request(ctx, method, a.baseURL+path, body, out)
}

func (a *Admin) request(ctx context.Context, method, target string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// New sb_secret_ keys are read from apikey; legacy service_role JWTs from both.
	req.Header.Set("apikey", a.key)
	if !strings.HasPrefix(a.key, "sb_") {
		req.Header.Set("Authorization", "Bearer "+a.key)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("supabase auth: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 300 {
		ae := &APIError{Status: resp.StatusCode}
		_ = json.NewDecoder(resp.Body).Decode(ae)
		return ae
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
