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
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	md "github.com/russross/blackfriday"
	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/vfs"
)

const (
	templateDir = "templates"
)

// Each module has one associated template. It contains all of the templates
// in its named directory inside `templates`. Each template should be
// enclosed in a `{{ define "name" }} … {{ end }}` so that they can be referred to by
// the other templates.
var (
	Templates    map[string]*template.Template
	templateLock sync.RWMutex
	templateFS   vfs.FileSystem

	markdownExts = md.EXTENSION_NO_INTRA_EMPHASIS | md.EXTENSION_TABLES |
		md.EXTENSION_FENCED_CODE | md.EXTENSION_STRIKETHROUGH |
		md.EXTENSION_FOOTNOTES

	markdownRenderer = md.HtmlRenderer(md.HTML_USE_SMARTYPANTS, "", "")

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
			templateFS, err = vfs.NewNativeFS(".")
			if err != nil {
				log.Fatalf("templates: %v", err)
			}
		}
		parseTemplates(templateFS)
	})
	gas.Hook(syscall.SIGHUP, func() {
		parseTemplates(templateFS)
		log.Printf("templates: reloaded all templates")
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
	return template.HTML(md.Markdown(in, markdownRenderer, markdownExts))
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
func parseTemplates(fs vfs.FileSystem) {
	// fs should be a filesystem with a dir in the top level called
	// "templates". Inside this dir should be an arbitrary dir tree full of
	// *.tmpl files. The path to each .tmpl file determines how it is referred
	// to in application code, e.g. templates defined in ./templates/a/b/c.tmpl
	// are referred to as "a/b/<name>".

	//os.MkdirAll(base, 0755)
	templateLock.Lock()
	defer templateLock.Unlock()

	Templates = make(map[string]*template.Template)

	e := fs.Walk("templates", func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() || filepath.Ext(path) != ".tmpl" {
			return nil
		}

		log.Printf("templates: loading '%s'", path)

		// remove the "templates/" from the front of the path
		name, _ := filepath.Rel(templateDir, path)
		// drop the .tmpl file from the name
		name = filepath.Dir(name)
		if name == "." {
			name = ""
		}
		// name should now be path without templates e.g.
		//     templates/a/b.tmpl => "a"
		//     templates/c.tmpl   => ""
		//name := strings.TrimLeft(filepath.Dir(relpath), string([]rune{filepath.Separator}))
		t, ok := Templates[name]
		if !ok {
			t = template.New(path).Funcs(globalFuncmap)
			Templates[name] = t
		}

		f, err := fs.Open(path)
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
	})

	if e != nil {
		log.Fatalf("templates: %v", e)
	}
}

// represents a template location (containing path and defined name)
type templatePath struct {
	path string
	name string
}

// An outputter that outputs HTML templates
type templateOutputter struct {
	templatePath
	layouts []templatePath
	data    interface{}
}

// separates a full template path including the path and name into its
// components.
func parseTemplatePath(path string) templatePath {
	i := strings.LastIndex(path, "/")
	var name string
	if i < 0 {
		name = path
		path = ""
	} else {
		name = path[i+1:]
		path = path[:i]
	}
	return templatePath{path, name}
}

// HTML returns an outputter that will render the named HTML template with
// package html/template, with data as the context, to the response. Templates
// are named by their path and then their defined name within the template,
// e.g. a template in ./templates/foo/bar.tmpl defined with name "quux" will be
// called "foo/bar/quux".
//
// Layouts are applied in order outside-in, e.g.
// layout1(layout2(content(data))) and are each executed with the top level
// data binding. Each layout has access to a "content" function which will
// instruct the next layout or the content to be rendered in its place. The
// io.Writer is injected into the function's scope in a closure, so minimal
// buffering takes place. It is an error to call the "content" function in a
// non-layout template.
func HTML(path string, data interface{}, layoutPaths ...string) gas.Outputter {
	var layouts []templatePath
	if len(layoutPaths) > 0 {
		layouts = make([]templatePath, len(layoutPaths))
		for i, path := range layoutPaths {
			layouts[i] = parseTemplatePath(path)
		}
	}

	return &templateOutputter{parseTemplatePath(path), layouts, data}
}

type Context struct {
	G    *gas.Gas
	Data interface{}

	content func() (string, error)
}

func (c *Context) Content() (string, error) {
	return c.content()
}

func (o *templateOutputter) Output(code int, g *gas.Gas) {
	templateLock.RLock()
	defer templateLock.RUnlock()
	group := Templates[o.path]
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

	if !partial && o.layouts != nil && len(o.layouts) > 0 {
		layouts := make([]*template.Template, len(o.layouts))

		// conceptually the layouts are arranged like this
		// [l1, l2, l3] t
		//  ^
		// execution starts at the beginning of the queue. l1 has a link via
		// the closure below to l(1+1) = l2, l2 has a link to l3, and l3 has a
		// link to t. Once the execution chain starts, each one will fire off
		// the next one until it reaches the end, at which point the main
		// content template is rendered. The layouts will then be rendered
		// outside-in with the main content last (innermost).

		// we need this func slice to properly close over the loop variables.
		// Otherwise the value of n would be the final value always. The layout
		// execution would then always skip all layouts after the first.
		funcs := make([]func(), len(o.layouts))
		contexts := make([]*Context, len(o.layouts))

		for n, path := range o.layouts {
			if err := (func(i int) error {
				group, ok := Templates[path.path]
				if !ok {
					return fmt.Errorf("no such template path %q for layout %q", path.path, path.name)
				}
				layout := group.Lookup(path.name)
				if layout == nil {
					return fmt.Errorf("no such layout %q in path %q", path.name, path.path)
				}

				layouts[i] = layout

				// closure closes over:
				// - layouts slice so that it can access the next layout,
				// - w so that it can write a template with minimal buffering,
				// - i so that it knows its position,
				// - t to render the final content.

				contexts[i] = &Context{
					G:    g,
					Data: o.data,
				}

				funcs[i] = func() {
					contexts[i].content = func() (string, error) {
						// If this is the last layout in the queue, then do the
						// data instead. Then it'll stop "recursing" to this
						// closure.
						if i < len(funcs)-1 {
							funcs[i+1]()
							return "", layouts[i+1].Execute(w, contexts[i+1])
						}
						return "", t.Execute(w, contexts[i])
					}
				}

				return nil
			})(n); err != nil {
				log.Printf("Render: Layouts: %v", err)
				g.WriteHeader(500)
				fmt.Fprint(w, err)
				return
			}
		}

		g.WriteHeader(code)
		funcs[0]()
		if err := layouts[0].Execute(w, contexts[0]); err != nil {
			fmt.Fprint(w, err)
		}
		return
	}

	g.WriteHeader(code)

	ctx := &Context{
		G:    g,
		Data: o.data,
	}

	if err := t.Execute(w, ctx); err != nil {
		t = Templates[o.path].Lookup(o.name + "-error")
		if t == nil {
			fmt.Fprintf(g, "Error: failed to serve error page for %s/%s (error template not found)", o.path, o.name)
			return
		}
		if err = t.Execute(g, err); err != nil {
			fmt.Fprintf(g, "Error: failed to serve error page for %s/%s (%v)", o.path, o.name, err)
			return
		}
	}
}
