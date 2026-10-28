package solarlog

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/chrishrb/noonside/internal/modules/datasources/solarlog/storage"
)

// Limits restrict a hosted upload directory (an SFTPGo account the
// device uploads to): noonside owns the directory, so it deletes what it no
// longer needs and anything that is not a Solar-Log export.
type Limits struct {
	// Retention keeps imported dated minute files (minYYMMDD.js) this long.
	Retention time.Duration
	// ForeignGrace is how long files that are not Solar-Log exports are
	// kept before they are deleted.
	ForeignGrace time.Duration
}

// Usage is the result of a housekeeping run.
type Usage struct {
	// UsedBytes is the size of the files left in the directory.
	UsedBytes int64
	// Removed counts the files deleted in this run.
	Removed int
	// RemoveErr holds the deletions that failed (the files still count).
	RemoveErr error
}

// reExportFile matches the files of a Solar-Log export (base_vars.js,
// min_cur.js, min_day.js, minYYMMDD.js, days.js, days_hist.js, months.js,
// years.js, …); everything else is foreign.
var reExportFile = regexp.MustCompile(`^[a-z][a-z0-9_]*\.js$`)

// housekeep deletes from m: dated minute files that were imported and are
// older than the retention (or before the system's minute-data cutoff, so
// never imported), foreign files after their grace period, and files too
// large to be exports. It returns the remaining usage; the error is only set
// when the directory cannot be listed.
func housekeep(ctx context.Context, m storage.Manager, now time.Time, last, cutoff *time.Time, loc *time.Location, lim Limits) (Usage, error) {
	files, err := m.Files(ctx)
	if err != nil {
		return Usage{}, err
	}
	// Minute files of days before keepFrom are deleted: older than the
	// retention and imported (strictly before the last imported day, which
	// may still grow), or before the retention cutoff.
	keepFrom := ""
	if last != nil {
		keepFrom = min(now.Add(-lim.Retention).In(loc).Format("060102"), last.In(loc).Format("060102"))
	}
	if cutoff != nil {
		keepFrom = max(keepFrom, cutoff.In(loc).Format("060102"))
	}

	var u Usage
	var errs []error
	for _, f := range files {
		remove := false
		switch {
		case f.Size > storage.MaxFileSize:
			remove = true
		case !reExportFile.MatchString(f.Name):
			remove = now.Sub(f.ModTime) > lim.ForeignGrace
		default:
			if match := reMinuteFile.FindStringSubmatch(f.Name); match != nil && keepFrom != "" && match[1] < keepFrom {
				remove = true
			}
		}
		if !remove {
			u.UsedBytes += f.Size
			continue
		}
		if err := m.Remove(ctx, f.Name); err != nil {
			errs = append(errs, err)
			u.UsedBytes += f.Size
			continue
		}
		u.Removed++
	}
	u.RemoveErr = errors.Join(errs...)
	return u, nil
}
