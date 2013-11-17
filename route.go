package gas

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// All request handlers implemented should have this signature.
type Handler func(*Gas)

type matcher struct {
	s    string
	name string
	// the byte directly after the match group. the matcher will look for this
	// to know when to stop capturing (this behavior only occurs for ID
	// matchers)
	// if next is 0 ('\0'), that means it's the last token in the string. this
	// means that the captured string should include the entire trailing
	// portion of the string (the entire string passed into match())
	next byte
}

func (m matcher) String() string {
	return fmt.Sprintf("[s='%s' name='%s' next='%c']", m.s, m.name, m.next)
}

// Try to capture a segment of the remaining path fragment.
// empty string denotes no match
func (m matcher) match(s string) string {
	if len(m.name) == 0 {
		if len(s) < len(m.s) || m.s != s[:len(m.s)] {
			return ""
		}
		return m.s
	}
	if m.next == 0 {
		return s
	}
	for i := 0; i < len(s); i++ {
		if s[i] == m.next {
			return s[:i]
		}
	}
	return s
}

type route struct {
	method   string
	matchers []matcher
	handlers []Handler
}

// Compile a route string into a usable format.
func newRoute(method, pattern string, handlers []Handler) (r *route) {
	r = new(route)
	r.method = method
	r.matchers = make([]matcher, 0)
	r.handlers = handlers

	last := 0
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			if i > 0 {
				s := pattern[last:i]
				r.matchers = append(r.matchers, matcher{s, "", 0})
			}
			last = i + 1
		case '}':
			s := pattern[last:i]
			var next byte
			if i+1 < len(pattern) {
				next = pattern[i+1]
			}
			last = i + 1
			r.matchers = append(r.matchers, matcher{"", s, next})
		}
	}
	if last < len(pattern) {
		r.matchers = append(r.matchers, matcher{pattern[last:], "", 0})
	}
	return
}

func (r *route) String() string {
	return fmt.Sprintf("[%s] %v", r.method, r.matchers)
}

func (r *route) match(method, url string) (map[string]string, bool) {
	if method != r.method {
		return nil, false
	}
	values := make(map[string]string)
	i := 0
	for _, m := range r.matchers {
		if s := m.match(url[i:]); len(s) > 0 {
			if len(m.name) != 0 {
				values[m.name] = s
			}
			i += len(s)
		} else {
			return nil, false
		}
	}
	// don't match if there was still more url left
	if len(url[i:]) > 0 {
		return nil, false
	}
	return values, true
}

var (
	default_router *Router
	routers        = make(map[string]*Router)
)

// Router is the URL router. Attached methods may be chained for easy adding of
// routes.
type Router struct {
	routes []*route
}

// Create a new Router that responds to the given subdomains. If no subdomains
// are given, it assumes the base host.
func New(subdomains ...string) (r *Router) {
	r = new(Router)
	r.routes = make([]*route, 0)
	if len(subdomains) > 0 {
		for _, s := range subdomains {
			routers[s] = r
		}
	} else {
		default_router = r
	}
	return
}

func (r *Router) match(req *http.Request) (map[string]string, []Handler) {
	for _, route := range r.routes {
		if values, ok := route.match(req.Method, req.URL.Path); ok {
			return values, route.handlers
		}
	}
	return nil, nil
}

// Add a route to the router using the given method.
func (r *Router) Add(pattern string, method string, handlers ...Handler) *Router {
	r.routes = append(r.routes, newRoute(method, pattern, handlers))
	return r
}

func (r *Router) Head(pattern string, handlers ...Handler) *Router {
	return r.Add(pattern, "HEAD", handlers...)
}

func (r *Router) Get(pattern string, handlers ...Handler) *Router {
	// Go1.2 adds HEAD to GET automatically
	return r.Add(pattern, "GET", handlers...)
}

func (r *Router) Post(pattern string, handlers ...Handler) *Router {
	return r.Add(pattern, "POST", handlers...)
}

func (r *Router) Put(pattern string, handlers ...Handler) *Router {
	return r.Add(pattern, "PUT", handlers...)
}

func (r *Router) Delete(pattern string, handlers ...Handler) *Router {
	return r.Add(pattern, "DELETE", handlers...)
}

func dispatch(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if nuke := recover(); nuke != nil {
			g := &Gas{ResponseWriter: w, Request: r}
			var (
				err error
				ok  bool
			)
			if err, ok = nuke.(error); !ok {
				err = fmt.Errorf("%v", nuke)
			}
			g.Error(500, err)
		}
	}()
	defer r.Body.Close()
	r.Close = true

	now := time.Now()
	router := routers[strings.Split(r.Host, ":")[0]]
	if router == nil {
		router = default_router
	}

	g := &Gas{
		ResponseWriter: w,
		Request:        r,
	}

	// Handle reroute cookie if there is one
	reroute, err := g.Cookie("_reroute")
	if err == nil {
		blob, err := base64.StdEncoding.DecodeString(reroute.Value)

		if err == nil {
			g.RerouteInfo = &RerouteInfo{raw: blob}
		} else {
			Log(Warning, "gas: dispatch reroute: %v", err)
		}

		// Empty the cookie out and toss it back
		reroute.Value = ""
		reroute.MaxAge = -1

		g.SetCookie(reroute)
	}

	if values, handlers := router.match(r); handlers != nil {
		g.args = values
		for _, handler := range handlers {
			handler(g)
			if g.responseWritten {
				break
			}
		}
	} else {
		g.Error(404, nil)
		Log(Debug, "404 serving %s", r.URL.Path)
		return
	}

	Log(Debug, "%v\t%s\t%s %s%s\tfrom %s", time.Now().Sub(now), r.RemoteAddr, r.Method, r.Host, r.URL.Path, r.Header.Get("Referer"))
}
