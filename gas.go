// package gas implements some sort of web framework
package gas

// gas.go contains initialization code and a big pile of things that don't
// belong in or are too small for their own files

import (
	"encoding/base64"
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
	flag_verbosity     = flag.Int("gas.loglevel", 2, "How much information to log (0=none, 1=fatal, 2=warning, 3=notice, 4=debug)")
	flag_port          = flag.Int("gas.port", 80, "Port to listen on")
	flag_log           = flag.String("gas.log", "", "File to log to (log disabled for empty path)")
	flag_daemon        = flag.Bool("gas.daemon", false, "Internal use")
	flag_db_idle_conns = flag.Int("gas.db.conns.idle", 10, "Maximum number of idle DB connections")
	flag_db_conns      = flag.Int("gas.db.conns.open", 4, "Maximum number of open DB connections")
	sigchan            = make(chan os.Signal, 2)
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

// A RerouteInfo object stores the data passed on from a page rerouting its
// requester to a different handler. If the RerouteInfo field of *Gas is not
// nil, that means a reroute was requested. The reroute cookies will always be
// removed at the beginning of every request, if they exist.
type RerouteInfo struct {
	From string
	Val  interface{}
	raw  []byte
}

// Recover the original data left behind on the call to Reroute
func (self *RerouteInfo) Recover(val interface{}) error {
	self.Val = val
	return json.Unmarshal(self.raw, self)
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

	*RerouteInfo

	// user is the user object used for user authorization, etc. It will be
	// populated automatically upon a call to SignIn(), if successful.
	// Otherwise, it will be populated upon a call to Allowed()—again, if
	// successful.
	user user
}

func (g *Gas) Allowed(privileges interface{}) (bool, error) {
	if g.User() == nil {
		return false, errNotLoggedIn
	}
	return g.user.Allowed(privileges), nil
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
// arbitrary JSON blob encoded from the data passed in. The recieving handler
// should then check for the RerouteInfo on the request, and handle the special
// case if necessary.
func (g *Gas) Reroute(path string, code int, data interface{}) {
	var cookieVal string

	if data != nil {
		reroute := &RerouteInfo{
			From: g.URL.Path,
			Val:  data,
		}

		val, err := json.Marshal(reroute)
		if err == nil {
			cookieVal = base64.StdEncoding.EncodeToString(val)
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

func do_subcommands() bool {
	// TODO subcommands
	args := flag.Args()
	if len(args) > 0 {
		switch args[0] {
		case "makeuser":
			if len(args) != 3 {
				// TODO: error types
				Log(Fatal, "Wrong number of arguments for %s: %d for %d", args[0], len(args), 3)
			}
			if err := NewUser(args[1], args[2]); err != nil {
				Log(Fatal, "%s: %v", args[0], err)
			}
			Log(Notice, "User '%s' created.", args[1])
			return true
		}
	}
	return false
}

func handle_signals(c chan os.Signal) {
	for {
		if f := signal_funcs[<-c]; f != nil {
			f()
		}
	}
}

// Run the provided subcommand, if any. If no subcommand given, start the
// server running on the given port. This should be the last call in the main()
// function.
func Ignition() {
	if Verbosity > None {
		logChan = make(chan logMessage, 10)
		logNeedRotate = make(chan time.Time)

		go logLines(logChan, logNeedRotate)

		// XXX: arbitrarily chosen time duration, tweak as needed!
		go pollLogfile(5*time.Second, logNeedRotate)
	}

	now := time.Now().Format("2006-01-02 15:04")

	LogNotice("=== Session: %s =========================", now)

	if DB != nil {
		defer DB.Close()
	}

	if do_subcommands() {
		return
	}

	if initFuncs != nil {
		for _, f := range initFuncs {
			f()
		}
	}

	go handle_signals(sigchan)

	Templates = parse_templates("./templates")

	// TODO: move all this first-run shit to a new thing
	/*
		if UsersTable != "" {
			if _, err := DB.Exec("CREATE TABLE IF NOT EXISTS " + UsersTable + " ( id serial PRIMARY KEY, name text, pass bytea, salt bytea )"); err != nil {
				Log(Fatal, "Couldn't create users table")
			}
		}
	*/
	port_string := ":" + strconv.Itoa(*flag_port)

	srv := &http.Server{
		Addr:         port_string,
		Handler:      http.HandlerFunc(dispatch),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
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

// TODO: this
type Subcommand struct {
	Name string
	Do   func() error
}

// Unimplemented
func RegisterSubcommand(commands ...*Subcommand) {

}
