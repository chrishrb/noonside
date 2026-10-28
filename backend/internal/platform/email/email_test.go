package email

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomail "github.com/wneessen/go-mail"
)

var testApp = App{Name: "noonside", URL: "https://solar.example.com"}

func TestEveryTemplateHasASample(t *testing.T) {
	r, err := NewRenderer(testApp)
	require.NoError(t, err)
	assert.Equal(t, r.Names(), Registered(), "each templates/<name>.html needs a Register() call and vice versa")
}

func TestRenderAllSamples(t *testing.T) {
	r, err := NewRenderer(testApp)
	require.NoError(t, err)
	for _, name := range Registered() {
		t.Run(name, func(t *testing.T) {
			e, _ := Sample(name)
			out, err := r.Render(e)
			require.NoError(t, err)
			assert.NotEmpty(t, out.Subject)
			assert.Contains(t, out.HTML, "<!DOCTYPE html>")
			assert.Contains(t, out.HTML, e.Preheader())
			assert.NotContains(t, out.HTML, "<no value>")
			assert.NotContains(t, out.Text, "<no value>")
			assert.NotContains(t, out.Text, "<", "text part must not contain markup")
		})
	}
}

func TestInvitation(t *testing.T) {
	r, err := NewRenderer(testApp)
	require.NoError(t, err)
	e := Invitation{
		SystemName:  `Anlage <script>alert(1)</script>`,
		InviterName: "Alex",
		Role:        "admin",
		AcceptURL:   "https://solar.example.com/invitations/s0i_abc",
		ExpiresAt:   time.Date(2026, 10, 2, 12, 30, 0, 0, time.UTC),
	}
	out, err := r.Render(e)
	require.NoError(t, err)

	assert.Equal(t, "You're invited to Anlage <script>alert(1)</script>", out.Subject)
	assert.NotContains(t, out.HTML, "<script>")
	assert.Contains(t, out.HTML, "&lt;script&gt;")
	assert.Contains(t, out.HTML, `href="https://solar.example.com/invitations/s0i_abc"`)
	assert.Contains(t, out.HTML, "As an <strong")
	assert.Contains(t, out.HTML, "02.10.2026 12:30")
	assert.Contains(t, out.HTML, "someone invited this address", "email overrides the footer")

	assert.Contains(t, out.Text, "https://solar.example.com/invitations/s0i_abc")
	assert.Contains(t, out.Text, "Role:         Admin")
}

type captureSender struct{ msgs []*gomail.Msg }

func (c *captureSender) Send(_ context.Context, m *gomail.Msg) error {
	c.msgs = append(c.msgs, m)
	return nil
}

func TestMailerComposesMultipart(t *testing.T) {
	r, err := NewRenderer(testApp)
	require.NoError(t, err)
	cs := &captureSender{}
	m := NewWithSender(cs, r, "noreply@solar.example.com", "noonside")

	e, _ := Sample("invitation")
	require.NoError(t, m.Send(context.Background(), "someone@example.com", e))
	require.Len(t, cs.msgs, 1)

	var buf bytes.Buffer
	_, err = cs.msgs[0].WriteTo(&buf)
	require.NoError(t, err)
	raw := buf.String()
	assert.Contains(t, raw, `From: "noonside" <noreply@solar.example.com>`)
	assert.Contains(t, raw, "To: <someone@example.com>")
	assert.Contains(t, raw, "multipart/alternative")
	assert.Contains(t, raw, "text/plain")
	assert.Contains(t, raw, "text/html")

	assert.Error(t, m.Send(context.Background(), "not an address", e))
}

func TestNewWithoutSMTPDiscards(t *testing.T) {
	m, err := New(Config{App: testApp})
	require.NoError(t, err)
	assert.False(t, m.Enabled())
	e, _ := Sample("invitation")
	assert.NoError(t, m.Send(context.Background(), "someone@example.com", e))

	_, err = New(Config{App: testApp, SMTP: SMTPConfig{Host: "localhost"}})
	assert.ErrorContains(t, err, "from address")
	_, err = New(Config{App: testApp, From: "a@b.c", SMTP: SMTPConfig{Host: "localhost", TLS: "bogus"}})
	assert.Error(t, err)
}

func TestFuncs(t *testing.T) {
	_, err := funcs["map"].(func(...any) (map[string]any, error))("odd")
	assert.Error(t, err)
	assert.True(t, strings.HasPrefix(funcs["title"].(func(string) string)("viewer"), "V"))
}

func TestAuthTemplates(t *testing.T) {
	r, err := NewRenderer(testApp)
	require.NoError(t, err)
	for _, tpl := range AuthTemplates {
		html, err := r.RenderAuthTemplate(tpl)
		require.NoError(t, err)
		assert.Contains(t, html, `href="{{ .ConfirmationURL }}"`, tpl.Kind)
		assert.NotContains(t, html, "gotrue-confirmation-url", tpl.Kind)
		assert.Contains(t, html, `href="{{ .SiteURL }}"`, tpl.Kind)
		assert.NotContains(t, html, "gotrue-site-url", tpl.Kind)
		assert.Contains(t, html, tpl.Email.Title, tpl.Kind)
	}
}
