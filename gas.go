// Package gas implements some sort of web framework.
package gas

// gas.go contains initialization code and a big pile of things that don't
// belong in or are too small for their own files

import (
	"crypto/tls"
	"encoding"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
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

// SetFilename adds a Content-Disposition header to the response instructing
// the browser to use the given filename for the resource.
func (g *Gas) SetFilename(filename string) {
	encoded := strings.Replace(url.QueryEscape(filename), "+", "%20", -1)
	disposition := fmt.Sprintf("filename*=UTF-8''%s; filename=%s", encoded, encoded)
	g.Header().Add("Content-Disposition", disposition)
}

// SetCookie sets a cookie in the response, adding an HMAC digest to the end of
// the value if that's enabled. If value isn't nil, it'll be interpreted as the
// value destined for the cookie, the sum calculated off of it, and whatever
// value the Cookie has already will be replaced.
func (g *Gas) SetCookie(cookie *http.Cookie) {
	http.SetCookie(g, cookie)
}

// AcceptHeader is an accepted media type with associated q-value.
type AcceptHeader struct {
	Type string
	Q    float32
}

// AcceptList is a slice of AcceptHeader that can be sorted by descending
// q-value using package sort.
type AcceptList []AcceptHeader

func (a AcceptList) Len() int           { return len(a) }
func (a AcceptList) Less(i, j int) bool { return a[i].Q > a[j].Q }
func (a AcceptList) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

// ParseAcceptHeader parses and sort a list of accepted media types as appears
// in the client's Accept header. If an error is encountered, ParseAcceptHeader
// will do the best it can with the rest and return the first error
// encountered.
func ParseAcceptHeader(h string) (accepts AcceptList, e error) {
	types := strings.Split(h, ",")
	accepts = make(AcceptList, 0, len(types))

	for _, t := range types {
		mediaType, params, err := mime.ParseMediaType(t)
		if err != nil {
			if e == nil {
				e = fmt.Errorf("ParseAcceptHeader: %v", err)
			}
			continue
		}
		a := AcceptHeader{Type: mediaType}
		if q, ok := params["q"]; ok {
			qval, err := strconv.ParseFloat(q, 32)
			if err != nil {
				if e == nil {
					e = fmt.Errorf("ParseAcceptHeader: %v", err)
				}
				continue
			}
			a.Q = float32(qval)
		} else {
			a.Q = 1.0
		}
		accepts = append(accepts, a)
	}

	sort.Stable(accepts)
	return
}

// Wants tries to determine what RFC 1521 media type the client wants in
// return. If it can't decide, defaults to text/html. Returned media types will
// be normalized and have any parameters stripped.
func (g *Gas) Wants() string {
	accept := g.Request.Header.Get("Accept")
	if accept == "" {
		v := mime.TypeByExtension(path.Ext(g.URL.Path))
		if v == "" {
			return "text/html"
		}
		t, _, _ := mime.ParseMediaType(v)
		return t
	}

	a, err := ParseAcceptHeader(accept)
	if err != nil {
		log.Print(err)
	}
	return a[0].Type
}

// UA is a user agent.
type UA struct {
	Name    string
	Version string
	Comment string
}

// ParseUserAgents splits a User-Agent: header value into a list of individual
// user agents.
func ParseUserAgents(ua string) (list []UA) {
	if ua == "" {
		return
	}

	var (
		fields      = strings.Fields(ua)
		start       int
		commentNest int
	)

	for i, field := range fields {
		if field == "" {
			continue
		}
		if strings.HasPrefix(field, "(") {
			if commentNest == 0 {
				start = i
			}
			commentNest++
		} else if strings.HasSuffix(field, ")") {
			commentNest--
			if commentNest < 0 {
				commentNest = 0
				continue
			}
			if commentNest == 0 {
				commentstr := strings.Join(fields[start:i+1], " ")
				commentstr = commentstr[1 : len(commentstr)-1]
				if len(list) > 0 {
					list[len(list)-1].Comment = commentstr
				}
				continue
			}
		}
		if commentNest == 0 {
			nameversion := strings.SplitN(field, "/", 2)
			if len(nameversion) != 2 && len(list) > 0 {
				list[len(list)-1].Version += " " + field
			} else {
				list = append(list, UA{Name: nameversion[0], Version: nameversion[1]})
			}
		}
	}

	return
}

// UserAgents returns a slice of the user agents listed in the request's
// User-Agent header, in the format defined by RFC 2616. If no User-Agent
// header is present, an empty slice is returned.
func (g *Gas) UserAgents() []UA {
	return ParseUserAgents(g.Request.Header.Get("User-Agent"))
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

func tlsConfig(certPath, keyPath, hostName string) (*tls.Config, error) {
	cfg := &tls.Config{}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	cfg.Certificates = []tls.Certificate{cert}
	cfg.ServerName = hostName
	cfg.BuildNameToCertificate()

	if cfg.NextProtos == nil {
		cfg.NextProtos = []string{"h2", "http/1.1"}
	}

	return cfg, nil
}

func listenTLS(srv *http.Server, c chan error, quit chan struct{}) {
	var (
		cfg *tls.Config
		err error
	)

	if srv.TLSConfig == nil {
		cfg, err = tlsConfig(Env.TLSCert, Env.TLSKey, Env.TLSHost)
		if err != nil {
			c <- err
		}
		srv.TLSConfig = cfg
	} else {
		cfg = srv.TLSConfig
	}

	l, err := net.Listen("tcp", ":"+strconv.Itoa(Env.TLSPort))
	if err != nil {
		c <- err
	}

	t := tls.NewListener(l, cfg)
	log.Printf("Server listening on port %d (TLS)", Env.TLSPort)

	select {
	case c <- srv.Serve(t):
	case <-quit:
		l.Close()
	}
}

func listen(srv *http.Server, c chan error, quit chan struct{}) {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(Env.Port))
	if err != nil {
		c <- err
	}
	log.Printf("Server listening on port %d", Env.Port)

	select {
	case c <- srv.Serve(l):
	case <-quit:
		l.Close()
	}
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

var (
	errNotStructPointer = errors.New("UnmarshalForm: dst must be a pointer to a struct value")
	errUnsupportedKind  = "UnmarshalForm: cannot unmarshal form value into field '%s' of type %T"
)

// UnmarshalForm pulls values from a request's form (multipart or query string)
// and places them into a struct, like encoding/json. It honors
// encoding.TextUnmarshaler, but the part about copying the bytes is
// irrelevant.
//
// A "form" struct tag can be used to refer to any named field in the form for
// a given struct field.
//
// If the field is a time.Time, it will try to parse it as a UNIX timestamp
// unless a "timeFormat" tag is present, in which case it will parse the time
// using that. If the field is a numeric type, an empty string as the field
// value will become a zero value. If you wish to customize this behavior,
// either specify the field as a string and parse it yourself, or make a type
// that satisfies TextUnmarshaler.
func (g *Gas) UnmarshalForm(dst interface{}) error {
	dv := reflect.ValueOf(dst)
	if dv.Kind() != reflect.Ptr {
		return errNotStructPointer
	}
	dv = dv.Elem()
	if dv.Kind() != reflect.Struct {
		return errNotStructPointer
	}
	dt := dv.Type()

	for i := 0; i < dv.NumField(); i++ {
		field := dv.Field(i)
		tf := dt.Field(i)
		key := tf.Tag.Get("form")
		if key == "" {
			key = tf.Name
		}
		val := g.FormValue(key)
		if len(val) == 0 {
			continue
		}

		// handle common non-core types
		fi := field.Interface()
		switch v := fi.(type) {
		case encoding.TextUnmarshaler:
			err := v.UnmarshalText([]byte(val))
			if err != nil {
				return err
			}
			continue
		case time.Time:
			format := tf.Tag.Get("timeFormat")
			var t time.Time
			if format == "" {
				n, err := strconv.ParseInt(val, 10, 64)
				if err != nil {
					return err
				}
				t = time.Unix(n, 0)
			} else {
				var err error
				t, err = time.Parse(format, val)
				if err != nil {
					return err
				}
			}
			field.Set(reflect.ValueOf(t))
			continue
		}

		// handle core types
		switch field.Kind() {
		case reflect.Bool:
			x, err := strconv.ParseBool(val)
			if err != nil {
				if val == "on" {
					field.SetBool(true)
					break
				}
				return err
			}
			field.SetBool(x)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if val == "" {
				field.SetInt(0)
			} else {
				x, err := strconv.ParseInt(val, 10, 64)
				if err != nil {
					return err
				}
				field.SetInt(x)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if val == "" {
				field.SetUint(0)
			} else {
				x, err := strconv.ParseUint(val, 10, 64)
				if err != nil {
					return err
				}
				field.SetUint(x)
			}
		case reflect.Float32, reflect.Float64:
			if val == "" {
				field.SetFloat(0.0)
			} else {
				x, err := strconv.ParseFloat(val, 64)
				if err != nil {
					return err
				}
				field.SetFloat(x)
			}
		case reflect.String:
			s, err := url.QueryUnescape(val)
			if err != nil {
				return err
			}
			field.SetString(s)
		//case reflect.Slice: // byte slice
		default:
			return fmt.Errorf(errUnsupportedKind, key, fi)
		}
	}

	return nil
}
