// Package hostedftp runs the upload accounts of hosted FTP data sources
// (solarlog_hosted) on noonside's own SFTPGo server: devices (Solar-Log)
// upload their exports over FTP, SFTPGo calls the API's upload hook, and
// the scheduler then reads the files over SFTP.
package hostedftp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"
)

// Client is a minimal client for the SFTPGo REST API (v2.6): users and the
// event rule that reports uploads.
type Client struct {
	base     string // …/api/v2
	user     string
	password string
	http     *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// APIError is an error response of SFTPGo.
type APIError struct {
	Status  int
	Message string `json:"message"`
	Err     string `json:"error"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("sftpgo api: %d %s %s", e.Status, e.Message, e.Err)
}

// IsNotFound reports whether the object does not exist.
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// NewClient connects to the SFTPGo server at baseURL (e.g.
// http://sftpgo:8080) as the admin user.
func NewClient(baseURL, user, password string) (*Client, error) {
	if baseURL == "" || user == "" || password == "" {
		return nil, errors.New("sftpgo: url, admin user and password are required")
	}
	base := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/api/v2")
	return &Client{base: base + "/api/v2", user: user, password: password, http: errtrack.HTTPClient(15 * time.Second)}, nil
}

// User is an SFTPGo user (only the fields noonside reads).
type User struct {
	Username      string `json:"username"`
	Description   string `json:"description"`
	HomeDir       string `json:"home_dir"`
	QuotaSize     int64  `json:"quota_size"`
	UsedQuotaSize int64  `json:"used_quota_size"`
}

// NewUser is what CreateUser sets up.
type NewUser struct {
	Username    string
	Password    string
	HomeDir     string // absolute; SFTPGo creates it
	QuotaBytes  int64  // 0: unlimited
	MaxFileSize int64  // 0: unlimited
	Description string
}

// uploadPermissions is what an upload account may do in its directory:
// devices upload and replace files, the scheduler lists, reads and deletes
// them.
var uploadPermissions = []string{"list", "download", "upload", "overwrite", "delete", "create_dirs", "rename"}

// CreateUser creates an enabled user on the local filesystem that may only
// use FTP and SFTP (no WebDAV, web client or REST API).
func (c *Client) CreateUser(ctx context.Context, u NewUser) error {
	body := map[string]any{
		"status":      1,
		"username":    u.Username,
		"password":    u.Password,
		"description": u.Description,
		"home_dir":    u.HomeDir,
		"quota_size":  u.QuotaBytes,
		"permissions": map[string][]string{"/": uploadPermissions},
		"filesystem":  map[string]any{"provider": 0},
		"filters": map[string]any{
			"denied_protocols":     []string{"DAV", "HTTP"},
			"max_upload_file_size": u.MaxFileSize,
		},
	}
	return c.do(ctx, http.MethodPost, "/users", body, nil)
}

// GetUser returns a user.
func (c *Client) GetUser(ctx context.Context, username string) (*User, error) {
	var u User
	if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(username), nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// UpdatePassword sets a new password and disconnects the user's sessions.
// SFTPGo's PUT replaces the whole user (only the password is kept when
// left out), so it sends back the user as read with the new password.
func (c *Client) UpdatePassword(ctx context.Context, username, password string) error {
	path := "/users/" + url.PathEscape(username)
	var u map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &u); err != nil {
		return err
	}
	u["password"] = password
	return c.do(ctx, http.MethodPut, path+"?disconnect=1", u, nil)
}

// DeleteUser deletes a user (its files stay); a missing user is no error.
func (c *Client) DeleteUser(ctx context.Context, username string) error {
	err := c.do(ctx, http.MethodDelete, "/users/"+url.PathEscape(username), nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// listPage is the largest page SFTPGo returns.
const listPage = 500

// ListUsers returns all users.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	var all []User
	for offset := 0; ; offset += listPage {
		var page []User
		q := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(listPage)}, "order": {"ASC"}}
		if err := c.do(ctx, http.MethodGet, "/users?"+q.Encode(), nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < listPage {
			return all, nil
		}
	}
}

// Hook is the event rule that reports uploads to the API.
type Hook struct {
	// Name of the event action and the rule.
	Name string
	// URL the action POSTs to, with Token as bearer token.
	URL   string
	Token string
	// Users limits the rule to these username patterns (empty: all users).
	Users []string
}

// hookBody is the JSON SFTPGo sends (placeholders are JSON-escaped because
// of the content type).
const hookBody = `{"username":"{{Name}}","path":"{{VirtualPath}}","event":"{{Event}}"}`

// EnsureEventRule creates or updates the HTTP event action and the rule
// that runs it after every upload, so it is safe to call on every start.
func (c *Client) EnsureEventRule(ctx context.Context, h Hook) error {
	action := map[string]any{
		"name":        h.Name,
		"description": "noonside: import hosted FTP uploads",
		"type":        1, // HTTP
		"options": map[string]any{
			"http_config": map[string]any{
				"endpoint": h.URL,
				"method":   http.MethodPost,
				"timeout":  20,
				"headers": []map[string]string{
					{"key": "Authorization", "value": "Bearer " + h.Token},
					{"key": "Content-Type", "value": "application/json"},
				},
				"body": hookBody,
			},
		},
	}
	if err := c.upsert(ctx, "/eventactions", h.Name, action); err != nil {
		return fmt.Errorf("event action: %w", err)
	}
	conditions := map[string]any{"fs_events": []string{"upload"}}
	if len(h.Users) > 0 {
		names := make([]map[string]any, len(h.Users))
		for i, u := range h.Users {
			names[i] = map[string]any{"pattern": u}
		}
		conditions["options"] = map[string]any{"names": names}
	}
	rule := map[string]any{
		"name":        h.Name,
		"description": "noonside: report uploads to the API",
		"status":      1,
		"trigger":     1, // filesystem event
		"conditions":  conditions,
		"actions":     []map[string]any{{"name": h.Name, "order": 1}},
	}
	if err := c.upsert(ctx, "/eventrules", h.Name, rule); err != nil {
		return fmt.Errorf("event rule: %w", err)
	}
	return nil
}

// DeleteEventRule removes the rule and its action (missing ones are no
// error).
func (c *Client) DeleteEventRule(ctx context.Context, name string) error {
	for _, p := range []string{"/eventrules/", "/eventactions/"} {
		if err := c.do(ctx, http.MethodDelete, p+url.PathEscape(name), nil, nil); err != nil && !IsNotFound(err) {
			return err
		}
	}
	return nil
}

// upsert replaces the named object, or creates it if it does not exist.
func (c *Client) upsert(ctx context.Context, collection, name string, body any) error {
	err := c.do(ctx, http.MethodPut, collection+"/"+url.PathEscape(name), body, nil)
	if IsNotFound(err) {
		err = c.do(ctx, http.MethodPost, collection, body, nil)
	}
	return err
}

// accessToken returns a cached admin token, or gets a new one.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/token", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.user, c.password)
	var t struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := c.send(req, &t); err != nil {
		return "", fmt.Errorf("sftpgo token: %w", err)
	}
	c.token = t.AccessToken
	// Renew a minute early, so a token never expires in flight.
	c.expires = t.ExpiresAt.Add(-time.Minute)
	return c.token, nil
}

func (c *Client) dropToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
}

// do sends an authenticated request; on 401 it gets a new token and tries
// once more (SFTPGo revokes tokens on restart).
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		token, err := c.accessToken(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		err = c.send(req, out)
		var apiErr *APIError
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
			c.dropToken()
			continue
		}
		return err
	}
}

func (c *Client) send(req *http.Request, out any) error {
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(res.Body, 10<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		e := &APIError{Status: res.StatusCode}
		_ = json.Unmarshal(data, e)
		return e
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
