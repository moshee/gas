package gas

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/fcgi"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/pkg/errors"
)

// A Handler can be used as a request handler for a Router.
type Handler func(g *Gas) (code int, o Outputter)

// An Outputter implements a method to return a response back to a request.
type Outputter interface {
	Output(code int, g *Gas)
}

// OutputFunc is an Outputter that is just a function.
type OutputFunc func(code int, g *Gas)

// Output implements the Outputter interface.
func (o OutputFunc) Output(code int, g *Gas) {
	o(code, g)
}

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

// match this route against an incoming url and return args if it matches
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

// Router is the URL router. Attached methods may be chained for easy adding of
// routes.
type Router struct {
	routes []*route

	// these will be executed in order on every request made to this router
	middleware []Handler

	// Server is the HTTP server that the package will attach to and use. If
	// it's nil, an empty *http.Server instance will be used.
	Server *http.Server

	// quit can be used to close the server
	quit chan struct{}
}

// New creates a new router onto which routes may be added.
func New() *Router {
	return &Router{routes: make([]*route, 0), quit: make(chan struct{})}
}

// Use instructs this router to use the given middleware stack. The router's
// existing stack, if any, will be replaced.
func (r *Router) Use(middleware ...Handler) *Router {
	r.middleware = middleware
	return r
}

// UseMore adds more middleware to the current stack
func (r *Router) UseMore(middleware ...Handler) *Router {
	r.middleware = append(r.middleware, middleware...)
	return r
}

// SetServer allows a user to attach a server to the router inline with other
// chained setup method calls.
func (r *Router) SetServer(srv *http.Server) *Router {
	r.Server = srv
	return r
}

// match each route against incoming url and return args
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

// Head adds a route that responds to HEAD requests.
func (r *Router) Head(pattern string, handlers ...Handler) *Router {
	return r.Add(pattern, "HEAD", handlers...)
}

// Get adds a route that responds to GET requests.
func (r *Router) Get(pattern string, handlers ...Handler) *Router {
	return r.Add(pattern, "GET", handlers...).Head(pattern, handlers...)
}

// Post adds a route that responds to POST requests.
func (r *Router) Post(pattern string, handlers ...Handler) *Router {
	return r.Add(pattern, "POST", handlers...)
}

// Put adds a route that responds to PUT requests.
func (r *Router) Put(pattern string, handlers ...Handler) *Router {
	return r.Add(pattern, "PUT", handlers...)
}

// Delete adds a route that responds to DELETE requests.
func (r *Router) Delete(pattern string, handlers ...Handler) *Router {
	return r.Add(pattern, "DELETE", handlers...)
}

// StaticHandler adds a handler that serves static files from a directory
// called "static" in `root` (relative to the working directory). The route
// path is determined by joining `prefix` with "static" (so e.g. register a
// handler "/static" by passing in "/" as the prefix).
//
// If `root` is an empty string and files have been registered in package
// bindata, that will be used instead of the physical filesystem. Otherwise, no
// handlers are added to the router.
func (r *Router) StaticHandler(urlpath string, dir http.FileSystem) *Router {
	fs := http.StripPrefix(urlpath, http.FileServer(dir))
	return r.Get(path.Join(urlpath, "{file}"), func(g *Gas) (int, Outputter) {
		fs.ServeHTTP(g, g.Request)
		return g.Stop()
	})
}

// Quit closes all of the listeners in r and causes Ignition to return. It can
// be used to close the server from another goroutine.
func (r *Router) Quit() {
	close(r.quit)
}

// Continue instructs the request context to advance to the next handler in the
// chain. It is an error to call Continue when no more handlers exist down the
// chain.
func (g *Gas) Continue() (int, Outputter) {
	if g.handlers == nil || len(g.handlers) == 0 {
		return 500, OutputFunc(func(code int, g *Gas) {
			g.WriteHeader(code)
			g.Write([]byte("nil or empty handler chain"))
		})
	}

	handler := g.handlers[0]
	g.handlers = g.handlers[1:]
	return handler(g)
}

// Stop instructs the request context to stop in the handler chain without
// writing a response.
func (g *Gas) Stop() (int, Outputter) {
	return -1, nil
}

// ServeHTTP satisfies the http.Handler interface.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if nuke := recover(); nuke != nil {
			log.Printf("panic: %s %s %s%s: %v", req.RemoteAddr, req.Method, req.Host, req.URL.Path, nuke)

			err, ok := nuke.(error)
			if !ok {
				err = fmt.Errorf("%v", nuke)
			}
			g := &Gas{w: w, Request: req}
			notifyPanic(g, err)
		}
	}()
	defer req.Body.Close()

	g := &Gas{
		w:       w,
		Request: req,
	}

	now := time.Now()

	if values, handlers := r.match(req); handlers != nil {
		g.args = values
		g.handlers = append(r.middleware, handlers...)

		code, outputter := g.Continue()
		if outputter == nil {
			if code > 0 {
				g.WriteHeader(code)
			}
		} else {
			outputter.Output(code, g)
		}
	} else {
		http.NotFound(g, g.Request)
	}

	host, _, _ := net.SplitHostPort(g.Host)

	remote := g.Request.Header.Get("X-Forwarded-For")
	if remote == "" {
		remote, _, _ = net.SplitHostPort(g.RemoteAddr)
	}
	log.Printf("[%s] %15s %8s %7s (%d) %s%s", fmtDuration(time.Since(now)),
		remote, g.Proto, g.Method, g.responseCode, host, g.URL.Path)
}

