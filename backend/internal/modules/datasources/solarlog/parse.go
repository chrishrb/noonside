// Package solarlog imports the HTML/JS export of a Solar-Log data logger.
package solarlog

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Reading is one inverter's values from a minute record.
type Reading struct {
	Inverter    int // 1-based index as in the Solar-Log ("WR 1")
	Pac         float64
	Pdc         float64
	Eday        float64 // Wh since midnight
	Udc         float64
	Temperature *float64
}

type MinuteRecord struct {
	Time     time.Time
	Readings []Reading
}

type DayRecord struct {
	Date     time.Time
	EnergyWh []float64 // per inverter, index 0 = WR 1
	PacMaxW  []float64
}

// Inverter is one entry of WRInfo in base_vars.js.
type Inverter struct {
	Index       int
	Model       string
	Serial      string
	PeakPowerW  float64
	Name        string
	StatusCodes []string
	ErrorCodes  []string
}

// BaseVars holds the parts of base_vars.js that describe the plant.
type BaseVars struct {
	PeakPowerW    float64
	TargetMonthly []float64 // percent of the yearly yield per month
	TargetYearKWp float64   // expected kWh per kWp per year
	TariffCtPer   float64   // feed-in tariff in 1/100 ct per kWh (Verguetung)
	Title         string
	Operator      string
	Location      string
	Modules       string
	Commissioned  string
	Orientation   string
	Inverters     []Inverter
}

// Current is min_cur.js: the latest snapshot.
type Current struct {
	Time        time.Time
	Pac         []float64
	Pdc         []float64
	StatusCodes []int
	ErrorCodes  []int
}

// recordValue extracts the quoted payload of lines like m[mi++]="..."
func recordValue(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	if i := strings.IndexByte(line, '"'); i >= 0 {
		j := strings.LastIndexByte(line, '"')
		if j <= i {
			return "", false
		}
		return line[i+1 : j], true
	}
	return line, true
}

// ParseMinuteLine parses one line of min*.js:
// "dd.mm.yy HH:MM:SS|pac;pdc;eday;udc[;temp]|..."
func ParseMinuteLine(line string, loc *time.Location) (*MinuteRecord, error) {
	v, ok := recordValue(line)
	if !ok {
		return nil, nil
	}
	parts := strings.Split(v, "|")
	ts, err := time.ParseInLocation("02.01.06 15:04:05", parts[0], loc)
	if err != nil {
		return nil, fmt.Errorf("parse time %q: %w", parts[0], err)
	}
	rec := &MinuteRecord{Time: ts, Readings: make([]Reading, 0, len(parts)-1)}
	for i, inv := range parts[1:] {
		f := strings.Split(inv, ";")
		if len(f) < 4 {
			return nil, fmt.Errorf("inverter %d: expected at least 4 values, got %d", i+1, len(f))
		}
		vals, err := floats(f)
		if err != nil {
			return nil, fmt.Errorf("inverter %d: %w", i+1, err)
		}
		r := Reading{Inverter: i + 1, Pac: vals[0], Pdc: vals[1], Eday: vals[2], Udc: vals[3]}
		if len(vals) > 4 {
			t := vals[4]
			r.Temperature = &t
		}
		rec.Readings = append(rec.Readings, r)
	}
	return rec, nil
}

// ParseDayLine parses one line of days.js / days_hist.js:
// "dd.mm.yy|energy;pacmax|..."
func ParseDayLine(line string) (*DayRecord, error) {
	v, ok := recordValue(line)
	if !ok {
		return nil, nil
	}
	parts := strings.Split(v, "|")
	d, err := time.Parse("02.01.06", parts[0])
	if err != nil {
		return nil, fmt.Errorf("parse date %q: %w", parts[0], err)
	}
	rec := &DayRecord{Date: d}
	for i, inv := range parts[1:] {
		vals, err := floats(strings.Split(inv, ";"))
		if err != nil || len(vals) == 0 {
			return nil, fmt.Errorf("inverter %d: invalid values %q", i+1, inv)
		}
		rec.EnergyWh = append(rec.EnergyWh, vals[0])
		pm := 0.0
		if len(vals) > 1 {
			pm = vals[1]
		}
		rec.PacMaxW = append(rec.PacMaxW, pm)
	}
	return rec, nil
}

// reRecordStart matches the start of a record such as m[mi++]=" or da[dx++]=".
var reRecordStart = regexp.MustCompile(`\w+\[\w+\+\+\]\s*=\s*"`)

