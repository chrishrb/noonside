package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrishrb/noonside/internal/app"
	"github.com/chrishrb/noonside/internal/platform/config"
	"github.com/chrishrb/noonside/internal/platform/email"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	emailPreviewText bool
	emailTo          string
)

var emailCmd = &cobra.Command{
	Use:   "email",
	Short: "Preview and test emails",
}

var emailListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all emails",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(strings.Join(email.Registered(), "\n"))
	},
}

var emailPreviewCmd = &cobra.Command{
	Use:   "preview NAME",
	Short: "Render an email with sample data to stdout (HTML, or --text)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e, ok := email.Sample(args[0])
		if !ok {
			return fmt.Errorf("unknown email %q (see `noonside email list`)", args[0])
		}
		r, err := email.NewRenderer(email.App{Name: "noonside", URL: "http://localhost:5173"})
		if err != nil {
			return err
		}
		out, err := r.Render(e)
		if err != nil {
			return err
		}
		if emailPreviewText {
			fmt.Printf("Subject: %s\n\n%s", out.Subject, out.Text)
		} else {
			fmt.Print(out.HTML)
		}
		return nil
	},
}

var emailSendTestCmd = &cobra.Command{
	Use:   "send-test NAME",
	Short: "Send an email with sample data via the configured SMTP server (e.g. Mailpit from `supabase start`)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ReadConfig()
		e, ok := email.Sample(args[0])
		if !ok {
			return fmt.Errorf("unknown email %q (see `noonside email list`)", args[0])
		}
		m, err := app.NewMailer()
		if err != nil {
			return err
		}
		if !m.Enabled() {
			return fmt.Errorf("SMTP_HOST is not configured")
		}
		if err := m.Send(context.Background(), emailTo, e); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "sent %q via %s\n", args[0], viper.GetString("SMTP_HOST"))
		return nil
	},
}

var emailTemplatesDir string

var emailSupabaseTemplatesCmd = &cobra.Command{
	Use:   "supabase-templates",
	Short: "Render the Supabase Auth email templates in the noonside layout",
	Long: `Writes confirmation/recovery/magic_link/email_change templates for the Auth
server (GoTrue) into backend/db/auth-templates. The API serves them under /auth-templates/
(auth_templates_dir), where GoTrue fetches them (GOTRUE_MAILER_TEMPLATES_*).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ReadConfig()
		r, err := email.NewRenderer(email.App{Name: "noonside", URL: viper.GetString("PUBLIC_URL")})
		if err != nil {
			return err
		}
		dir := emailTemplatesDir
		if dir == "" {
			// backend/db/auth-templates, wherever the command runs from.
			dir = filepath.Join(config.Dir, "..", "db", "auth-templates")
		}
		written, err := r.ExportSupabaseTemplates(dir)
		if err != nil {
			return err
		}
		for _, p := range written {
			fmt.Fprintln(os.Stderr, "wrote", p)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(emailCmd)
	emailCmd.AddCommand(emailListCmd, emailPreviewCmd, emailSendTestCmd, emailSupabaseTemplatesCmd)
	emailSupabaseTemplatesCmd.Flags().StringVar(&emailTemplatesDir, "dir", "", "output directory (default backend/db/auth-templates)")
	emailPreviewCmd.Flags().BoolVar(&emailPreviewText, "text", false, "render the plain-text part")
	emailSendTestCmd.Flags().StringVar(&emailTo, "to", "test@noonside.local", "recipient")
}