// TODO: write tests for listen code, including for TLS and all network types

// Ignition starts the server. Should be called after everything else is set up.
func (r *Router) Ignition() error {
	var (
		now = time.Now()
		c   = make(chan error)
	)

	if initFuncs != nil {
		for _, f := range initFuncs {
			f()
		}
	}

	go handleSignals(sigchan)

	log.Printf("Initialization took %v", time.Now().Sub(now))
	log.Printf("=== Session: %s =========================", now.Format("2006-01-02 15:04"))

	if Env.Listen != "" {
		return r.listen(Env.Listen)
	}

	log.Print("GAS_PORT, GAS_TLS_PORT, and GAS_FAST_CGI are deprecated, please use GAS_LISTEN")

	if Env.FastCGI != "" {
		parts := strings.SplitN(Env.FastCGI, ":", 2)

		if len(parts) != 2 {
			return errors.New("invalid GAS_FAST_CGI syntax")
		}

		var (
			port          = ":" + strconv.Itoa(Env.Port)
			network, addr = parts[0], parts[1]
			l             net.Listener
			err           error
			s             = fmt.Sprintf("%s:%s", network, addr)
		)

		if strings.HasPrefix(network, "unix") {
			os.Remove(addr)
			l, err = net.ListenUnix(network, &net.UnixAddr{addr, network})
		} else {
			l, err = net.Listen(network, addr+port)
			s += port
		}
		if err != nil {
			log.Fatalf("gas: fcgi: %v", err)
		}

		log.Printf("FastCGI listening on %s", s)
		go func() {
			select {
			case c <- fcgi.Serve(l, r):
			case <-r.quit:
				l.Close()
			}
		}()
	} else {
		if Env.Port < 0 && Env.TLSPort < 0 {
			log.Fatalf("must have at least one of either GAS_PORT or GAS_TLS_PORT set")
		}
		if r.Server == nil {
			r.Server = &http.Server{
			//ReadTimeout:  60 * time.Second,
			//WriteTimeout: 10 * time.Second,
			}
		}

		r.Server.Handler = r

		if Env.TLSPort > 0 {
			go listenTLS(r.Server, c, r.quit)
		}

		if Env.Port > 0 {
			go listen(r.Server, c, r.quit)
		}
	}

	select {
	case err := <-c:
		return err
	case <-r.quit:
		return nil
	}
}

func (r *Router) listen(listenenv string) error {
	var (
		addrs   = strings.Split(listenenv, ",")
		ll      = make([]net.Listener, len(addrs))
		cfg     *tls.Config
		srv     *http.Server
		errchan chan error
	)

	if r.Server != nil {
		srv = r.Server
	} else {
		srv = &http.Server{}
	}

	srv.Handler = r

	for i, addr := range addrs {
		var (
			optTLS  bool
			network string
			laddr   string
		)

		addropt := strings.SplitN(addr, ";", 2)
		if len(addropt) == 2 {
			switch addropt[1] {
			case "tls":
				optTLS = true
			default:
				return errors.Errorf("GAS_LISTEN: invalid option: %q", addropt[1])
			}
		}

		netaddr := strings.SplitN(strings.TrimSpace(addr), "!", 2)
		switch len(netaddr) {
		case 1:
			network = "tcp"
			laddr = netaddr[0]
		case 2:
			network, laddr = netaddr[0], netaddr[1]
		default:
			return errors.Errorf("GAS_LISTEN: invalid listen syntax: %q", listenenv)
		}

		l, err := net.Listen(network, laddr)
		if err != nil {
			return err
		}

		if optTLS {
			if cfg == nil {
				cfg, err = tlsConfig(Env.TLSCert, Env.TLSKey, Env.TLSHost)
				if err != nil {
					return err
				}
			}
			l = tls.NewListener(l, cfg)
		}

		ll[i] = l
	}

	for _, l := range ll {
		go func(l net.Listener) {
			log.Printf("serving on %v", l.Addr())
			err := srv.Serve(l)
			log.Printf("%v: %v", l.Addr(), err)
			select {
			case errchan <- err:
			default:
			}
		}(l)
	}

	var (
		err error
		sig = make(chan os.Signal)
		ctx = context.Background()
	)

	signal.Notify(sig, os.Interrupt)

	select {
	case err = <-errchan:
	case <-sig:
		// TODO: attempt graceful shutdown upon first ^C, force on second
	case <-r.quit:
	}

	srv.Shutdown(ctx)
	return err
}

