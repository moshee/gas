// package gas implements some sort of web framework.
package gas

// gas.go contains initialization code and a big pile of things that don't
// belong in or are too small for their own files

import (
	"bytes"
	"crypto/hmac"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/fcgi"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"code.google.com/p/go.crypto/sha3"
)

var (
	//flag_syncdb = flag.Bool("gas.syncdb", false, "Create database tables from registered models")
	flag_verbosity = flag.Int("gas.loglevel", 2, "How much information to log (0=none, 1=fatal, 2=warning, 3=notice, 4=debug)")
	sigchan        = make(chan os.Signal, 2)
)

func init() {
	signal.Notify(sigchan)

	flag.Parse()
	Verbosity = LogLevel(*flag_verbosity)

	if err := EnvConf(&Env, EnvPrefix); err != nil {
		LogFatal("envconf: %v", err)
	}

	if len(Env.CookieAuthKey) > 0 {
		hmacKeys = bytes.Split(Env.CookieAuthKey, []byte{byte(os.PathListSeparator)})
	}
}

// Request context. All incoming requests are boxed into a *Gas and passed to
// handler functions. Comes with embedded standard net/http arguments as well
// as the captured URL variables (if any), and has some convenience methods
// attached.
type Gas struct {
	w http.ResponseWriter
	*http.Request
	args map[string]string
	data map[string]interface{}

	// Whatever data was left behind by the rerouteOutputter
	rerouteInfo []byte

	responseCode int

	// Session cache
	session *Session
}

func (g *Gas) Write(p []byte) (int, error) {
	if g.responseCode == 0 {
		g.responseCode = 200
	}

	return g.w.Write(p)
}

func (g *Gas) WriteHeader(code int) {
	g.responseCode = code
	g.w.WriteHeader(code)
}

func (g *Gas) Header() http.Header {
	return g.w.Header()
}

// Arg returns the URL parameter named by key
func (g *Gas) Arg(key string) string {
	if g.args != nil {
		return g.args[key]
	}
	return ""
}

// IntArg parses the named URL parameter as an int
func (g *Gas) IntArg(key string) (int, error) {
	return strconv.Atoi(g.Arg(key))
}

// attach some arbitrary data to this context that can be accessed further down
// the chain
func (g *Gas) SetData(key string, val interface{}) {
	if g.data == nil {
		g.data = make(map[string]interface{})
	}
	g.data[key] = val
}

// Access the data that an upstream handler might've left behind
func (g *Gas) Data(key string) interface{} {
	if g.data != nil {
		return g.data[key]
	}
	return nil
}

// TODO: deprecate
// Simple wrapper around `http.ServeFile`.
func (g *Gas) ServeFile(path string) {
	http.ServeFile(g, g.Request, path)
}

// Recover the reroute info stored in the cookie and decode it into dest. If
// there is no reroute cookie, an error is returned.
func (g *Gas) Recover(dest interface{}) error {
	if g.rerouteInfo == nil {
		return errors.New("gas: reroute: no reroute cookie found")
	}
	dec := gob.NewDecoder(bytes.NewReader(g.rerouteInfo))
	return dec.Decode(dest)
}

// Error returns an Outputter that will serve up an error page from
// templates/errors. Templates in that directory should be defined under the
// HTTP status code they correspond to, e.g.
//
//     {{ define "404" }} ... {{ end }}
//
// will provide the template for a 404 error. The template will be rendered
// with a *ErrorInfo as the data binding.
func (g *Gas) Error(err error) Outputter {
	return &ErrorInfo{
		Err:   err.Error(),
		Path:  g.URL.Path,
		Host:  g.Host,
		Stack: fmtStack(3, 10).String(),
	}
}

// SetCookie sets a cookie in the response, adding an HMAC digest to the end of
// the value if that's enabled. If value isn't nil, it'll be interpreted as the
// value destined for the cookie, the sum calculated off of it, and whatever
// value the Cookie has already will be replaced.
func (g *Gas) SetCookie(cookie *http.Cookie, value []byte) {
	if value != nil && hmacKeys != nil && len(hmacKeys) > 0 {
		value = hmacSum(value, hmacKeys[0], value)
		cookie.Value = base64.StdEncoding.EncodeToString(value)
	}

	http.SetCookie(g, cookie)
}

// Return the cookie, if it exists. If HMAC is enabled, first check to see if
// the cookie is valid.
func (g *Gas) Cookie(name string) (*http.Cookie, error) {
	cookie, err := g.Request.Cookie(name)
	if err != nil {
		return nil, err
	}

	decodedLen := base64.StdEncoding.DecodedLen(len(cookie.Value))
	if hmacKeys == nil || len(hmacKeys) == 0 || decodedLen < macLength {
		return cookie, nil
	}

	p, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil, err
	}

	var (
		pos = len(p) - macLength
		val = p[:pos]
		sum = p[pos:]
	)

	for _, key := range hmacKeys {
		s := hmacSum(val, key, nil)
		if hmac.Equal(s, sum) {
			cookie.Value = base64.StdEncoding.EncodeToString(val)
			return cookie, nil
		}
	}

	return nil, errBadMac
}

