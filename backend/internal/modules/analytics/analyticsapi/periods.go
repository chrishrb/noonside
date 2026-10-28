package analyticsapi

import (
	"fmt"
	"time"
)

// period is a calendar range in a system's time zone.
type period struct {
	kind  string // day, month, year, total
	start time.Time
	end   time.Time // exclusive
}

func dateOf(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// newPeriod returns the period of kind containing date. For "total", first
// is the first day with data (or the commissioning date).
func newPeriod(kind string, date time.Time, first time.Time, loc *time.Location) (period, error) {
	d := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	switch kind {
	case "day":
		return period{kind, d, d.AddDate(0, 0, 1)}, nil
	case "month":
		s := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, loc)
		return period{kind, s, s.AddDate(0, 1, 0)}, nil
	case "year":
		s := time.Date(d.Year(), 1, 1, 0, 0, 0, 0, loc)
		return period{kind, s, s.AddDate(1, 0, 0)}, nil
	case "total":
		f := first.In(loc)
		s := time.Date(f.Year(), 1, 1, 0, 0, 0, 0, loc)
		e := time.Date(d.Year()+1, 1, 1, 0, 0, 0, 0, loc)
		if !s.Before(e) {
			s = e.AddDate(-1, 0, 0)
		}
		return period{kind, s, e}, nil
	}
	return period{}, fmt.Errorf("invalid period %q", kind)
}

// granularity is the bucket size of the chart for this period.
func (p period) granularity() string {
	switch p.kind {
	case "month":
		return "day"
	case "year":
		return "month"
	case "total":
		return "year"
	}
	return ""
}

// buckets returns the start of every chart bucket (month/year/total).
func (p period) buckets() []time.Time {
	var out []time.Time
	for t := p.start; t.Before(p.end); {
		out = append(out, t)
		switch p.granularity() {
		case "day":
			t = t.AddDate(0, 0, 1)
		case "month":
			t = t.AddDate(0, 1, 0)
		case "year":
			t = t.AddDate(1, 0, 0)
		default:
			return out
		}
	}
	return out
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// targetKWh returns the expected energy in [from, to) given monthly targets,
// distributing each month's target evenly over its days.
func targetKWh(targets map[int]float64, from, to time.Time) float64 {
	if len(targets) == 0 || !from.Before(to) {
		return 0
	}
	total := 0.0
	for d := from; d.Before(to); {
		monthEnd := time.Date(d.Year(), d.Month()+1, 1, 0, 0, 0, 0, d.Location())
		end := monthEnd
		if to.Before(end) {
			end = to
		}
		days := calendarDays(d, end)
		total += targets[int(d.Month())] * days / float64(daysIn(d.Year(), d.Month()))
		d = end
	}
	return total
}

// calendarDays counts local calendar days in [from, to), ignoring DST shifts.
func calendarDays(from, to time.Time) float64 {
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return t.Sub(f).Hours() / 24
}
