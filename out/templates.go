package out

import (
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	md "github.com/russross/blackfriday/v2"
	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/vfs"
)

const (
	templateDir        = "templates"
	templateContentDir = "content"
	templateLayoutDir  = "layout"
)

// Each module has one associated template. It contains all of the templates
// in its named directory inside `templates`. Each template should be
// enclosed in a `{{ define "name" }} … {{ end }}` so that they can be referred to by
// the other templates.
//
// The executable should have access to a vfs.Filesystem with the following structure:
// "templates" with the following structure:
//
//     /
//     └─templates/
//       ├─layouts/
//       │ └─directories/*.tmpl files...
//       └─content/
//         └─directories/*.tmpl files...
//
// The path to each .tmpl file determines how it is referred to in
// application code, e.g. templates defined in
// ./templates/content/a/b/c.tmpl are referred to as "a/b/c/<name>".
//
// All layouts get dumped into a common bucket and are cloned as a base for
// every content template.
var (
	Templates map[string]*template.Template

	templateLock sync.RWMutex
	templateFS   vfs.FileSystem

	mdExtensions = md.NoIntraEmphasis | md.FencedCode | md.Strikethrough | md.Footnotes
	mdRenderer   = md.NewHTMLRenderer(md.HTMLRendererParameters{Flags: md.Smartypants})

	globalFuncmap = template.FuncMap{
		"string": func(b []byte) string {
			return string(b)
		},
		"raw": func(s string) template.HTML {
			return template.HTML(s)
		},
		"rawattr": func(s string) template.HTMLAttr {
			return template.HTMLAttr(s)
		},
		"rawurl": func(s string) template.URL {
			return template.URL(s)
		},
		"markdown":  markdown,
		"smarkdown": smarkdown,
		"datetime": func(t time.Time) string {
			return t.Format("2006-01-02T15:04:05Z")
		},
	}
)

func init() {
	gas.Init(func() {
		var err error
		if templateFS == nil {
			templateFS, err = vfs.Native(".")
			if err != nil {
				log.Fatalln("templates:", err)
			}
		}
		err = parseTemplates(templateFS)
		if err != nil {
			log.Fatalln("templates: failed to load:", err)
		}
	})
	gas.Hook(syscall.SIGHUP, func() {
		err := parseTemplates(templateFS)
		if err != nil {
			log.Printf("templates: failed to reload: %v", err)
		} else {
			log.Printf("templates: reloaded all templates")
		}
	})
}

// TemplateFunc adds a function to the template func map which will be
// accessible within the templates. TemplateFunc must be called before Ignition,
// or else it will have no effect.
//
// Predefined global funcs that will be overridden:
//     "string":    func(b []byte) string
//     "raw":       func(s string) template.HTML
//     "markdown":  func(b []byte) template.HTML
//     "smarkdown": func(s string) (template.HTML, error)
//     "datetime":  func(t time.Time) string
func TemplateFunc(name string, f interface{}) {
	globalFuncmap[name] = f
}

// TemplateFS assigns a virtual filesystem to look for HTML templates in. If no
// filesystem is specified before server launch, the default value is a native
// filesystem which looks for a directory called "templates" in the current
// working directory.
func TemplateFS(fs vfs.FileSystem) {
	templateFS = fs
}

// return safe HTML of rendered markdown
func markdown(in []byte) template.HTML {
	return template.HTML(md.Run(in, md.WithExtensions(mdExtensions), md.WithRenderer(mdRenderer)))
}

// return safe HTML of markdown rendered from either a string or sql.NullString
func smarkdown(s interface{}) (template.HTML, error) {
	switch v := s.(type) {
	case sql.NullString:
		if v.Valid {
			return markdown([]byte(v.String)), nil
		}
		return template.HTML(""), nil

	case string:
		return markdown([]byte(v)), nil
	}

	return template.HTML(""), errors.New("non-string type passed into smarkdown")
}