func fmtDuration(d time.Duration) string {
	switch {
	case d <= time.Microsecond:
		return fmt.Sprintf("% 4dns", d)
	case d <= time.Millisecond:
		return fmt.Sprintf("% 4dµs", d/time.Microsecond)
	case d <= time.Second:
		return fmt.Sprintf("% 4dms", d/time.Millisecond)
	case d <= time.Minute:
		return fmt.Sprintf("% 2.2fs", float64(d)/float64(time.Second))
	default:
		return fmt.Sprintf("% 6s", d.String())
	}
}

// number of lines of context to show around panicking code
const amountOfContext = 5

// format the current goroutine's stack nicely, optionally returning the lines
// of code around and including the panicking line
func fmtStack(skip, count int, showSource bool) (source []string, actualLine int, panickingFile string, stack *bytes.Buffer) {
	stack = new(bytes.Buffer)
	pcs := make([]uintptr, count)
	s := runtime.Callers(skip, pcs)
	pcs = pcs[:s]
	tw := tabwriter.NewWriter(stack, 4, 8, 1, ' ', 0)

	for i, pc := range pcs {
		f := runtime.FuncForPC(pc)
		path, line := f.FileLine(pc)
		name := f.Name()
		oneUp := filepath.Base(filepath.Dir(path))
		file := filepath.Join(oneUp, filepath.Base(path))

		fmt.Fprintf(tw, "%2d: %s\t(%s:%d)\n", i, name, file, line)

		// if we are returning source code context, check each successive function and
		// skip runtime/panic ones. The line of code we are looking for is probably
		// not in the runtime.
		if showSource && !strings.HasPrefix(name, "runtime.") {
			// then disable the flag so we don't keep searching
			showSource = false
			panickingFile = file
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			data, err := ioutil.ReadAll(f)
			if err != nil {
				continue
			}
			lines := bytes.Split(data, []byte{'\n'})
			if line > len(lines) {
				continue
			}

			actualLine = amountOfContext
			lower := line - 1 - amountOfContext
			if lower < 0 {
				actualLine += lower
				lower = 0
			}
			upper := line - 1 + amountOfContext
			if upper >= len(lines) {
				upper = len(lines) - 1
			}

			source = make([]string, 0, upper-lower)
			magnitude := int(math.Floor(math.Log10(float64(upper)))) + 1
			for l := lower; l <= upper; l++ {
				line := strings.Replace(string(lines[l]), "\t", "    ", -1)
				source = append(source, fmt.Sprintf("%*d  %s", magnitude, l+1, line))
			}
		}
	}
	tw.Flush()

	return
}

func printStack(skip, count int) {
	_, _, _, buf := fmtStack(skip+1, count, false)
	io.Copy(os.Stderr, buf)
}

func notifyPanic(g *Gas, err error) {
	// here we skip 5 because we know the last calls are guaranteed:
	//     0 runtime.panic
	//     1 func·NNN (the deferred recover)
	//     2 runtime.Callers
	//     3 fmtStack
	//     4 notifyPanic
	// that way we can get right to the source of it with less noise
	source, lineNum, file, stack := fmtStack(5, 10, true)

	// don't write header if panic happened in outputter
	if g.w.Header().Get("Content-Type") == "" {
		g.w.Header().Set("Content-Type", "text/html; encoding=utf-8")
		g.w.WriteHeader(500)
	}

	tmplErr := panicTemplate.Execute(g.w, &struct {
		Err    error
		Stack  string
		File   string
		Source []string
		Line   int
	}{err, stack.String(), file, source, lineNum})

	if tmplErr != nil {
		fmt.Fprintln(g, "Error writing panic:", tmplErr)
	}
}

var panicTemplate = template.Must(template.New("panic").Parse(`
<!DOCTYPE html>
<html>
	<head>
		<title>Panic!</title>
		<style>
			body {
				width: 100%;
				max-width: 800px;
				margin: 50px auto;
				font-family: sans-serif;
			}
			h1, h2 {
				text-align: center;
			}
			h2 {
				font-size: 18px;
			}
			pre, code {
				font-size: 13px;
				font-family: menlo, dejavu sans mono, bitstream vera sans mono, monospace;
			}
			.gray {
				color: #888;
			}
		</style>
	</head>
	<body>
		<h1>The server is panicking!</h1>
		{{ with .Err }}<h2><span class="gray">Error:</span> {{ . }}</h2>{{ end }}
		<p>Details:</p>
		<pre>{{ .Stack }}</pre>
		<p>The offending code in <code>{{ .File }}</code>:</p>
		<pre>{{ range $i, $v := .Source }}{{ if eq $i $.Line }}<strong>! {{ $v }}</strong>{{ else }}<span class="gray">  {{ $v }}</span>{{ end }}
{{ end }}</pre>
	</body>
</html>`))
