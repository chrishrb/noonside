// Package email renders and sends transactional emails.
//
// To add an email:
//  1. Define a struct holding the template data and implement Email on it.
//  2. Add templates/<name>.html (defines "content") and templates/<name>.txt.
//  3. Register a sample in init() so it can be previewed and is covered by
//     the render tests.
package email

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"
)

// Email is one kind of message. The struct itself is the template data,
// available in templates as .Data.
type Email interface {
	// Name selects templates/<name>.html and templates/<name>.txt.
	Name() string
	// Subject is the subject line (without the app name prefix).
	Subject() string
	// Preheader is the preview text shown by mail clients next to the subject.
	Preheader() string
}

// Sender delivers a composed message (SMTP in production, a fake in tests).
type Sender interface {
	Send(ctx context.Context, msg *gomail.Msg) error
}

// Mailer renders emails and hands them to a Sender.
type Mailer struct {
	sender   Sender
	renderer *Renderer
	from     string
	fromName string
	logger   *slog.Logger
}

type Config struct {
	SMTP SMTPConfig
	// From is the sender address; FromName its display name.
	From     string
	FromName string
	App      App
}

// New creates a Mailer. Without an SMTP host, emails are rendered and
// dropped with a log line, so the app works without mail delivery.
func New(cfg Config) (*Mailer, error) {
	r, err := NewRenderer(cfg.App)
	if err != nil {
		return nil, err
	}
	var sender Sender = discardSender{}
	if cfg.SMTP.Host != "" {
		if cfg.From == "" {
			return nil, fmt.Errorf("mail from address is required when SMTP is configured")
		}
		if sender, err = NewSMTPSender(cfg.SMTP); err != nil {
			return nil, err
		}
	}
	return NewWithSender(sender, r, cfg.From, cfg.FromName), nil
}

func NewWithSender(sender Sender, r *Renderer, from, fromName string) *Mailer {
	return &Mailer{
		sender: sender, renderer: r, from: from, fromName: fromName,
		logger: slog.Default().With("component", "email"),
	}
}

// Enabled reports whether emails are actually delivered.
func (m *Mailer) Enabled() bool {
	_, discard := m.sender.(discardSender)
	return !discard
}

// Send renders e and sends it to the given address.
func (m *Mailer) Send(ctx context.Context, to string, e Email) error {
	msg, err := m.Compose(to, e)
	if err != nil {
		return err
	}
	if err := m.sender.Send(ctx, msg); err != nil {
		return fmt.Errorf("send %s email: %w", e.Name(), err)
	}
	m.logger.InfoContext(ctx, "email sent", "email", e.Name(), "delivered", m.Enabled())
	return nil
}

// Compose builds the MIME message for e without sending it.
func (m *Mailer) Compose(to string, e Email) (*gomail.Msg, error) {
	r, err := m.renderer.Render(e)
	if err != nil {
		return nil, err
	}
	msg := gomail.NewMsg()
	from := m.from
	if from == "" {
		from = "noreply@localhost"
	}
	if m.fromName != "" {
		err = msg.FromFormat(m.fromName, from)
	} else {
		err = msg.From(from)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid from address: %w", err)
	}
	if err := msg.To(strings.TrimSpace(to)); err != nil {
		return nil, fmt.Errorf("invalid recipient: %w", err)
	}
	msg.Subject(r.Subject)
	msg.SetDate()
	msg.SetMessageID()
	msg.SetUserAgent("noonside")
	if h, ok := e.(interface{ Headers() map[string]string }); ok {
		for k, v := range h.Headers() {
			msg.SetGenHeader(gomail.Header(k), v)
		}
	}
	msg.SetBodyString(gomail.TypeTextPlain, r.Text)
	msg.AddAlternativeString(gomail.TypeTextHTML, r.HTML)
	return msg, nil
}

type discardSender struct{}

func (discardSender) Send(context.Context, *gomail.Msg) error { return nil }

// SMTPConfig configures delivery. TLS is one of "starttls" (default,
// mandatory STARTTLS), "tls" (implicit TLS, e.g. port 465),
// "opportunistic" or "none" (e.g. local Mailpit).
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	TLS      string
	Timeout  time.Duration
}

type smtpSender struct {
	client *gomail.Client
}

func NewSMTPSender(cfg SMTPConfig) (Sender, error) {
	opts := []gomail.Option{}
	if cfg.Timeout > 0 {
		opts = append(opts, gomail.WithTimeout(cfg.Timeout))
	} else {
		opts = append(opts, gomail.WithTimeout(15*time.Second))
	}
	switch strings.ToLower(cfg.TLS) {
	case "", "starttls":
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	case "tls", "ssl":
		opts = append(opts, gomail.WithSSLPort(false))
	case "opportunistic":
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSOpportunistic))
	case "none":
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	default:
		return nil, fmt.Errorf("invalid smtp tls mode %q", cfg.TLS)
	}
	// Set the port after the TLS mode, since WithSSLPort changes it.
	if cfg.Port != 0 {
		opts = append(opts, gomail.WithPort(cfg.Port))
	}
	if cfg.Username != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(cfg.Username),
			gomail.WithPassword(cfg.Password),
		)
	}
	c, err := gomail.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("smtp client: %w", err)
	}
	return &smtpSender{client: c}, nil
}

func (s *smtpSender) Send(ctx context.Context, msg *gomail.Msg) error {
	return s.client.DialAndSendWithContext(ctx, msg)
}
