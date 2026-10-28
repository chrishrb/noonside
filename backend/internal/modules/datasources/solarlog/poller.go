package solarlog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog/storage"
	"github.com/chrishrb/noonside/internal/modules/systems"
	"github.com/chrishrb/noonside/internal/modules/telemetry"
	"github.com/chrishrb/noonside/internal/platform/db"
	"github.com/google/uuid"
)

// Protocols of a solarlog_remote data source.
const (
	ProtocolFTP  = "ftp"
	ProtocolFTPS = "ftps"
	ProtocolSFTP = "sftp"
)

// Config is the JSON config of a solarlog_remote / solarlog_hosted data
// source.
type Config struct {
	// Protocol is ProtocolFTP, ProtocolFTPS or ProtocolSFTP (remote only).
	Protocol string `json:"protocol,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	// PasswordEnc is the FTP/SFTP password sealed with the server's secret key.
	PasswordEnc string `json:"password_enc,omitempty"`
	Directory   string `json:"directory,omitempty"`
	// HostKey pins the SFTP server's public key(s), see storage.SFTPConfig.
	HostKey         string `json:"host_key,omitempty"`
	BackfillHistory bool   `json:"backfill_history,omitempty"`
	// SubaccountID marks a solarlog_hosted source of the former Hetzner
	// Storage Box; such sources no longer work and must be re-created.
	SubaccountID int64 `json:"subaccount_id,omitempty"`
}

var reMinuteFile = regexp.MustCompile(`^min(\d{6})\.js$`)

// Store is what the poller reads and writes besides measurements: the
// system and its inverters (systems module) and the newest sample (telemetry
// module).
type Store interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
	GetSystem(ctx context.Context, id uuid.UUID) (*systems.System, error)
	UpdateSystem(ctx context.Context, s *systems.System) (*systems.System, error)
	GetTargets(ctx context.Context, systemID uuid.UUID) (map[int]float64, error)
	SetTargets(ctx context.Context, systemID uuid.UUID, targets map[int]float64) error
	GetInverterByRef(ctx context.Context, systemID uuid.UUID, ref string) (*systems.Inverter, error)
	CreateInverter(ctx context.Context, inv *systems.Inverter) (*systems.Inverter, error)
	UpdateInverter(ctx context.Context, inv *systems.Inverter) (*systems.Inverter, error)
	LastMeasurementTime(ctx context.Context, systemID uuid.UUID) (*time.Time, error)
}

// Ingester stores what the poller read (implemented by the telemetry module).
type Ingester interface {
	Measurements(ctx context.Context, systemID uuid.UUID, points []telemetry.Point, opts telemetry.Options) (telemetry.Result, error)
	Daily(ctx context.Context, systemID uuid.UUID, days []telemetry.Day, opts telemetry.Options) (telemetry.Result, error)
	State(ctx context.Context, systemID uuid.UUID, points []telemetry.Point, opts telemetry.Options) error
}

// Poller imports a Solar-Log export directory into one system.
type Poller struct {
	store    Store
	ingest   Ingester
	systemID uuid.UUID
	open     func(ctx context.Context) (storage.Storage, error)
	backfill bool
	logger   *slog.Logger

	// Housekeeping of hosted directories (see WithHousekeeping).
	limits *Limits
	around func(ctx context.Context, clean func(context.Context) (int64, error)) error
}

func NewPoller(st Store, ing Ingester, systemID uuid.UUID, backfill bool,
	open func(ctx context.Context) (storage.Storage, error)) *Poller {
	return &Poller{
		store: st, ingest: ing, systemID: systemID, open: open, backfill: backfill,
		logger: slog.Default().With("component", "adapter.solarlog", "system_id", systemID),
	}
}

// WithHousekeeping makes the poller clean up the directory after every poll
// (see Limits). around runs the clean-up, which returns the bytes left, and
// may fail the run (e.g. over quota). The storage must implement
// storage.Manager.
func (p *Poller) WithHousekeeping(lim Limits, around func(ctx context.Context, clean func(context.Context) (int64, error)) error) *Poller {
	p.limits, p.around = &lim, around
	return p
}

var createInverters = telemetry.Options{CreateInverters: true}

// stopMargin is kept free before the poll's deadline: no further minute
// files are started, so a long backfill ends as a normal run (recorded,
// lease released) instead of being cancelled or killed mid-transfer.
const stopMargin = 2 * time.Minute

func timeUp(ctx context.Context, now time.Time) bool {
	deadline, ok := ctx.Deadline()
	return ok && deadline.Sub(now) < stopMargin
}

func (p *Poller) Poll(ctx context.Context) error {
	st, err := p.open(ctx)
	if err != nil {
		return err
	}
	defer st.Close() //nolint:errcheck

	sys, err := p.store.GetSystem(ctx, p.systemID)
	if err != nil {
		return fmt.Errorf("load system: %w", err)
	}
	err = p.poll(ctx, st, sys)
	if p.limits != nil {
		// Also after a failed import: the quota must hold regardless.
		err = errors.Join(err, p.housekeep(ctx, st, sys))
	}
	return err
}

func (p *Poller) housekeep(ctx context.Context, st storage.Storage, sys *systems.System) error {
	m, ok := st.(storage.Manager)
	if !ok {
		return fmt.Errorf("housekeeping: storage cannot delete files")
	}
	last, err := p.store.LastMeasurementTime(ctx, p.systemID)
	if err != nil {
		return err
	}
	return p.around(ctx, func(ctx context.Context) (int64, error) {
		now := time.Now()
		u, err := housekeep(ctx, m, now, last, sys.MinuteCutoff(now), sys.Loc(), *p.limits)
		if err != nil {
			return 0, err
		}
		if u.Removed > 0 {
			p.logger.Info("deleted upload files", "files", u.Removed, "used_bytes", u.UsedBytes)
		}
		if u.RemoveErr != nil {
			p.logger.Warn("could not delete upload files", "error", u.RemoveErr)
		}
		return u.UsedBytes, nil
	})
}

func (p *Poller) poll(ctx context.Context, st storage.Storage, sys *systems.System) error {
	loc := sys.Loc()

	names, err := st.List(ctx)
	if err != nil {
		return err
	}
	has := func(n string) bool { return slices.Contains(names, n) }

	var base *BaseVars
	if has("base_vars.js") {
		data, err := st.Read(ctx, "base_vars.js")
		if err != nil {
			return err
		}
		if base, err = ParseBaseVars(data); err != nil {
			return fmt.Errorf("base_vars.js: %w", err)
		}
		if err := p.syncMetadata(ctx, sys, base); err != nil {
			return fmt.Errorf("sync metadata: %w", err)
		}
	}

	last, err := p.store.LastMeasurementTime(ctx, p.systemID)
	if err != nil {
		return err
	}
	files := minuteFiles(names, last, sys.MinuteCutoff(time.Now()), loc)
	for i, name := range files {
		if timeUp(ctx, time.Now()) {
			// Each file is committed on its own; the next run continues here.
			p.logger.Info("minute import paused, time budget used", "next_file", name, "remaining_files", len(files)-i)
			break
		}
		if err := p.importMinutes(ctx, st, name, loc); err != nil {
			return err
		}
	}

	dayFiles := []string{"days.js"}
	if p.backfill || last == nil {
		dayFiles = append(dayFiles, "days_hist.js")
	}
	for _, name := range dayFiles {
		if !has(name) {
			continue
		}
		if err := p.importDays(ctx, st, name); err != nil {
			return err
		}
	}

	if has("min_cur.js") {
		if err := p.importCurrent(ctx, st, loc, base); err != nil {
			return err
		}
	}
	return nil
}

// minuteFiles returns the minute files to import, oldest first: dated files
// from the day of the last import (and the retention cutoff) on, plus
// min_day.js (today).
func minuteFiles(names []string, last, cutoff *time.Time, loc *time.Location) []string {
	since := ""
	if last != nil {
		since = last.In(loc).Format("060102")
	}
	// Files before the retention cutoff would only be deleted again.
	if cutoff != nil {
		since = max(since, cutoff.In(loc).Format("060102"))
	}
	var out []string
	for _, n := range names {
		if m := reMinuteFile.FindStringSubmatch(n); m != nil && m[1] >= since {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	if slices.Contains(names, "min_day.js") {
		out = append(out, "min_day.js")
	}
	return out
}

func (p *Poller) importMinutes(ctx context.Context, st storage.Storage, name string, loc *time.Location) error {
	data, err := st.Read(ctx, name)
	if err != nil {
		return err
	}
	recs, err := ParseLines(data, func(l string) (*MinuteRecord, error) { return ParseMinuteLine(l, loc) })
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	points := make([]telemetry.Point, 0, len(recs)*12)
	for _, rec := range recs {
		midnight := rec.Time.Hour() == 0 && rec.Time.Minute() == 0
		for _, r := range rec.Readings {
			pt := telemetry.Point{
				InverterRef:  strconv.Itoa(r.Inverter),
				Time:         rec.Time,
				PacW:         ptr(r.Pac),
				PdcW:         ptr(r.Pdc),
				UdcV:         ptr(r.Udc),
				TemperatureC: r.Temperature,
			}
			// The 00:00 record still carries the previous day's counter.
			if !midnight {
				pt.EdayWh = ptr(r.Eday)
			}
			points = append(points, pt)
		}
	}
	if len(points) == 0 {
		return nil
	}
	res, err := p.ingest.Measurements(ctx, p.systemID, points, createInverters)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	p.logger.Debug("imported minute file", "file", name, "points", res.Accepted, "rejected", res.Rejected)
	return nil
}

func (p *Poller) importDays(ctx context.Context, st storage.Storage, name string) error {
	data, err := st.Read(ctx, name)
	if err != nil {
		return err
	}
	recs, err := ParseLines(data, ParseDayLine)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var days []telemetry.Day
	for _, rec := range recs {
		for i, e := range rec.EnergyWh {
			d := telemetry.Day{InverterRef: strconv.Itoa(i + 1), Date: rec.Date, EnergyWh: e}
			if pm := rec.PacMaxW[i]; pm > 0 {
				d.PacMaxW = ptr(pm)
			}
			days = append(days, d)
		}
	}
	if len(days) == 0 {
		return nil
	}
	if _, err := p.ingest.Daily(ctx, p.systemID, days, createInverters); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func (p *Poller) importCurrent(ctx context.Context, st storage.Storage, loc *time.Location, base *BaseVars) error {
	data, err := st.Read(ctx, "min_cur.js")
	if err != nil {
		return err
	}
	cur, err := ParseCurrent(data, loc)
	if err != nil {
		return fmt.Errorf("min_cur.js: %w", err)
	}
	var points []telemetry.Point
	for i := range cur.StatusCodes {
		pt := telemetry.Point{InverterRef: strconv.Itoa(i + 1), Time: cur.Time}
		if i < len(cur.Pac) {
			pt.PacW = ptr(cur.Pac[i])
		}
		if i < len(cur.Pdc) {
			pt.PdcW = ptr(cur.Pdc[i])
		}
		sc := cur.StatusCodes[i]
		status := &telemetry.Status{Code: &sc}
		if i < len(cur.ErrorCodes) {
			ec := cur.ErrorCodes[i]
			status.ErrorCode = &ec
		}
		if base != nil && i < len(base.Inverters) {
			status.Text = Label(base.Inverters[i].StatusCodes, sc)
			if status.ErrorCode != nil {
				status.ErrorText = Label(base.Inverters[i].ErrorCodes, *status.ErrorCode)
			}
		}
		pt.Status = status
		points = append(points, pt)
	}
	return p.ingest.State(ctx, p.systemID, points, createInverters)
}

// syncMetadata creates/updates inverters from base_vars.js and fills system
// fields and targets that have not been set yet.
func (p *Poller) syncMetadata(ctx context.Context, sys *systems.System, bv *BaseVars) error {
	return p.store.InTx(ctx, func(ctx context.Context) error {
		for _, bi := range bv.Inverters {
			ref := strconv.Itoa(bi.Index)
			inv, err := p.store.GetInverterByRef(ctx, sys.ID, ref)
			switch {
			case errors.Is(err, db.ErrNotFound):
				_, err = p.store.CreateInverter(ctx, &systems.Inverter{
					SystemID: sys.ID, ExternalRef: ref, Name: bi.Name, Model: bi.Model,
					SerialNumber: bi.Serial, PeakPowerKWp: bi.PeakPowerW / 1000,
					Color: defaultColor(bi.Index), SortOrder: bi.Index,
				})
			case err == nil:
				changed := false
				if inv.Model == "" && bi.Model != "" {
					inv.Model, changed = bi.Model, true
				}
				if inv.SerialNumber == "" && bi.Serial != "" {
					inv.SerialNumber, changed = bi.Serial, true
				}
				if inv.PeakPowerKWp == 0 && bi.PeakPowerW > 0 {
					inv.PeakPowerKWp, changed = bi.PeakPowerW/1000, true
				}
				if inv.Color == "" {
					inv.Color, changed = defaultColor(bi.Index), true
				}
				if changed {
					_, err = p.store.UpdateInverter(ctx, inv)
				}
			}
			if err != nil {
				return err
			}
		}

		changed := false
		set := func(dst *string, v string) {
			if *dst == "" && v != "" {
				*dst, changed = v, true
			}
		}
		set(&sys.Location, bv.Location)
		set(&sys.ModuleInfo, bv.Modules)
		set(&sys.Operator, bv.Operator)
		set(&sys.Orientation, bv.Orientation)
		if sys.PeakPowerKWp == 0 && bv.PeakPowerW > 0 {
			sys.PeakPowerKWp, changed = bv.PeakPowerW/1000, true
		}
		if sys.TariffPerKWh == 0 && bv.TariffCtPer > 0 {
			// Verguetung is in 1/100 ct: 3914 -> 0.3914 EUR/kWh
			sys.TariffPerKWh, changed = bv.TariffCtPer/10000, true
		}
		if changed {
			if _, err := p.store.UpdateSystem(ctx, sys); err != nil {
				return err
			}
		}

		targets, err := p.store.GetTargets(ctx, sys.ID)
		if err != nil {
			return err
		}
		if len(targets) == 0 && len(bv.TargetMonthly) == 12 && bv.TargetYearKWp > 0 && bv.PeakPowerW > 0 {
			yearly := bv.PeakPowerW / 1000 * bv.TargetYearKWp
			t := make(map[int]float64, 12)
			for i, pct := range bv.TargetMonthly {
				t[i+1] = math.Round(yearly*pct) / 100
			}
			if err := p.store.SetTargets(ctx, sys.ID, t); err != nil {
				return err
			}
		}
		return nil
	})
}

// Solar-Log's inverter colors (WR 1..11).
var palette = []string{"#ff0000", "#008000", "#a52a2a", "#32cd32", "#808000", "#ffc0cb",
	"#ff00ff", "#add8e6", "#00ffff", "#d3d3d3", "#808080"}

func defaultColor(index int) string {
	return palette[(index-1+len(palette))%len(palette)]
}

func ptr[T any](v T) *T { return &v }
