package gas

import (
	md "github.com/russross/blackfriday"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"path/filepath"
	"sync"
)

// Each module has one associated template. It contains all of the templates
// in its named directory inside `templates`. Each template should be
// enclosed in a `{{ define "name" }} … {{ end }}` so that they can be referred to by
// the other templates.
var (
	Templates    map[string]*template.Template
	templateLock sync.Mutex
	template_dir = "templates"
)

func init() {
	Templates = parse_templates("templates")
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
		log.Fatalf("Couldn't open templates directory: %v\n", err)
	}
	for _, fi := range fis {
		if !fi.IsDir() {
			continue
		}
		name := fi.Name()
		Log(Debug, "found template dir '%s'", name)
		t, err := template.New(name).Funcs(
			template.FuncMap{
				"eq": func(a, b string) bool {
					return a == b
				},
				"string": func(b []byte) string {
					return string(b)
				},
				"raw": func(s string) template.HTML {
					return template.HTML(s)
				},
				"markdown": func(b []byte) template.HTML {
					return markdown(b)
				},
				"smarkdown": func(s string) template.HTML {
					return markdown([]byte(s))
				},
			}).ParseGlob(filepath.Join(base, name, "*.tmpl"))
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
