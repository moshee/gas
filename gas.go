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
	"log"
	"net"
	"net/http"
	"net/http/fcgi"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"code.google.com/p/go.crypto/sha3"
)

var (
	//flag_syncdb = flag.Bool("gas.syncdb", false, "Create database tables from registered models")
	flag_verbosity = flag.Int("gas.loglevel", 2, "How much information to log (0=none, 1=fatal, 2=warning, 3=notice, 4=debug)")
	flag_log       = flag.String("gas.log", "", "File to log to (log disabled for empty path)")
	sigchan        = make(chan os.Signal, 2)
	fcgiListener   net.Listener
)

var (
	errNotLoggedIn = errors.New("User is not logged in.")
)

func init() {
	signal.Notify(sigchan)

	flag.Parse()
	Verbosity = LogLevel(*flag_verbosity)

	var err error

	if *flag_log != "" {
		logFile, err = os.OpenFile(*flag_log, os.O_CREATE|os.O_APPEND|os.O_RDWR, os.FileMode(0644))
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}
		logFilePath, err = filepath.Abs(*flag_log)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		logFile = os.Stdout
	}
	logger = log.New(logFile, "", log.LstdFlags)

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

type OutputFunc func(code int, g *Gas)

func (o OutputFunc) Output(code int, g *Gas) {
	o(code, g)
}

type ErrorInfo struct {
	Err   string
	Path  string
	Host  string
	Stack string
}

type errorOutputter struct {
	error
}

func (o errorOutputter) Output(code int, g *Gas) {
	s := strconv.Itoa(code)
	err := &ErrorInfo{
		Err:   o.error.Error(),
		Path:  g.URL.Path,
		Host:  g.Host,
		Stack: fmtStack(3, 10).String(),
	}
	(&templateOutputter{"errors", s, err}).Output(code, g)
}

// Error returns an Outputter that will serve up an error page from
// /templates/errors. Templates in that directory should have the naming scheme
// `<code>.tmpl`, where <code> is the numeric HTTP status code (such as
// `404.tmpl`). The provided error is supplied as the template context in a
// gas.ErrorInfo value.
func Error(err error) Outputter {
	return errorOutputter{err}
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

func (g *Gas) Domain() string {
	return strings.SplitN(g.Host, ":", 2)[0]
}

type jsonOutputter struct {
	data interface{}
}

func JSON(data interface{}) Outputter {
	return jsonOutputter{data}
}

func (o jsonOutputter) Output(code int, g *Gas) {
	h := g.Header()
	if _, foundType := h["Content-Type"]; !foundType {
		h.Set("Content-Type", "application/json; charset=utf-8")
	}
	g.WriteHeader(code)
	json.NewEncoder(g).Encode(o.data)
}

type redirectOutputter string

func Redirect(path string) Outputter {
	return redirectOutputter(path)
}

func (o redirectOutputter) Output(code int, g *Gas) {
	http.Redirect(g, g.Request, string(o), code)
}

type rerouteOutputter struct {
	path string
	data interface{}
}

// Reroute will perform a redirect, but first place a cookie on the client
// containing an encoding/gob blob encoded from the data passed in. The
// recieving handler should then check for the RerouteInfo on the request, and
// handle the special case if necessary.
func Reroute(path string, data interface{}) Outputter {
	return &rerouteOutputter{path, data}
}

func (o *rerouteOutputter) Output(code int, g *Gas) {
	var cookieVal []byte

	if o.data != nil {
		buf := new(bytes.Buffer)
		enc := gob.NewEncoder(buf)
		err := enc.Encode(o.data)

		// TODO: do we want to ignore an encode error here?
		if err != nil {
			Error(err).Output(code, g)
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

func handle_signals(c chan os.Signal) {
	for {
		if f := signal_funcs[<-c]; f != nil {
			f()
		}
	}
}

// Call before Ignition with a net.Listener to use FastCGI instead of the
// regular server
func UseFastCGI(l net.Listener) {
	fcgiListener = l
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
	initThings()

	defer func() {
		for _, stmt := range stmtCache {
			stmt.Close()
		}
		if DB != nil {
			DB.Close()
		}
	}()

	go handle_signals(sigchan)

	Templates = parse_templates("./templates")

	now := time.Now().Format("2006-01-02 15:04")
	LogNotice("=== Session: %s =========================", now)

	if fcgiListener != nil {
		LogFatal("FastCGI: %v", fcgi.Serve(fcgiListener, http.HandlerFunc(dispatch)))
	} else {
		port := ":" + strconv.Itoa(Env.Port)

		if srv == nil {
			srv = &http.Server{
				ReadTimeout:  60 * time.Second,
				WriteTimeout: 10 * time.Second,
			}
		}

		srv.Addr = port
		srv.Handler = http.HandlerFunc(dispatch)
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
