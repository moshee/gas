package gas

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"time"
)

var (
	//flag_syncdb = flag.Bool("gas.syncdb", false, "Create database tables from registered models")
	flag_verbosity = flag.Int("gas.loglevel", 2, "How much information to log (0=none, 1=fatal, 2=warning, 3=notice, 4=debug)")
	flag_port      = flag.Int("gas.port", 80, "Port to listen on")
	flag_log       = flag.String("gas.log", "", "File to log to (log disabled for empty path)")
	flag_daemon    = flag.Bool("gas.daemon", false, "Internal use")
	sigchan        = make(chan os.Signal, 2)
)

func init() {
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
	logger = log.New(logfile, "", log.LstdFlags|log.Lshortfile)

	signal.Notify(sigchan)
}

type Error struct {
	Path string
	Err  error
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
	Args map[string]string
}

// Simple wrapper around `http.ServeFile`.
func (g *Gas) ServeFile(path string) {
	http.ServeFile(g.ResponseWriter, g.Request, path)
}

// Simple wrapper around `http.Redirect`.
func (g *Gas) Redirect(path string, code int) {
	http.Redirect(g.ResponseWriter, g.Request, path, code)
}

// Serve up an error page from /templates/errors. Templates in that directory
// should have the naming scheme `<code>.tmpl`, where code is the numeric HTTP
// status code (such as `404.tmpl`). The provided error is supplied as the
// template context.
func (g *Gas) Error(code int, err error) {
	g.WriteHeader(code)
	if err != nil {
		code_s := strconv.Itoa(code)
		g.Render("errors", code_s, Error{g.URL.Path, err})
	}
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
	defer DB.Close()

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

func RegisterSubcommand(commands ...*Subcommand) {

}