// recursively parse templates in a directory
func parseTemplates(fs vfs.FileSystem) error {
	var (
		templates  = make(map[string]*template.Template)
		layouts    = template.New("layouts").Funcs(globalFuncmap)
		layoutDir  = filepath.Join(templateDir, templateLayoutDir)
		contentDir = filepath.Join(templateDir, templateContentDir)
	)

	fmt.Println(layoutDir)

	err := fs.Walk(layoutDir, func(tmplPath string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() || filepath.Ext(tmplPath) != ".tmpl" {
			return nil
		}

		log.Printf("templates: loading layout '%s'", tmplPath)

		return parseFile(layouts, fs, tmplPath)
	})

	if err != nil {
		abort := true
		if os.IsNotExist(err) {
			if pe, ok := err.(*os.PathError); ok && pe.Path == layoutDir {
				abort = false
			}
		}

		if abort {
			return err
		}
	}

	fmt.Println(contentDir)

	err = fs.Walk(contentDir, func(tmplPath string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() || filepath.Ext(tmplPath) != ".tmpl" {
			return nil
		}

		log.Printf("templates: loading content '%s'", tmplPath)

		// remove the "templates/content" from the front of the path
		name, _ := filepath.Rel(contentDir, tmplPath)
		// drop the .tmpl file from the name
		extlen := len(filepath.Ext(name))
		name = name[:len(name)-extlen]

		// name should now be tmplPath without extension e.g.
		//     templates/a/b.tmpl => "a/b"
		//     templates/c.tmpl   => "c"
		//name := strings.TrimLeft(filepath.Dir(relpath), string([]rune{filepath.Separator}))
		t, ok := templates[name]
		if !ok {
			t, err = layouts.Clone()
			if err != nil {
				return err
			}
			templates[name] = t
		}

		return parseFile(t, fs, tmplPath)
	})

	if err != nil {
		return err
	}

	templateLock.Lock()
	Templates = templates

	for k, t := range Templates {
		for _, tt := range t.Templates() {
			log.Printf("%s/%s", k, tt.Name())
		}
	}
	templateLock.Unlock()

	return nil
}

func parseFile(t *template.Template, fs vfs.FileSystem, tmplPath string) error {
	f, err := fs.Open(tmplPath)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	_, err = t.Parse(string(b))
	return err
}

// represents a template location (containing path and defined name)
type templatePath struct {
	path string
	name string
}

// An outputter that outputs HTML templates
type templateOutputter struct {
	templatePath
	data interface{}
}

// separates a full template path including the path and name into its
// components.
func parseTemplatePath(p string) templatePath {
	if p == "" {
		return templatePath{}
	}
	p = path.Clean(p)
	dir, base := path.Split(p)
	return templatePath{strings.Trim(dir, "/"), strings.Trim(base, "/")}
}

// HTML returns an outputter that will render the named HTML template with
// package html/template, with data as the context, to the response. Templates
// are named by their path and then their defined name within the template,
// e.g. a template in ./templates/content/foo/bar.tmpl defined with name "quux"
// will be called "foo/bar/quux".
//
// Layouts can be applied using the text/template "block" functionality.
// Content placeholders can be defined in a layout template
// ./templates/layout/layouts.tmpl as:
//
//     {{ define "layout-main" }}
//     ...
//     {{ block "content" . }}{{ end }}
//     ...
//     {{ end }}
//
// and in the content template foo/bar:
//
//     {{ define "content" }}
//     ...
//     {{ end }}
//
// The set of layouts are cloned for each content template. So, due to block
// semantics, the content can be rendered by calling HTML with path:
//
//     HTML("foo/bar/content/layout-main", data)
func HTML(path string, data interface{}) gas.Outputter {
	return &templateOutputter{parseTemplatePath(path), data}
}

// Context is passed to every template execution for holding global and local
// state relevant to the rendering.
type Context struct {
	G    *gas.Gas
	Data interface{}

	content func() (string, error)
}

// Content returns the content data for a layout template.
func (c *Context) Content() (string, error) {
	return c.content()
}

func (o *templateOutputter) Output(code int, g *gas.Gas) {
	templateLock.RLock()
	group := Templates[o.path]
	templateLock.RUnlock()
	var t *template.Template

	if group == nil {
		log.Printf("templates: failed to access template group \"%s\"", o.path)
		g.WriteHeader(500)
		fmt.Fprintf(g, "Error: template group \"%s\" not found. Did it fail to compile?", o.path)
		return
	}

	partial := g.Request.Header.Get("X-Ajax-Partial") != ""

	// If it's a partial page request, try to serve a partial template
	// (denoted by a '%' prepended to the template name). If it doesn't
	// exist, fall back to the regular one.
	if partial && !strings.HasPrefix(o.name, "%") {
		t = group.Lookup("%" + o.name)
	}

	if t == nil {
		t = group.Lookup(o.name)
	}

	if t == nil {
		log.Printf("No such template: %s/%s", o.path, o.name)
		g.WriteHeader(500)
		fmt.Fprintf(g, "Error: no such template: %s/%s", o.path, o.name)
		return
	}

	h := g.Header()
	if _, foundType := h["Content-Type"]; !foundType {
		h.Set("Content-Type", "text/html; charset=utf-8")
	}
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

	ctx := &Context{
		G:    g,
		Data: o.data,
	}

	if err := t.Execute(w, ctx); err != nil {
		t = Templates[o.path].Lookup(o.name + "-error")

		if t == nil {
			log.Printf("out: %v", err)
			fmt.Fprintf(w, "%v\n", err)
			msg := fmt.Sprintf("out: %[1]s/%[2]s: %[2]s-error template not found", o.path, o.name)
			log.Println(msg)
			fmt.Fprintln(w, msg)
		} else if err = t.Execute(w, err); err != nil {
			fmt.Fprintf(g, "Error: failed to serve error page for %s/%s (%v)", o.path, o.name, err)
		}
	}
}
