package push

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllowedEndpoint(t *testing.T) {
	for _, ok := range []string{
		"https://fcm.googleapis.com/fcm/send/abc",
		"https://updates.push.services.mozilla.com/wpush/v2/abc",
		"https://web.push.apple.com/QGx",
		"https://wns2-db5p.notify.windows.com/w/?token=abc",
	} {
		assert.True(t, AllowedEndpoint(ok), ok)
	}
	for _, bad := range []string{
		"http://fcm.googleapis.com/fcm/send/abc",
		"https://fcm.googleapis.com.evil.example/x",
		"https://evilfcm.googleapis.com/x",
		"https://169.254.169.254/latest/meta-data",
		"https://user@fcm.googleapis.com/x",
		"https://fcm.googleapis.com:8443/x",
		"not a url",
	} {
		assert.False(t, AllowedEndpoint(bad), bad)
	}
}

func TestDisabledWithoutPrivateKey(t *testing.T) {
	c, err := New("", "", "")
	assert.NoError(t, err)
	assert.False(t, c.Enabled())
	_, err = New("", "priv", "a@example.com")
	assert.Error(t, err)
}

func TestGenerateKeys(t *testing.T) {
	pub, priv, err := GenerateKeys()
	assert.NoError(t, err)
	c, err := New(pub, priv, "mailto:a@example.com")
	assert.NoError(t, err)
	assert.True(t, c.Enabled())
	assert.Equal(t, pub, c.PublicKey())
}
