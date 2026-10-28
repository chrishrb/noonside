package cmd

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/chrishrb/noonside/internal/app"
	"github.com/chrishrb/noonside/internal/modules/waitlist"
	"github.com/chrishrb/noonside/internal/platform/config"
	"github.com/spf13/cobra"
)

var waitlistAll bool

var waitlistCmd = &cobra.Command{
	Use:   "waitlist",
	Short: "Numbers and export of the landing page's waitlist",
}

var waitlistStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Sign-ups by status, role and plan, and pricing clicks per plan and ISO week",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ReadConfig()
		ctx := context.Background()
		d, err := app.OpenDB(ctx)
		if err != nil {
			return err
		}
		defer d.Close()
		st := waitlist.New(d)
		entries, err := st.ListWaitlistEntries(ctx)
		if err != nil {
			return err
		}
		clicks, err := st.ListPricingClicks(ctx)
		if err != nil {
			return err
		}

		var confirmed, pending int
		byRole, byPlan := map[string]int{}, map[string]int{}
		for _, e := range entries {
			if e.ConfirmedAt == nil {
				pending++
				continue
			}
			confirmed++
			byRole[orDash(e.Role)]++
			byPlan[orDash(e.Plan)]++
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintf(w, "confirmed\t%d\npending\t%d\n\n", confirmed, pending)
		printCounts(w, "role (confirmed)", byRole)
		printCounts(w, "plan (confirmed)", byPlan)

		weeks := map[string]map[string]int{}
		for _, c := range clicks {
			y, wk := c.Day.ISOWeek()
			key := fmt.Sprintf("%d-W%02d", y, wk)
			if weeks[key] == nil {
				weeks[key] = map[string]int{}
			}
			weeks[key][c.Plan] += c.Count
		}
		_, _ = fmt.Fprintln(w, "pricing clicks\tfree\tpro\tinstaller")
		for _, k := range sortedKeys(weeks) {
			_, _ = fmt.Fprintf(w, "%s\t%d\t%d\t%d\n", k, weeks[k]["free"], weeks[k]["pro"], weeks[k]["installer"])
		}
		return w.Flush()
	},
}

var waitlistExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Confirmed sign-ups as CSV on stdout (--all includes unconfirmed ones)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		config.ReadConfig()
		ctx := context.Background()
		d, err := app.OpenDB(ctx)
		if err != nil {
			return err
		}
		defer d.Close()
		st := waitlist.New(d)
		entries, err := st.ListWaitlistEntries(ctx)
		if err != nil {
			return err
		}
		w := csv.NewWriter(os.Stdout)
		_ = w.Write([]string{"email", "role", "plan", "created_at", "confirmed_at", "unsubscribe_token"})
		for _, e := range entries {
			if e.ConfirmedAt == nil && !waitlistAll {
				continue
			}
			confirmedAt := ""
			if e.ConfirmedAt != nil {
				confirmedAt = e.ConfirmedAt.UTC().Format(time.RFC3339)
			}
			_ = w.Write([]string{e.Email, e.Role, e.Plan, e.CreatedAt.UTC().Format(time.RFC3339), confirmedAt, e.UnsubscribeToken})
		}
		w.Flush()
		return w.Error()
	},
}

func printCounts(w *tabwriter.Writer, title string, m map[string]int) {
	_, _ = fmt.Fprintln(w, title)
	for _, k := range sortedKeys(m) {
		_, _ = fmt.Fprintf(w, "  %s\t%d\n", k, m[k])
	}
	_, _ = fmt.Fprintln(w)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func init() {
	rootCmd.AddCommand(waitlistCmd)
	waitlistCmd.AddCommand(waitlistStatsCmd, waitlistExportCmd)
	waitlistExportCmd.Flags().BoolVar(&waitlistAll, "all", false, "include unconfirmed sign-ups")
}
