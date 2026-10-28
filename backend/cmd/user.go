package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/chrishrb/noonside/internal/app"
	"github.com/chrishrb/noonside/internal/modules/identity"
	"github.com/chrishrb/noonside/internal/platform/config"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/chrishrb/noonside/internal/platform/gotrue"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	userName       string
	userSuperadmin bool
	userRevoke     bool
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage GoTrue users",
}

var userCreateCmd = &cobra.Command{
	Use:   "create EMAIL",
	Short: "Create a confirmed user (the password is read from stdin or a prompt)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ReadConfig()
		admin, err := app.NewAuthAdmin()
		if err != nil {
			return err
		}
		password, err := readPassword()
		if err != nil {
			return err
		}
		// Creating an account only calls the Auth admin API.
		id, err := identity.New(nil, admin).CreateUser(context.Background(), identity.NewUser{
			Email: args[0], Password: password, Name: userName, Superadmin: userSuperadmin,
		})
		if errors.Is(err, gotrue.ErrUserExists) {
			return fmt.Errorf("a user with this email already exists")
		}
		if err != nil {
			return err
		}
		fmt.Printf("created user %s (superadmin=%t)\n", id, userSuperadmin)
		return nil
	},
}

var userPromoteCmd = &cobra.Command{
	Use:   "promote EMAIL",
	Short: "Grant (or with --revoke remove) superadmin",
	Long:  "Superadmins can create systems and see all systems. The change applies to the user's next access token: a revoked superadmin keeps the flag until the current one expires (at most 1 hour).",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ReadConfig()
		admin, err := app.NewAuthAdmin()
		if err != nil {
			return err
		}
		ctx := context.Background()
		d, err := app.OpenDB(ctx)
		if err != nil {
			return err
		}
		defer d.Close()
		users := identity.New(d, admin)
		prof, err := users.GetProfileByEmail(ctx, identity.NormalizeEmail(args[0]))
		if errors.Is(err, db.ErrNotFound) {
			return fmt.Errorf("no user with this email")
		}
		if err != nil {
			return err
		}
		if err := users.SetSuperadmin(ctx, prof.ID, !userRevoke); err != nil {
			return err
		}
		fmt.Printf("user %s superadmin=%t\n", prof.ID, !userRevoke)
		return nil
	},
}

func readPassword() (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func init() {
	rootCmd.AddCommand(userCmd)
	userCmd.AddCommand(userCreateCmd, userPromoteCmd)
	userCreateCmd.Flags().StringVar(&userName, "name", "", "display name")
	userCreateCmd.Flags().BoolVar(&userSuperadmin, "superadmin", false, "grant superadmin")
	userPromoteCmd.Flags().BoolVar(&userRevoke, "revoke", false, "remove superadmin instead")
}
