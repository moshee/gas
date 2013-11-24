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
	templateLock     sync.RWMutex
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
			LogWarning("failed to parse templates in %s: %v\n", name, err)
		}

		ts[name] = t
	}
	return ts
}

// Render the given template by name out of the given directory.
func (g *Gas) Render(path, name string, data interface{}) {
	templateLock.RLock()
	defer templateLock.RUnlock()

	var (
		group = Templates[path]
		t     *template.Template
	)

	if group == nil {
		LogWarning("Failed to access template group \"%s\"", path)
		fmt.Fprintf(g, "Error: template group \"%s\" not found. Did it fail to compile?", path)
		return
	}

	// If it's a partial page request, try to serve a partial template
	// (denoted by a '%' prepended to the template name). If it doesn't
	// exist, fall back to the regular one.
	if g.Request.Header.Get("X-Ajax-Partial") != "" {
		t = group.Lookup("%" + name)
		if t == nil {
			t = group.Lookup(name)
		}
	} else {
		t = group.Lookup(name)
	}

	if t == nil {
		LogWarning("No such template: %s/%s", path, name)
		fmt.Fprintf(g, "Error: no such template: %s/%s", path, name)
		return
	}
	if err := t.Execute(g, data); err != nil {
		t = Templates[path].Lookup(name + "-error")
		LogWarning("Failed to render template %s/%s: %v", path, name, err)
		if t == nil {
			LogWarning("Template %s/%s has no error template", path, name)
			fmt.Fprintf(g, "Error: failed to serve error page for %s/%s (error template not found)", path, name)
			return
		}
		if err = t.Execute(g, err); err != nil {
			LogWarning("Failed to render error template for %s/%s (%v)", path, name, err)
			fmt.Fprintf(g, "Error: failed to serve error page for %s/%s (%v)", path, name, err)
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
