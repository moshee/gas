package gas

import (
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
		"eq": func(a, b string) bool {
			return a == b
		},
		"string": func(b []byte) string {
			return string(b)
		},
		"raw": func(s string) template.HTML {
			return template.HTML(s)
		},
		"markdown": markdown,
		"smarkdown": func(s string) template.HTML {
			return markdown([]byte(s))
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
//     "eq":        func(a, b string) bool
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

func exec_template(path, name string, w io.Writer, data interface{}) {
	t := Templates[path].Lookup(name)
	if t == nil {
		panic("No such template: " + path + "/" + name)
	}
	if err := t.Execute(w, data); err != nil {
		// TODO: this
		panic(err)
	}
}

// Render the given template by name out of the given directory.
func (g *Gas) Render(path, name string, data interface{}) {
	templateLock.Lock()
	defer templateLock.Unlock()
	exec_template(path, name, g.ResponseWriter, data)
}
