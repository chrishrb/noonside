package cmd

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/chrishrb/noonside/internal/app"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/config"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	importSystem string
	importBatch  int
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import historical data",
}

var importSolarLogCmd = &cobra.Command{
	Use:   "solarlog DIR",
	Short: "Import a Solar-Log export directory (metadata, minute data and full daily history)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
		config.ReadConfig()
		systemID, err := uuid.Parse(importSystem)
		if err != nil {
			return fmt.Errorf("--system must be a system id: %w", err)
		}
		ctx := context.Background()
		d, err := app.OpenDB(ctx)
		if err != nil {
			return err
		}
		defer d.Close()

		m := app.NewModules(d, app.Deps{})
		if err := m.DataSources.ImportSolarLogDir(ctx, systemID, args[0]); err != nil {
			return err
		}
		fmt.Println("import completed")
		return nil
	},
}

var importMeasurementsCmd = &cobra.Command{
	Use:   "measurements FILE.csv",
	Short: "Load minute data from a CSV export of the former measurements table",
	Long: `Loads a CSV with a header row and the columns time, inverter_id, pac_w, pdc_w,
udc_v, eday_wh and temperature_c, as written by

  \copy (SELECT * FROM measurements ORDER BY inverter_id, time) TO 'measurements.csv' CSV HEADER

Existing samples with the same inverter and time are replaced. Daily energy
is not touched.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if importBatch < 1 {
			return errors.New("--batch must be positive")
		}
		config.ReadConfig()
		ctx := context.Background()
		d, err := app.OpenDB(ctx)
		if err != nil {
			return err
		}
		defer d.Close()
		st := app.NewModules(d, app.Deps{}).Telemetry

		f, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		r := csv.NewReader(f)
		header, err := r.Read()
		if err != nil {
			return fmt.Errorf("read header: %w", err)
		}
		col := map[string]int{}
		for i, h := range header {
			col[h] = i
		}
		for _, h := range []string{"time", "inverter_id", "pac_w", "pdc_w", "udc_v", "eday_wh", "temperature_c"} {
			if _, ok := col[h]; !ok {
				return fmt.Errorf("missing column %q", h)
			}
		}

		batch := make([]telemetry.Measurement, 0, importBatch)
		total := 0
		flush := func() error {
			if err := st.UpsertMeasurements(ctx, batch); err != nil {
				return err
			}
			total += len(batch)
			batch = batch[:0]
			fmt.Fprintf(os.Stderr, "\r%d rows", total)
			return nil
		}
		for line := 2; ; line++ {
			rec, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			m, err := parseMeasurementRecord(rec, col)
			if err != nil {
				return fmt.Errorf("line %d: %w", line, err)
			}
			batch = append(batch, m)
			if len(batch) == importBatch {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		if err := flush(); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr)
		fmt.Println("import completed")
		return nil
	},
}

func parseMeasurementRecord(rec []string, col map[string]int) (telemetry.Measurement, error) {
	var m telemetry.Measurement
	var err error
	if m.Time, err = parsePGTime(rec[col["time"]]); err != nil {
		return m, err
	}
	if m.InverterID, err = uuid.Parse(rec[col["inverter_id"]]); err != nil {
		return m, err
	}
	for name, dst := range map[string]**float64{
		"pac_w": &m.PacW, "pdc_w": &m.PdcW, "udc_v": &m.UdcV, "eday_wh": &m.EdayWh, "temperature_c": &m.TemperatureC,
	} {
		v := rec[col[name]]
		if v == "" {
			continue
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return m, fmt.Errorf("%s: %w", name, err)
		}
		*dst = &f
	}
	return m, nil
}

// parsePGTime parses timestamptz text output, e.g. "2026-09-01 10:00:00+00".
func parsePGTime(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05.999999-07", "2006-01-02 15:04:05.999999-07:00", time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", s)
}

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.AddCommand(importSolarLogCmd, importMeasurementsCmd)
	importMeasurementsCmd.Flags().IntVar(&importBatch, "batch", 50000, "rows per transaction")
	importSolarLogCmd.Flags().StringVar(&importSystem, "system", "", "target system id")
	_ = importSolarLogCmd.MarkFlagRequired("system")
}
