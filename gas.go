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
)

var (
	//flag_syncdb = flag.Bool("gas.syncdb", false, "Create database tables from registered models")
	flag_verbosity = flag.Int("gas.loglevel", 0, "How much information to log (0=none, 1=fatal, 2=warning, 3=notice, 4=debug)")
	flag_port      = flag.Int("gas.port", 80, "Port to listen on")
	sigchan        = make(chan os.Signal, 2)
)

func init() {
	flag.Parse()
	Verbosity = LogLevel(*flag_verbosity)

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

var Verbosity LogLevel = None

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
	if Verbosity >= level {
		log.Printf(level.String()+format, args...)
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
	go handle_signals(sigchan)

	if UsersTable != "" {
		if _, err := DB.Exec("CREATE TABLE IF NOT EXISTS " + UsersTable + " ( id serial PRIMARY KEY, name text, pass bytea, salt bytea )"); err != nil {
			Log(Fatal, "Couldn't create users table")
		}
	}
	port_string := ":" + strconv.Itoa(*flag_port)
	http.HandleFunc("/", dispatch)
	println("let's go!")
	Log(Fatal, "%v", http.ListenAndServe(port_string, nil))
}
