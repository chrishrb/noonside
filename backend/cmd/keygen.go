package cmd

import (
	"fmt"
	"time"

	"github.com/chrishrb/noonside/internal/platform/auth"
	"github.com/chrishrb/noonside/internal/platform/push"
	"github.com/chrishrb/noonside/internal/platform/secretbox"
	"github.com/spf13/cobra"
)

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Generate a SECRET_KEY value (encrypts data source passwords)",
	RunE: func(cmd *cobra.Command, args []string) error {
		secret, err := secretbox.GenerateKey()
		if err != nil {
			return err
		}
		fmt.Printf("SECRET_KEY=%s\n", secret)
		return nil
	},
}

var keygenVAPIDCmd = &cobra.Command{
	Use:   "vapid",
	Short: "Generate a VAPID key pair for Web Push (vapid_public_key, VAPID_PRIVATE_KEY)",
	RunE: func(cmd *cobra.Command, args []string) error {
		pub, priv, err := push.GenerateKeys()
		if err != nil {
			return err
		}
		fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", pub, priv)
		return nil
	},
}

var keygenGoTrueCmd = &cobra.Command{
	Use:   "gotrue",
	Short: "Generate the signing key and API keys for a self-hosted Auth server (GoTrue)",
	Long: `Generate the secrets of a self-hosted Supabase Auth (GoTrue) server:

  GOTRUE_JWT_KEYS       ES256 private JWK that signs access tokens (GoTrue)
  GOTRUE_JWT_SECRET     HS256 secret GoTrue requires (GoTrue only)
  SUPABASE_SECRET_KEY   service_role token for the admin API (noonside API)
  SUPABASE_ANON_KEY     anon token, the SPA's publishable key (not secret)

Changing them signs everyone out; the API keys are valid for --years.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		years, _ := cmd.Flags().GetInt("years")
		k, err := auth.GenerateGoTrueKeys(time.Duration(years) * 365 * 24 * time.Hour)
		if err != nil {
			return err
		}
		fmt.Printf("GOTRUE_JWT_KEYS=%s\nGOTRUE_JWT_SECRET=%s\nSUPABASE_SECRET_KEY=%s\nSUPABASE_ANON_KEY=%s\n",
			k.JWTKeys, k.JWTSecret, k.ServiceRoleKey, k.AnonKey)
		return nil
	},
}

func init() {
	keygenGoTrueCmd.Flags().Int("years", 10, "validity of the API keys")
	keygenCmd.AddCommand(keygenVAPIDCmd)
	keygenCmd.AddCommand(keygenGoTrueCmd)
	rootCmd.AddCommand(keygenCmd)
}
