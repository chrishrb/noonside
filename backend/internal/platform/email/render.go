package email

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"sort"
	"strings"
	texttemplate "text/template"
	"time"
)

//go:embed all:templates
var templateFS embed.FS

// App holds values shared by all emails (branding, links).
type App struct {
	Name string
	// URL is the public frontend URL, used for the logo link and footer.
	URL string
}

// view is what templates receive: .App for shared values, .Data for the email.
type view struct {
	App       App
	Data      Email
	Subject   string
	Preheader string
	Year      int
}

// Rendered is a fully rendered email.
type Rendered struct {
	Subject string
	HTML    string
	Text    string
}

type Renderer struct {
	app  App
	html map[string]*htmltemplate.Template
	text map[string]*texttemplate.Template
}

var funcs = map[string]any{
	"date": func(t time.Time) string { return t.Format("02.01.2006") },
	"datetime": func(t time.Time) string {
		return t.Format("02.01.2006 15:04")
	},
	// map builds template arguments: (map "URL" .x "Label" "Go").
	"map": func(kv ...any) (map[string]any, error) {
		if len(kv)%2 != 0 {
			return nil, fmt.Errorf("map: odd number of arguments")
		}
		m := make(map[string]any, len(kv)/2)
		for i := 0; i < len(kv); i += 2 {
			k, ok := kv[i].(string)
			if !ok {
				return nil, fmt.Errorf("map: key %v is not a string", kv[i])
			}
			m[k] = kv[i+1]
		}
		return m, nil
	},
	// rows builds label/value pairs for the "facts" partial.
	"rows": func(kv ...any) ([][2]any, error) {
		if len(kv)%2 != 0 {
			return nil, fmt.Errorf("rows: odd number of arguments")
		}
		out := make([][2]any, 0, len(kv)/2)
		for i := 0; i < len(kv); i += 2 {
			out = append(out, [2]any{kv[i], kv[i+1]})
		}
		return out, nil
	},
	"repeat": strings.Repeat,
	"title": func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	},
}

// NewRenderer parses all templates. Every templates/<name>.html is combined
// with the shared layout and partials.
func NewRenderer(app App) (*Renderer, error) {
	if app.Name == "" {
		app.Name = "noonside"
	}
	r := &Renderer{app: app, html: map[string]*htmltemplate.Template{}, text: map[string]*texttemplate.Template{}}

	base, err := htmltemplate.New("root").Funcs(funcs).ParseFS(templateFS, "templates/_*.html")
	if err != nil {
		return nil, fmt.Errorf("parse email layout: %w", err)
	}
	textBase, err := texttemplate.New("root").Funcs(funcs).ParseFS(templateFS, "templates/_*.txt")
	if err != nil {
		return nil, fmt.Errorf("parse email text layout: %w", err)
	}

	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, path := range names {
		file := strings.TrimPrefix(path, "templates/")
		if strings.HasPrefix(file, "_") {
			continue
		}
		name := strings.TrimSuffix(file, ".html")

		h, err := base.Clone()
		if err != nil {
			return nil, err
		}
		if r.html[name], err = h.ParseFS(templateFS, path); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}

		t, err := textBase.Clone()
		if err != nil {
			return nil, err
		}
		if r.text[name], err = t.ParseFS(templateFS, "templates/"+name+".txt"); err != nil {
			return nil, fmt.Errorf("parse templates/%s.txt: %w", name, err)
		}
	}
	return r, nil
}

// Names lists all emails that have templates.
func (r *Renderer) Names() []string {
	out := make([]string, 0, len(r.html))
	for n := range r.html {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (r *Renderer) Render(e Email) (*Rendered, error) {
	h, ok := r.html[e.Name()]
	if !ok {
		return nil, fmt.Errorf("no templates for email %q", e.Name())
	}
	v := view{
		App:       r.app,
		Data:      e,
		Subject:   e.Subject(),
		Preheader: e.Preheader(),
		Year:      time.Now().Year(),
	}
	var hb, tb bytes.Buffer
	if err := h.ExecuteTemplate(&hb, "layout", v); err != nil {
		return nil, fmt.Errorf("render %s html: %w", e.Name(), err)
	}
	if err := r.text[e.Name()].ExecuteTemplate(&tb, "layout", v); err != nil {
		return nil, fmt.Errorf("render %s text: %w", e.Name(), err)
	}
	return &Rendered{
		Subject: e.Subject(),
		HTML:    hb.String(),
		Text:    strings.TrimSpace(tb.String()) + "\n",
	}, nil
}
