package gas

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
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

	var (
		logfile *os.File
		err     error
	)

	if *flag_log != "" {
		logfile, err = os.OpenFile(*flag_log, os.O_TRUNC|os.O_CREATE, 0)
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}
	} else {
		logfile = os.Stdout
	}
	logger = log.New(logfile, "", log.LstdFlags)

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

type LogLevel int

const (
	None LogLevel = iota
	Fatal
	Warning
	Notice
	Debug
)

var (
	Verbosity LogLevel = None
	logger    *log.Logger
)

func (l LogLevel) String() string {
	switch l {
	case Fatal:
		return "FATAL: "
	case Warning:
		return "Warning: "
	}
	return ""
}

func Log(level LogLevel, format string, args ...interface{}) {
	if logger == nil {
		return
	}

	if Verbosity >= level {
		logger.Printf(level.String()+format, args...)
		if Verbosity == Fatal {
			debug.PrintStack()
			os.Exit(1)
		}
	}
}

// Request context. All incoming requests are boxed into a *Gas and passed to
// handler functions. Comes with embedded standard net/http arguments as well
// as the captured URL variables (if any), and has some convenience methods
// attached.
type Gas struct {
	http.ResponseWriter
	*http.Request
	args            map[string]string
	data            map[string]interface{}
	responseWritten bool

	*RerouteInfo

	// user is the user object used for user authorization, etc. It will be
	// populated automatically upon a call to SignIn(), if successful.
	// Otherwise, it will be populated upon a call to Allowed()—again, if
	// successful.
	user User
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

// Write a response to the underlying http.ResponseWriter, flagging the request
// as complete. If the request is complete, it will not travel further down the
// handler chain. One handler may call Write multiple times, but downstream
// handlers will not be evaluated.
func (g *Gas) Write(p []byte) (n int, err error) {
	g.responseWritten = true
	return g.ResponseWriter.Write(p)
}

// Simple wrapper around `http.ServeFile`.
func (g *Gas) ServeFile(path string) {
	http.ServeFile(g.ResponseWriter, g.Request, path)
}

// Simple wrapper around `http.Redirect`.
func (g *Gas) Redirect(path string, code int) {
	http.Redirect(g.ResponseWriter, g.Request, path, code)
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
	http.SetCookie(g.ResponseWriter, cookie)
}

// Simple wrapper around (http.ResponseWriter).Header().Set
func (g *Gas) SetHeader(key, vals string) {
	g.ResponseWriter.Header().Set(key, vals)
}

// Write the given value as JSON to the client.
func (g *Gas) JSON(val interface{}) error {
	g.ResponseWriter.Header().Set("Content-Type", "application/json")
	e := json.NewEncoder(g.ResponseWriter)
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
	http.HandleFunc("/", dispatch)

	srv := &http.Server{
		Addr:        port_string,
		ReadTimeout: 30 * time.Second,
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
