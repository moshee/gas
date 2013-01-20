package gas

import (
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"encoding/json"
	"flag"
)

var (
//flag_syncdb = flag.Bool("gas.syncdb", false, "Create database tables from registered models")
	flag_verbosity = flag.Int("gas.loglevel", 0, "How much information to log (0=none, 1=fatal, 2=warning, 3=notice, 4=debug)")
)

func init() {
	flag.Parse()
	Verbosity = LogLevel(*flag_verbosity)
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

// Request context comes with embedded standard net/http arguments as well as the captured URL variables (if any), and has some convenience methods attached.
type Gas struct {
	http.ResponseWriter
	*http.Request
	Args map[string]string
}

// Simple wrapper around http.ServeFile
func (g *Gas) ServeFile(path string) {
	http.ServeFile(g.ResponseWriter, g.Request, path)
}

// Simple wrapper around http.Redirect
func (g *Gas) Redirect(path string, code int) {
	http.Redirect(g.ResponseWriter, g.Request, path, code)
}

func (g *Gas) Error(code int, err error) {
	g.WriteHeader(code)
	code_s := strconv.Itoa(code)
	g.Render("errors", code_s, Error{g.URL.Path, err})
}

func (g *Gas) SetCookie(cookie *http.Cookie) {
	http.SetCookie(g.ResponseWriter, cookie)
}

func (g *Gas) JSON(val interface{}) error {
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	g.ResponseWriter.Header().Set("Content-Type", "application/json")
	_, err = g.ResponseWriter.Write(data)
	return err
}

func Ignition(port int) {
	defer DB.Close()

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
			return
		}
	}

	if UsersTable != "" {
		if _, err := DB.Exec("CREATE TABLE IF NOT EXISTS " + UsersTable + " ( id serial PRIMARY KEY, name text, pass bytea, salt bytea )"); err != nil {
			log.Fatalln("Couldn't create users table")
		}
	}
	/*
		if *flag_syncdb {
			log.Println("Syncing database tables with registered models...")
			for _, model := range models {
				if err := model.Create(); err != nil {
					log.Fatalln(err)
				}
			}
			log.Println("Done.")
		}
	*/
	// TODO: handle SIGHUP/SIGINT/SIGSTOP/etc
	port_string := ":" + strconv.Itoa(port)
	http.HandleFunc("/", dispatch)
	println("let's go!")
	Log(Fatal, "%v", http.ListenAndServe(port_string, nil))
}
