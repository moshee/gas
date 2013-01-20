package gas

import (
	"text/template"
	"io"
	"io/ioutil"
	"log"
	"path/filepath"
)

// Each module has one associated template. It contains all of the templates
// in its named directory inside `templates`. Each template should be
// enclosed in a `{{ define "name" }} … {{ end }}` so that they can be referred to by
// the other templates.
var Templates = make(map[string]*template.Template)

func init() {
	parse_templates("templates")
}

func parse_templates(base string) {
	fis, err := ioutil.ReadDir(base)
	if err != nil {
		log.Fatalf("Couldn't open templates directory: %v\n", err)
	}
	for _, fi := range fis {
		if !fi.IsDir() {
			continue
		}
		name := fi.Name()
		t, err := template.New(name).ParseGlob(filepath.Join(base, name, "*.tmpl"))
		if err != nil {
			log.Printf("Warning: failed to parse templates in %s: %v\n", name, err)
		}
		Templates[name] = t
	}
}

func exec_template(path, name string, w io.Writer, data interface{}) {
	Templates[path].Lookup(name).Execute(w, data)
}

// Render the given template by name out of the given directory.
func (g *Gas) Render(path, name string, data interface{}) {
	exec_template(path, name, g.ResponseWriter, data)
}
