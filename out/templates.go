package out

import (
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/moshee/gas"
	md "github.com/russross/blackfriday"
)

// Each module has one associated template. It contains all of the templates
// in its named directory inside `templates`. Each template should be
// enclosed in a `{{ define "name" }} … {{ end }}` so that they can be referred to by
// the other templates.
var (
	Templates    map[string]*template.Template
	templateLock sync.RWMutex
	templateDir  = "templates"

	markdownExts = md.EXTENSION_NO_INTRA_EMPHASIS | md.EXTENSION_TABLES |
		md.EXTENSION_FENCED_CODE | md.EXTENSION_STRIKETHROUGH |
		md.EXTENSION_FOOTNOTES

	markdownRenderer = md.HtmlRenderer(md.HTML_GITHUB_BLOCKCODE|md.HTML_USE_SMARTYPANTS, "", "")

	globalFuncmap = template.FuncMap{
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
				}
				return template.HTML(""), nil

			case string:
				return markdown([]byte(v)), nil
			}

			return template.HTML(""), errors.New("non-string type passed into smarkdown")
		},
		"datetime": func(t time.Time) string {
			return t.Format("2006-01-02T15:04:05Z")
		},
		"content": func() (string, error) {
			return "", errors.New("content template func used from non-layout context")
		},
	}
)

func init() {
	parseTemplates(templateDir)
	gas.Hook(syscall.SIGUSR1, func() {
		parseTemplates(templateDir)
		log.Printf("Templates reloaded.")
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

// return safe HTML of rendered markdown
func markdown(in []byte) template.HTML {
	return template.HTML(md.Markdown(in, markdownRenderer, markdownExts))
}

// recursively parse templates in a directory
func parseTemplates(base string) {
	os.MkdirAll(base, 0755)
	templateLock.Lock()
	defer templateLock.Unlock()

	Templates = make(map[string]*template.Template)

	err := filepath.Walk(base, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return nil
		}

		glob := filepath.Join(path, "*.tmpl")
		log.Printf("adding templates in %s", glob)
		files, err := filepath.Glob(glob)
		if err != nil {
			return err
		}

		if len(files) == 0 {
			log.Printf("no template files in %s", path)
			return nil
		}

		t, err := template.New(path).Funcs(globalFuncmap).ParseFiles(files...)
		if err != nil {
			return err
		}

		name := strings.TrimPrefix(path, base)
		name = strings.TrimLeftFunc(name, func(c rune) bool { return c == filepath.Separator })
		Templates[name] = t
		return nil
	})

	if err != nil {
		if os.IsNotExist(err) {
			log.Fatalf("missing 'templates' directory")
		} else {
			log.Fatalf("templates: %v", err)
		}
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

func (o *templateOutputter) Output(code int, g *gas.Gas) {
	templateLock.RLock()
	defer templateLock.RUnlock()
	group := Templates[o.path]
	var t *template.Template

	if group == nil {
		log.Printf("Failed to access template group \"%s\"", o.path)
		g.WriteHeader(500)
		fmt.Fprintf(g, "Error: template group \"%s\" not found. Did it fail to compile?", o.path)
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

	if o.layouts != nil && len(o.layouts) > 0 {
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

		for n, path := range o.layouts {
			if err := (func(i int) error {
				group, ok := Templates[path.path]
				if !ok {
					return fmt.Errorf("no such template path %s for layout %s", path.path, path.name)
				}
				layout := group.Lookup(path.name)
				if layout == nil {
					return fmt.Errorf("no such layout %s in path %s", path.name, path.path)
				}

				layouts[i] = layout

				// closure closes over:
				// - layouts slice so that it can access the next layout,
				// - w so that it can write a template with minimal buffering,
				// - i so that it knows its position,
				// - t to render the final content.

				funcs[i] = func() {
					f := func() (string, error) {
						// If this is the last layout in the queue, then do the
						// data instead. Then it'll stop "recursing" to this
						// closure.
						if i < len(funcs)-1 {
							funcs[i+1]()
							layouts[i+1].Execute(w, o.data)
							return "", nil
						}
						return "", t.Execute(w, o.data)
					}
					layout.Funcs(template.FuncMap{"content": f})
				}

				return nil
			})(n); err != nil {
				log.Printf("Render: Layouts: %v", err)
				g.WriteHeader(500)
				fmt.Fprint(w, err.Error())
				return
			}
		}

		g.WriteHeader(code)
		funcs[0]()
		layouts[0].Execute(w, o.data)
		return
	}

	g.WriteHeader(code)

	if err := t.Execute(w, o.data); err != nil {
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