// return the remote address without the port number
func (g *Gas) Domain() string {
	//return g.Host
	for i := len(g.RemoteAddr) - 1; i > 0; i-- {
		ch := g.Host[i]
		if ch >= '0' && ch <= '9' {
			continue
		} else if ch == ':' {
			return g.RemoteAddr[:i]
		} else {
			break
		}
	}
	return g.Host
}

type OutputFunc func(code int, g *Gas)

func (o OutputFunc) Output(code int, g *Gas) {
	o(code, g)
}

// ErrorInfo represents an error that occurred in a particular request handler.
type ErrorInfo struct {
	Err   string
	Path  string
	Host  string
	Stack string
}

func (o *ErrorInfo) Output(code int, g *Gas) {
	s := strconv.Itoa(code)
	(&templateOutputter{templatePath{"errors", s}, nil, o}).Output(code, g)
}

type jsonOutputter struct {
	data interface{}
}

func (o jsonOutputter) Output(code int, g *Gas) {
	h := g.Header()
	if _, foundType := h["Content-Type"]; !foundType {
		h.Set("Content-Type", "application/json; charset=utf-8")
	}
	g.WriteHeader(code)
	json.NewEncoder(g).Encode(o.data)
}

func JSON(data interface{}) Outputter {
	return jsonOutputter{data}
}

type redirectOutputter string

func (o redirectOutputter) Output(code int, g *Gas) {
	http.Redirect(g, g.Request, string(o), code)
}

func Redirect(path string) Outputter {
	return redirectOutputter(path)
}

type rerouteOutputter struct {
	path string
	data interface{}
}

func (o *rerouteOutputter) Output(code int, g *Gas) {
	var cookieVal []byte

	if o.data != nil {
		buf := new(bytes.Buffer)
		enc := gob.NewEncoder(buf)
		err := enc.Encode(o.data)

		// TODO: do we want to ignore an encode error here?
		if err != nil {
			g.Error(err).Output(code, g)
			return
		}

		cookieVal = buf.Bytes()
	}

	g.SetCookie(&http.Cookie{
		Path:     o.path,
		Name:     "_reroute",
		HttpOnly: true,
	}, cookieVal)

	redirectOutputter(o.path).Output(code, g)
}

// Reroute will perform a redirect, but first place a cookie on the client
// containing an encoding/gob blob encoded from the data passed in. The
// recieving handler should then check for the RerouteInfo on the request, and
// handle the special case if necessary.
func Reroute(path string, data interface{}) Outputter {
	return &rerouteOutputter{path, data}
}

func handleSignals(c chan os.Signal) {
	for {
		if f := signal_funcs[<-c]; f != nil {
			f()
		}
	}
}

func initThings() {
	if DB != nil {
		_, err := DB.Exec("CREATE TABLE IF NOT EXISTS " + Env.SessTable +
			" ( id bytea, expires timestamptz, username text )")
		if err != nil {
			LogFatal("%v", err)
		}
	}

	if initFuncs != nil {
		for _, f := range initFuncs {
			f()
		}
	}
}

// Start the server. Should be called after everything else is set up.
func Ignition(srv *http.Server) {
	now := time.Now()
	initThings()

	defer func() {
		for _, stmt := range stmtCache {
			stmt.Close()
		}
		if DB != nil {
			DB.Close()
		}
	}()

	go handleSignals(sigchan)
	parseTemplates(templateDir)
	port := ":" + strconv.Itoa(Env.Port)

	LogDebug("Initialization took %v", time.Now().Sub(now))
	LogNotice("=== Session: %s =========================", now.Format("2006-01-02 13:04"))

	if Env.FastCGI != "" {
		parts := strings.SplitN(Env.FastCGI, ":", 2)
		network, addr := parts[0], parts[1]
		var (
			l   net.Listener
			err error
			s   = fmt.Sprintf("%s:%s", network, addr)
		)

		if strings.HasPrefix(network, "unix") {
			os.Remove(addr)
			l, err = net.ListenUnix(network, &net.UnixAddr{addr, network})
		} else {
			l, err = net.Listen(network, addr+port)
			s += port
		}
		if err != nil {
			LogFatal("gas: fcgi: %v", err)
		}

		LogNotice("FastCGI listening on %s", s)
		LogFatal("FastCGI: %v", fcgi.Serve(l, http.HandlerFunc(dispatch)))
	} else {
		if srv == nil {
			srv = &http.Server{
				ReadTimeout:  60 * time.Second,
				WriteTimeout: 10 * time.Second,
			}
		}

		srv.Addr = port
		srv.Handler = http.HandlerFunc(dispatch)

		LogNotice("Server listening on port %d", Env.Port)
		LogFatal("Server: %v", srv.ListenAndServe())
	}
}

var initFuncs []func()

// Add list of funcs to run before server is launched.
// They are run in the order that they are added.
func Init(funcs ...func()) {
	if initFuncs == nil {
		initFuncs = make([]func(), 0, len(funcs))
	}
	initFuncs = append(initFuncs, funcs...)
}

func hmacSum(plaintext, key, b []byte) []byte {
	mac := hmac.New(sha3.NewKeccak256, key)
	mac.Write(plaintext)
	return mac.Sum(b)
}