// splitRecords splits a line into its records. The Solar-Log occasionally
// writes a record cut off mid-value with the next one appended on the same
// line (`...;3m[mi++]="..."`); the cut-off record lacks its closing quote and
// is dropped, since its last value is truncated.
func splitRecords(line string) []string {
	starts := reRecordStart.FindAllStringIndex(line, -1)
	if len(starts) == 0 {
		return []string{line}
	}
	out := make([]string, 0, len(starts))
	for i, s := range starts {
		end := len(line)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		rec := strings.TrimSpace(line[s[0]:end])
		if !strings.HasSuffix(strings.TrimSuffix(rec, ";"), `"`) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// ParseLines applies parse to every record of data, usually one per line.
func ParseLines[T any](data []byte, parse func(string) (*T, error)) ([]T, error) {
	var out []T
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		for _, r := range splitRecords(sc.Text()) {
			rec, err := parse(r)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			if rec != nil {
				out = append(out, *rec)
			}
		}
	}
	return out, sc.Err()
}

var (
	reVar     = regexp.MustCompile(`^var\s+(\w+)\s*=\s*(.*?);?\s*$`)
	reIndexed = regexp.MustCompile(`^(\w+)\[(\d+)\]\s*=\s*(.*?);?\s*$`)
	reArray   = regexp.MustCompile(`^new Array\((.*)\)$`)
	reNumber  = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

// ParseBaseVars parses base_vars.js.
func ParseBaseVars(data []byte) (*BaseVars, error) {
	bv := &BaseVars{}
	invs := map[int]*Inverter{}
	inv := func(i int) *Inverter {
		if invs[i] == nil {
			invs[i] = &Inverter{Index: i + 1}
		}
		return invs[i]
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := reVar.FindStringSubmatch(line); m != nil {
			name, val := m[1], m[2]
			switch name {
			case "AnlagenKWP":
				bv.PeakPowerW, _ = strconv.ParseFloat(val, 64)
			case "SollYearKWP":
				bv.TargetYearKWp, _ = strconv.ParseFloat(val, 64)
			case "Verguetung":
				bv.TariffCtPer, _ = strconv.ParseFloat(val, 64)
			case "sollMonth":
				bv.TargetMonthly, _ = floats(jsArray(val))
			case "HPTitel":
				bv.Title = jsString(val)
			case "HPBetreiber":
				bv.Operator = jsString(val)
			case "HPStandort":
				bv.Location = jsString(val)
			case "HPModul":
				bv.Modules = jsString(val)
			case "HPInbetrieb":
				bv.Commissioned = jsString(val)
			case "HPAusricht":
				bv.Orientation = strings.Join(strings.Fields(jsString(val)), " ")
			}
			continue
		}
		if m := reIndexed.FindStringSubmatch(line); m != nil {
			idx, _ := strconv.Atoi(m[2])
			switch m[1] {
			case "WRInfo":
				f := jsArray(m[3])
				if len(f) < 5 {
					continue
				}
				i := inv(idx)
				i.Model = strings.TrimSpace(jsString(f[0]))
				i.Serial = strings.TrimSpace(jsString(f[1]))
				i.PeakPowerW, _ = strconv.ParseFloat(f[2], 64)
				i.Name = strings.TrimSpace(jsString(f[4]))
			case "StatusCodes":
				inv(idx).StatusCodes = strings.Split(jsString(m[3]), ",")
			case "FehlerCodes":
				inv(idx).ErrorCodes = strings.Split(jsString(m[3]), ",")
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for _, i := range slices.Sorted(maps.Keys(invs)) {
		if invs[i].Name != "" {
			bv.Inverters = append(bv.Inverters, *invs[i])
		}
	}
	return bv, nil
}

// ParseCurrent parses min_cur.js.
func ParseCurrent(data []byte, loc *time.Location) (*Current, error) {
	c := &Current{}
	var date, clock string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := reVar.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "Datum":
				date = jsString(m[2])
			case "Uhrzeit":
				clock = jsString(m[2])
			case "PacArr":
				c.Pac = firstOfNested(m[2])
			case "PdcArr":
				c.Pdc = firstOfNested(m[2])
			}
			continue
		}
		if m := reIndexed.FindStringSubmatch(line); m != nil {
			idx, _ := strconv.Atoi(m[2])
			v, _ := strconv.Atoi(m[3])
			switch m[1] {
			case "curStatusCode":
				c.StatusCodes = setAt(c.StatusCodes, idx, v)
			case "curFehlerCode":
				c.ErrorCodes = setAt(c.ErrorCodes, idx, v)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	t, err := time.ParseInLocation("02.01.06 15:04:05", date+" "+clock, loc)
	if err != nil {
		return nil, fmt.Errorf("parse time: %w", err)
	}
	c.Time = t
	return c, nil
}

// Label returns the text for code in a Solar-Log code list.
func Label(codes []string, code int) string {
	if code < 0 || code >= len(codes) {
		return ""
	}
	return strings.TrimSpace(codes[code])
}

func setAt(s []int, i, v int) []int {
	for len(s) <= i {
		s = append(s, 0)
	}
	s[i] = v
	return s
}

// firstOfNested parses [[a,b], [c], ...] and returns the sum of each inner
// array (PdcArr lists per-string values).
func firstOfNested(v string) []float64 {
	v = strings.TrimSuffix(strings.TrimSpace(v), ";")
	v = strings.TrimPrefix(strings.TrimSuffix(v, "]"), "[")
	var out []float64
	for _, part := range strings.Split(v, "]") {
		part = strings.Trim(strings.TrimSpace(part), ",[ ")
		if part == "" {
			continue
		}
		vals, _ := floats(strings.Split(part, ","))
		sum := 0.0
		for _, x := range vals {
			sum += x
		}
		out = append(out, sum)
	}
	return out
}

// jsArray splits the arguments of "new Array(a, "b", c)" (quotes kept).
func jsArray(v string) []string {
	m := reArray.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return nil
	}
	var out []string
	var cur strings.Builder
	inStr := false
	for _, r := range m[1] {
		switch {
		case r == '"':
			inStr = !inStr
			cur.WriteRune(r)
		case r == ',' && !inStr:
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(out, strings.TrimSpace(cur.String()))
}

func jsString(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}

func floats(fs []string) ([]float64, error) {
	out := make([]float64, 0, len(fs))
	for _, f := range fs {
		f = strings.TrimSpace(f)
		if !reNumber.MatchString(f) {
			return nil, fmt.Errorf("invalid number %q", f)
		}
		v, _ := strconv.ParseFloat(f, 64)
		out = append(out, v)
	}
	return out, nil
}
