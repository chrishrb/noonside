package netguard

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPublic(t *testing.T) {
	for _, a := range []string{
		"127.0.0.1:21", "10.0.0.1:21", "172.16.5.4:22", "192.168.178.10:21",
		"169.254.169.254:80", "100.64.0.1:21", "0.0.0.0:21", "224.0.0.1:21",
		"[::1]:22", "[fd00::1]:22", "[fe80::1]:22", "[::ffff:127.0.0.1]:21", "[::]:21",
	} {
		assert.Error(t, CheckPublic(a), a)
	}
	for _, a := range []string{"93.184.215.14:21", "[2a00:1450:4001:80b::200e]:22"} {
		assert.NoError(t, CheckPublic(a), a)
	}
}

func TestDialerRefusesLocal(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close() //nolint:errcheck

	_, err = Dialer(time.Second, false).DialContext(context.Background(), "tcp", l.Addr().String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")

	c, err := Dialer(time.Second, true).DialContext(context.Background(), "tcp", l.Addr().String())
	require.NoError(t, err)
	_ = c.Close()
}

func TestHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()

	_, err := HTTPClient(time.Second, false).Get(srv.URL) //nolint:noctx
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")

	res, err := HTTPClient(time.Second, true).Get(srv.URL) //nolint:noctx
	require.NoError(t, err)
	_ = res.Body.Close()
	assert.Equal(t, 204, res.StatusCode)
}

func TestCheckURL(t *testing.T) {
	assert.NoError(t, CheckURL("https://ntfy.sh/topic", false))
	assert.Error(t, CheckURL("http://ntfy.sh/topic", false))
	assert.NoError(t, CheckURL("http://192.168.1.5/hook", true))
	assert.Error(t, CheckURL("ftp://x", true))
	assert.Error(t, CheckURL("https:///path", false))
	assert.Error(t, CheckURL("https://user:pw@example.com/", false))
	assert.Error(t, CheckURL("not a url", false))
}
