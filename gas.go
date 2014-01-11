// package gas implements some sort of web framework
package gas

// gas.go contains initialization code and a big pile of things that don't
// belong in or are too small for their own files

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	//flag_syncdb = flag.Bool("gas.syncdb", false, "Create database tables from registered models")
	flag_verbosity = flag.Int("gas.loglevel", 2, "How much information to log (0=none, 1=fatal, 2=warning, 3=notice, 4=debug)")
	flag_log       = flag.String("gas.log", "", "File to log to (log disabled for empty path)")
	sigchan        = make(chan os.Signal, 2)
)

var (
	errNotLoggedIn = errors.New("User is not logged in.")
)

func init() {
	// runtime.GOMAXPROCS(runtime.NumCPU())
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

	signal.Notify(sigchan)
}

type Error struct {
	Path  string
	Err   error
	Stack string
}

// Request context. All incoming requests are boxed into a *Gas and passed to
// handler functions. Comes with embedded standard net/http arguments as well
// as the captured URL variables (if any), and has some convenience methods
// attached.
//
// Gas satisfies the http.ResponseWriter interface.
type Gas struct {
	w http.ResponseWriter
	*http.Request
	args map[string]string
	data map[string]interface{}

	// The HTTP response code that was written to the request. If a response
	// has not be written yet, responseCode will be 0.
	responseCode int

	rerouteInfo []byte

	session *Session
}

func (g *Gas) Arg(key string) string {
	if g.args != nil {
		return g.args[key]
	}
	return ""
}

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

func (g *Gas) Write(p []byte) (n int, err error) {
	if g.responseCode == 0 {
		g.responseCode = 200
	}
	return g.w.Write(p)
}

func (g *Gas) WriteHeader(code int) {
	g.responseCode = code
	g.w.WriteHeader(code)
}

func (g *Gas) ResponseCode() int {
	return g.responseCode
}

func (g *Gas) Header() http.Header {
	return g.w.Header()
}

// Simple wrapper around `http.ServeFile`.
func (g *Gas) ServeFile(path string) {
	http.ServeFile(g, g.Request, path)
}

// Simple wrapper around `http.Redirect`.
func (g *Gas) Redirect(path string, code int) {
	http.Redirect(g, g.Request, path, code)
}

// Perform a redirect, but first place a cookie on the client containing an
// encoding/gob blob encoded from the data passed in. The recieving handler
// should then check for the RerouteInfo on the request, and handle the special
// case if necessary.
func (g *Gas) Reroute(path string, code int, data interface{}) {
	var cookieVal string

	if data != nil {
		buf := new(bytes.Buffer)
		enc := gob.NewEncoder(buf)
		err := enc.Encode(data)

		if err == nil {
			cookieVal = base64.StdEncoding.EncodeToString(buf.Bytes())
		} else {
			Log(Warning, "gas: reroute: %v", err)
		}
	}

	g.SetCookie(&http.Cookie{
		// Make it only visible on the target page (though if it's root it'll
		// be everywhere, whatever)
		Path:  path,
		Name:  "_reroute",
		Value: cookieVal,
	})

	g.Redirect(path, code)
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

// Serve up an error page from /templates/errors. Templates in that directory
// should have the naming scheme `<code>.tmpl`, where code is the numeric HTTP
// status code (such as `404.tmpl`). The provided error is supplied as the
// template context in a gas.Error value.
func (g *Gas) Error(code int, err error) {
	g.WriteHeader(code)
	ctx := Error{
		Path: g.URL.Path,
		Err:  err,
	}
	code_s := strconv.Itoa(code)

	if err != nil {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		ctx.Stack = string(buf[:n])
	}
	g.Render("errors", code_s, ctx)
}

// Simple wrapper around http.SetCookie.
func (g *Gas) SetCookie(cookie *http.Cookie) {
	http.SetCookie(g, cookie)
}

// Simple wrapper around (http.ResponseWriter).Header().Set
func (g *Gas) SetHeader(key, vals string) {
	g.Header().Set(key, vals)
}

// Write the given value as JSON to the client.
func (g *Gas) JSON(val interface{}) error {
	g.Header().Set("Content-Type", "application/json")
	e := json.NewEncoder(g)
	return e.Encode(val)
}

func (g *Gas) Domain() string {
	return strings.SplitN(g.Host, ":", 1)[0]
}

func handle_signals(c chan os.Signal) {
	for {
		if f := signal_funcs[<-c]; f != nil {
			f()
		}
	}
}

func initThings() {
	if DB != nil {
		_, err := DB.Exec("CREATE TABLE IF NOT EXISTS " + sessionTable + " ( id bytea, expires timestamptz, username text )")
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
	if Verbosity > None {
		logChan = make(chan logMessage, 10)
		logNeedRotate = make(chan time.Time)

		go logLines(logChan, logNeedRotate)

		// XXX: arbitrarily chosen time duration, tweak as needed!
		go pollLogfile(5*time.Second, logNeedRotate)
	}

	initThings()

	defer func() {
		if DB != nil {
			DB.Close()
		}
		for _, stmt := range stmtCache {
			stmt.Close()
		}
	}()

	go handle_signals(sigchan)

	Templates = parse_templates("./templates")

	port := ":" + strconv.Itoa(Env.PORT)

	if srv == nil {
		srv = &http.Server{
			Addr:         port,
			Handler:      http.HandlerFunc(dispatch),
			ReadTimeout:  60 * time.Second,
			WriteTimeout: 10 * time.Second,
		}
	} else {
		srv.Addr = port
		srv.Handler = http.HandlerFunc(dispatch)
	}

	now := time.Now().Format("2006-01-02 15:04")
	LogNotice("=== Session: %s =========================", now)

	Log(Fatal, "%v", srv.ListenAndServe())
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
