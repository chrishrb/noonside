package gotrue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdmin(t *testing.T) {
	id := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "sb_secret_test", r.Header.Get("apikey"))
		assert.Empty(t, r.Header.Get("Authorization"), "sb_ keys are not JWTs")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/v1/admin/users":
			switch body["email"] {
			case "taken@example.com":
				w.WriteHeader(422)
				_, _ = w.Write([]byte(`{"code":422,"error_code":"email_exists","msg":"A user with this email address has already been registered"}`))
			case "weak@example.com":
				w.WriteHeader(422)
				_, _ = w.Write([]byte(`{"code":422,"error_code":"weak_password","msg":"Password should be at least 10 characters."}`))
			default:
				assert.Equal(t, true, body["email_confirm"])
				assert.Equal(t, map[string]any{"name": "Neo"}, body["user_metadata"])
				_, _ = w.Write([]byte(`{"id":"` + id.String() + `","email":"new@example.com"}`))
			}
		case r.Method == http.MethodPut && r.URL.Path == "/auth/v1/admin/users/"+id.String():
			assert.Equal(t, map[string]any{"solar0_superadmin": true}, body["app_metadata"])
			_, _ = w.Write([]byte(`{"id":"` + id.String() + `","app_metadata":{"solar0_superadmin":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a, err := NewAdmin(srv.URL+"/auth/v1/", "sb_secret_test")
	require.NoError(t, err)
	ctx := context.Background()

	u, err := a.CreateUser(ctx, CreateUserRequest{Email: "new@example.com", Password: "x", Name: "Neo", EmailConfirmed: true})
	require.NoError(t, err)
	assert.Equal(t, id, u.ID)

	_, err = a.CreateUser(ctx, CreateUserRequest{Email: "taken@example.com", EmailConfirmed: true, Name: "Neo"})
	assert.ErrorIs(t, err, ErrUserExists)

	_, err = a.CreateUser(ctx, CreateUserRequest{Email: "weak@example.com", EmailConfirmed: true, Name: "Neo"})
	var ae *APIError
	require.ErrorAs(t, err, &ae)
	assert.True(t, ae.WeakPassword())

	u, err = a.SetAppMetadata(ctx, id, map[string]any{"solar0_superadmin": true})
	require.NoError(t, err)
	assert.Equal(t, true, u.AppMetadata["solar0_superadmin"])

	_, err = NewAdmin("", "")
	assert.Error(t, err)
}
