package gas

import (
	"database/sql"
	"fmt"
	md "github.com/russross/blackfriday"
	"html/template"
	"io"
	"io/ioutil"
	"path/filepath"
	"sync"
)

// Each module has one associated template. It contains all of the templates
// in its named directory inside `templates`. Each template should be
// enclosed in a `{{ define "name" }} … {{ end }}` so that they can be referred to by
// the other templates.
var (
	Templates        map[string]*template.Template
	templateLock     sync.Mutex
	template_dir     = "templates"
	template_funcmap map[string]template.FuncMap
	global_funcmap   = template.FuncMap{
		"string": func(b []byte) string {
			return string(b)
		},
		"raw": func(s string) template.HTML {
			return template.HTML(s)
		},
		"markdown": markdown,
		"smarkdown": func(s interface{}) template.HTML {
			switch v := s.(type) {
			case sql.NullString:
				if v.Valid {
					return markdown([]byte(v.String))
				} else {
					return template.HTML("")
				}

			case string:
				return markdown([]byte(v))
			}

			return template.HTML("")
		},
	}
)

func init() {
	template_funcmap = make(map[string]template.FuncMap)
}

// Add a function to the template func map which will be accessible within the
// templates. TemplateFunc must be called before Ignition, or else it will have
// no effect.
//
// Predefined global funcs that will be overridden:
//     "string":    func(b []byte) string
//     "raw":       func(s string) template.HTML
//     "markdown":  func(b []byte) template.HTML
//     "smarkdown": func(s string) template.HTML
func TemplateFunc(template_name, name string, f interface{}) {
	funcmap, ok := template_funcmap[template_name]
	if !ok {
		funcmap = make(template.FuncMap)
		template_funcmap[template_name] = funcmap
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
	ts := make(map[string]*template.Template)
	fis, err := ioutil.ReadDir(base)
	if err != nil {
		Log(Fatal, "Couldn't open templates directory: %v\n", err)
	}
	for _, fi := range fis {
		if !fi.IsDir() {
			continue
		}

		name := fi.Name()
		Log(Debug, "found template dir '%s'", name)

		local_funcmap, ok := template_funcmap[name]
		if !ok {
			local_funcmap = make(template.FuncMap)
		}

		for k, v := range global_funcmap {
			local_funcmap[k] = v
		}

		t, err := template.New(name).
			Funcs(template.FuncMap(local_funcmap)).
			ParseGlob(filepath.Join(base, name, "*.tmpl"))
		if err != nil {
			Log(Warning, "failed to parse templates in %s: %v\n", name, err)
		}

		ts[name] = t
	}
	return ts
}

func (g *Gas) exec_template(path, name string, data interface{}) {
	var (
		group *template.Template
		w     io.Writer = g.ResponseWriter
	)

	if g.Request.Header.Get("X-Ajax-Partial") != "" {
		// If it's a partial page request, try to serve a partial template
		// (denoted by a '%' prepended to the template name). If it doesn't
		// exist, fall back to the regular one.
		group = Templates["%"+path]
		if group == nil {
			group = Templates[path]
		}
	} else {
		group = Templates[path]
	}

	if group == nil {
		Log(Warning, "Failed to access template group \"%s\"", path)
		fmt.Fprintf(w, "Error: template group \"%s\" not found. Did it fail to compile?", path)
		return
	}

	t := group.Lookup(name)

	if t == nil {
		Log(Warning, "No such template: %s/%s", path, name)
		fmt.Fprintf(w, "Error: no such template: %s/%s", path, name)
		return
	}
	if err := t.Execute(w, data); err != nil {
		t = Templates[path].Lookup(name + "-error")
		Log(Warning, "Failed to render template %s/%s: %v", path, name, err)
		if t == nil {
			Log(Warning, "Template %s/%s has no error template", path, name)
			fmt.Fprintf(w, "Error: failed to serve error page for %s/%s (error template not found)", path, name)
			return
		}
		if err = t.Execute(w, err); err != nil {
			Log(Warning, "Failed to render error template for %s/%s (%v)", path, name, err)
			fmt.Fprintf(w, "Error: failed to serve error page for %s/%s (%v)", path, name, err)
			return
		}
	}
}

// Render the given template by name out of the given directory.
func (g *Gas) Render(path, name string, data interface{}) {
	templateLock.Lock()
	defer templateLock.Unlock()
	g.exec_template(path, name, data)
}
