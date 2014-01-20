package gas

import (
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"
	"time"

	md "github.com/russross/blackfriday"
)

// Each module has one associated template. It contains all of the templates
// in its named directory inside `templates`. Each template should be
// enclosed in a `{{ define "name" }} … {{ end }}` so that they can be referred to by
// the other templates.
var (
	Templates       map[string]*template.Template
	templateLock    sync.RWMutex
	template_dir    = "templates"
	templateFuncmap map[string]template.FuncMap
	globalFuncmap   = template.FuncMap{
		"string": func(b []byte) string {
			return string(b)
		},
		"raw": func(s string) template.HTML {
			return template.HTML(s)
		},
		"markdown": markdown,
		"smarkdown": func(s interface{}) (template.HTML, error) {
			switch v := s.(type) {
			case sql.NullString:
				if v.Valid {
					return markdown([]byte(v.String)), nil
				} else {
					return template.HTML(""), nil
				}

			case string:
				return markdown([]byte(v)), nil
			}

			return template.HTML(""), errors.New("non-string type passed into smarkdown")
		},
		"datetime": func(t time.Time) string {
			return t.Format("2006-01-02T15:04:05Z")
		},
	}
)

func init() {
	templateFuncmap = make(map[string]template.FuncMap)
}

// Add a function to the template func map which will be accessible within the
// templates. TemplateFunc must be called before Ignition, or else it will have
// no effect.
//
// Predefined global funcs that will be overridden:
//     "string":    func(b []byte) string
//     "raw":       func(s string) template.HTML
//     "markdown":  func(b []byte) template.HTML
//     "smarkdown": func(s string) (template.HTML, error)
//     "datetime":  func(t time.Time) string
func TemplateFunc(template_name, name string, f interface{}) {
	funcmap, ok := templateFuncmap[template_name]
	if !ok {
		funcmap = make(template.FuncMap)
		templateFuncmap[template_name] = funcmap
	}

	funcmap[name] = f
}

func markdown(in []byte) template.HTML {
	html := md.HTML_GITHUB_BLOCKCODE | md.HTML_USE_SMARTYPANTS
	ext := md.EXTENSION_NO_INTRA_EMPHASIS |
		md.EXTENSION_TABLES |
		md.EXTENSION_FENCED_CODE |
		md.EXTENSION_STRIKETHROUGH |
		md.EXTENSION_FOOTNOTES
	r := md.HtmlRenderer(html, "", "")
	return template.HTML(md.Markdown(in, r, ext))
}

func parse_templates(base string) map[string]*template.Template {
	templateLock.Lock()
	defer templateLock.Unlock()

	ts := make(map[string]*template.Template)
	fis, err := ioutil.ReadDir(base)
	if err != nil {
		LogFatal("Couldn't open templates directory: %v\n", err)
	}
	for _, fi := range fis {
		if !fi.IsDir() {
			continue
		}

		name := fi.Name()
		LogDebug("found template dir '%s'", name)

		localFuncmap, ok := templateFuncmap[name]
		if !ok {
			localFuncmap = make(template.FuncMap)
		}

		for k, v := range globalFuncmap {
			localFuncmap[k] = v
		}

		t, err := template.New(name).
			Funcs(template.FuncMap(localFuncmap)).
			ParseGlob(filepath.Join(base, name, "*.tmpl"))
		if err != nil {
			LogWarning("failed to parse templates in %s: %v\n", name, err)
		}

		ts[name] = t
	}
	return ts
}

type templateOutputter struct {
	path string
	name string
	data interface{}
}

// HTML returns an outputter that will render an HTML template named by path
// and name, using data as the context, to the response.
func HTML(path, name string, data interface{}) Outputter {
	return &templateOutputter{path, name, data}
}

func (o *templateOutputter) Output(code int, g *Gas) {
	h := g.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")

	var w io.Writer
	if strings.Contains(g.Request.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(g)
		defer gz.Close()

		w = io.Writer(gz)
	} else {
		w = g
	}

	g.WriteHeader(code)

	templateLock.RLock()
	defer templateLock.RUnlock()

	var (
		group = Templates[o.path]
		t     *template.Template
	)

	if group == nil {
		LogWarning("Failed to access template group \"%s\"", o.path)
		fmt.Fprintf(w, "Error: template group \"%s\" not found. Did it fail to compile?", o.path)
		return
	}

	// If it's a partial page request, try to serve a partial template
	// (denoted by a '%' prepended to the template name). If it doesn't
	// exist, fall back to the regular one.
	if g.Request.Header.Get("X-Ajax-Partial") != "" {
		t = group.Lookup("%" + o.name)
		if t == nil {
			t = group.Lookup(o.name)
		}
	} else {
		t = group.Lookup(o.name)
	}

	if t == nil {
		LogWarning("No such template: %s/%s", o.path, o.name)
		fmt.Fprintf(w, "Error: no such template: %s/%s", o.path, o.name)
		return
	}

	if err := t.Execute(w, o.data); err != nil {
		t = Templates[o.path].Lookup(o.name + "-error")
		LogWarning("Failed to render template %s/%s: %v", o.path, o.name, err)
		if t == nil {
			LogWarning("Template %s/%s has no error template", o.path, o.name)
			fmt.Fprintf(g, "Error: failed to serve error page for %s/%s (error template not found)", o.path, o.name)
			return
		}
		if err = t.Execute(g, err); err != nil {
			LogWarning("Failed to render error template for %s/%s (%v)", o.path, o.name, err)
			fmt.Fprintf(g, "Error: failed to serve error page for %s/%s (%v)", o.path, o.name, err)
			return
		}
	}
}

// Render a template anywhere.
func ExecTemplate(w io.Writer, path, name string, data interface{}) error {
	templateLock.RLock()
	defer templateLock.RUnlock()

	group := Templates[path]
	if group == nil {
		return fmt.Errorf("gas: ExecTemplate: template group '%s' not found", path)
	}

	t := group.Lookup(name)
	if t == nil {
		return fmt.Errorf("gas: ExecTemplate: named template '%s' not found in group '%s'", name, path)
	}

	return t.Execute(w, data)
}
