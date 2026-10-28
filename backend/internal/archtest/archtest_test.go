// Package archtest checks the boundaries of the modular monolith: which
// modules may import which, that only the composition root uses the
// modules' API handlers, and that each module's SQL touches only its own
// tables.
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/chrishrb/noonside/internal/"

// allowed lists the modules each module may import (their root packages
// only). Keep it a DAG: a module may only depend on modules listed before
// it in this order: identity, systems, telemetry, weather, prices, advice,
// datasources, notifications, alerting, reports, webhooks, analytics;
// waitlist stands alone.
var allowed = map[string][]string{
	"identity":      {},
	"systems":       {"identity"},
	"telemetry":     {"systems"},
	"weather":       {"systems", "telemetry"},
	"prices":        {"systems", "telemetry"},
	"advice":        {"systems", "weather", "prices"},
	"datasources":   {"systems", "telemetry"},
	"notifications": {"identity", "systems"},
	"alerting":      {"identity", "systems", "telemetry", "weather", "advice", "datasources", "notifications"},
	"reports":       {"systems", "telemetry", "weather", "prices", "alerting", "notifications"},
	"webhooks":      {"identity", "systems", "telemetry", "prices", "advice", "alerting", "notifications", "reports"},
	"analytics":     {"systems", "telemetry", "weather", "prices", "alerting", "datasources"},
	"waitlist":      {},
}

// owner maps every table to the module whose SQL may use it. A new
// migration's table must be added here.
var owner = map[string]string{
	"profiles":                "identity",
	"systems":                 "systems",
	"system_planes":           "systems",
	"system_targets":          "systems",
	"inverters":               "systems",
	"system_members":          "systems",
	"invitations":             "systems",
	"api_keys":                "systems",
	"share_links":             "systems",
	"measurement_days":        "telemetry",
	"inverter_daily":          "telemetry",
	"inverter_state":          "telemetry",
	"inverter_events":         "telemetry",
	"ingest_rejections":       "telemetry",
	"weather_forecasts":       "weather",
	"expected_daily":          "weather",
	"expected_daily_keys":     "weather",
	"market_prices":           "prices",
	"data_sources":            "datasources",
	"notifications":           "notifications",
	"notification_deliveries": "notifications",
	"notification_prefs":      "notifications",
	"notification_targets":    "notifications",
	"push_subscriptions":      "notifications",
	"alerts":                  "alerting",
	"alert_rules":             "alerting",
	"webhooks":                "webhooks",
	"webhook_deliveries":      "webhooks",
	"waitlist_entries":        "waitlist",
	"pricing_clicks":          "waitlist",
	// platform/db
	"job_leases": "",
}

// pkg is a parsed Go package directory.
type pkg struct {
	dir     string // relative to internal/, slash-separated
	imports []string
	strings []string // string literals of non-test files
}

func loadPackages(t *testing.T) []pkg {
	t.Helper()
	byDir := map[string]*pkg{}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel("..", filepath.Dir(path))
		rel = filepath.ToSlash(rel)
		if strings.Contains(rel, "/generated") || strings.Contains(rel, "testdata") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		p := byDir[rel]
		if p == nil {
			p = &pkg{dir: rel}
			byDir[rel] = p
		}
		test := strings.HasSuffix(path, "_test.go")
		for _, imp := range f.Imports {
			v, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(v, modulePath) {
				p.imports = append(p.imports, strings.TrimPrefix(v, modulePath))
			}
		}
		if !test {
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						p.strings = append(p.strings, s)
					}
				}
				return true
			})
		}
		return nil
	})
	require.NoError(t, err)
	out := make([]pkg, 0, len(byDir))
	for _, p := range byDir {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
	return out
}

// moduleOf returns the module of a package directory ("" outside modules).
func moduleOf(dir string) string {
	rest, ok := strings.CutPrefix(dir, "modules/")
	if !ok {
		return ""
	}
	m, _, _ := strings.Cut(rest, "/")
	return m
}

// isAPI reports whether dir is a module's API handler package.
func isAPI(dir string) bool {
	m := moduleOf(dir)
	return m != "" && dir == "modules/"+m+"/"+m+"api"
}

func TestModuleDependencies(t *testing.T) {
	pkgs := loadPackages(t)
	for _, p := range pkgs {
		from := moduleOf(p.dir)
		for _, imp := range p.imports {
			to := moduleOf(imp)
			switch {
			case strings.HasPrefix(p.dir, "platform") && to != "":
				t.Errorf("%s: platform must not import modules (%s)", p.dir, imp)
			case isAPI(imp) && p.dir != "app":
				t.Errorf("%s: only internal/app may import the API package %s", p.dir, imp)
			case from == "" || to == "" || to == from:
			case imp != "modules/"+to:
				t.Errorf("%s: other modules may only import the root package of %s, not %s", p.dir, to, imp)
			case !slices.Contains(allowed[from], to):
				t.Errorf("%s: module %s must not depend on %s (see allowed)", p.dir, from, to)
			}
		}
	}
	for m := range allowed {
		_, err := os.Stat(filepath.Join("..", "modules", m))
		assert.NoError(t, err, "module %s in allowed", m)
	}
}

var sqlTable = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE|TABLE)\s+(?:ONLY\s+)?([a-z_][a-z0-9_.]*)`)

func TestTableOwnership(t *testing.T) {
	for _, p := range loadPackages(t) {
		mod := moduleOf(p.dir)
		if p.dir == "platform/db" {
			mod = ""
		} else if mod == "" {
			// SQL lives in modules and platform/db only.
			for _, s := range p.strings {
				for _, m := range sqlTable.FindAllStringSubmatch(s, -1) {
					if _, ok := owner[m[1]]; ok {
						t.Errorf("%s: SQL on table %s outside its module", p.dir, m[1])
					}
				}
			}
			continue
		}
		for _, s := range p.strings {
			for _, m := range sqlTable.FindAllStringSubmatch(s, -1) {
				table := m[1]
				if table == "auth.users" && mod == "identity" {
					continue
				}
				o, ok := owner[table]
				if ok && o != mod {
					t.Errorf("%s: SQL on table %s owned by %q", p.dir, table, o)
				}
			}
		}
	}
}

var createTable = regexp.MustCompile(`(?i)\bcreate\s+table\s+(?:if\s+not\s+exists\s+)?(?:public\.)?([a-z_][a-z0-9_]*)`)
var dropTable = regexp.MustCompile(`(?i)\bdrop\s+table\s+(?:if\s+exists\s+)?(?:public\.)?([a-z_][a-z0-9_]*)`)

// Every table of the migrations needs an owner.
func TestTablesHaveOwners(t *testing.T) {
	files, err := filepath.Glob("../../db/migrations/*.sql")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	tables := map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, m := range createTable.FindAllStringSubmatch(string(b), -1) {
			tables[strings.ToLower(m[1])] = true
		}
		for _, m := range dropTable.FindAllStringSubmatch(string(b), -1) {
			delete(tables, strings.ToLower(m[1]))
		}
	}
	for table := range tables {
		_, ok := owner[table]
		assert.True(t, ok, "table %s has no owner in archtest.owner", table)
	}
	for table := range owner {
		assert.True(t, tables[table], "owner lists %s, which no migration creates", table)
	}
}
