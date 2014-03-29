// Package gas implements some sort of web framework.
package gas

// gas.go contains initialization code and a big pile of things that don't
// belong in or are too small for their own files

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"unicode"
)

var (
	sigchan = make(chan os.Signal, 2)
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	signal.Notify(sigchan)

	if err := EnvConf(&Env, EnvPrefix); err != nil {
		log.Fatalf("envconf: %v", err)
	}
}

// The Gas structure is the request context. All incoming requests are boxed
// into a *Gas and passed to handler functions. Comes with embedded standard
// net/http arguments as well as the captured URL variables (if any), and has
// some convenience methods attached.
type Gas struct {
	w http.ResponseWriter
	*http.Request
	args map[string]string      // named url args
	data map[string]interface{} // arbitrary data

	responseCode int // the response code that will be/has been written

	// the handler chain, first element is always the next one to execute (not
	// guaranteed to be nonzero length)
	handlers []Handler
}

func (g *Gas) Write(p []byte) (int, error) {
	if g.responseCode == 0 {
		g.responseCode = 200
	}

	return g.w.Write(p)
}

// WriteHeader and Header implement the http.ResponseWriter interface.
func (g *Gas) WriteHeader(code int) {
	g.responseCode = code
	g.w.WriteHeader(code)
}

// Header and WriteHeader implement the http.ResponseWriter interface.
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

// SetData allows a handler to attach some arbitrary data to this context that
// can be accessed further down the chain
func (g *Gas) SetData(key string, val interface{}) {
	if g.data == nil {
		g.data = make(map[string]interface{})
	}
	g.data[key] = val
}

// Data allows a handler to access the data that an upstream handler might've
// left behind
func (g *Gas) Data(key string) interface{} {
	if g.data != nil {
		return g.data[key]
	}
	return nil
}

// SetCookie sets a cookie in the response, adding an HMAC digest to the end of
// the value if that's enabled. If value isn't nil, it'll be interpreted as the
// value destined for the cookie, the sum calculated off of it, and whatever
// value the Cookie has already will be replaced.
func (g *Gas) SetCookie(cookie *http.Cookie) {
	http.SetCookie(g, cookie)
}

// Hook registers a func to run whenever the specified signal is recieved. If
// multiple funcs are registered under the same signal, they will be executed
// in the order they were added.
//
// Hook is not safe for concurrent calling.
func Hook(sig os.Signal, f func()) {
	sigs := signalFuncs[sig]
	if sigs == nil {
		sigs = make([]func(), 0, 1)
	}
	signalFuncs[sig] = append(sigs, f)
}

func handleSignals(c chan os.Signal) {
	for {
		if funcs := signalFuncs[<-c]; funcs != nil {
			for _, f := range funcs {
				f()
			}
		}
	}
}

func listenTLS(srv *http.Server) error {
	cfg := &tls.Config{}
	if srv.TLSConfig != nil {
		*cfg = *srv.TLSConfig
	} else {
		cert, err := tls.LoadX509KeyPair(Env.TLSCert, Env.TLSKey)
		if err != nil {
			return err
		}
		cfg.Certificates = []tls.Certificate{cert}
		cfg.ServerName = Env.TLSHost
		cfg.BuildNameToCertificate()
	}

	if cfg.NextProtos == nil {
		cfg.NextProtos = []string{"http/1.1"}
	}

	l, err := net.Listen("tcp", ":"+strconv.Itoa(Env.TLSPort))
	if err != nil {
		return err
	}

	t := tls.NewListener(l, cfg)
	log.Printf("Server listening on port %d (TLS)", Env.TLSPort)
	return srv.Serve(t)
}

func listen(srv *http.Server) error {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(Env.Port))
	if err != nil {
		return err
	}
	log.Printf("Server listening on port %d", Env.Port)
	return srv.Serve(l)
}

var initFuncs []func()

// Init adds a func to the list of funcs to run before server is launched.
// They are run in the order that they are added.
func Init(funcs ...func()) {
	if initFuncs == nil {
		initFuncs = make([]func(), 0, len(funcs))
	}
	initFuncs = append(initFuncs, funcs...)
}

// ToSnake is a utility function that converts from camelCase to snake_case.
func ToSnake(in string) string {
	if len(in) == 0 {
		return ""
	}

	out := make([]rune, 0, len(in))
	foundUpper := false
	r := []rune(in)

	for i := 0; i < len(r); i++ {
		ch := r[i]
		if unicode.IsUpper(ch) {
			if i > 0 && i < len(in)-1 && !unicode.IsUpper(r[i+1]) {
				out = append(out, '_', unicode.ToLower(ch), r[i+1])
				i++
				continue
				foundUpper = false
			}
			if i > 0 && !foundUpper {
				out = append(out, '_')
			}
			out = append(out, unicode.ToLower(ch))
			foundUpper = true
		} else {
			foundUpper = false
			out = append(out, ch)
		}
	}
	return string(out)
}

var exitQueue = make([]func(), 0)

// AddDestructor adds a func to the exit queue to be run when the server closes.
func AddDestructor(f func()) {
	exitQueue = append(exitQueue, f)
}

func exit(code int) {
	for _, f := range exitQueue {
		f()
	}
	os.Exit(code)
}
