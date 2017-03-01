package gas

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// The configuration parameters specified as environment variables. Field names
// in CamelCase correspond to the environment variables in SHOUTING_SNAKE_CASE.
// They may be overridden during runtime, but note that some are only used on
// startup (after init() and before Ignition).
var Env struct {
	// The port for the server to listen on.
	//
	// PORT and TLS_PORT determine whether to use normal HTTP and/or HTTPS via
	// TLS. If a port number is set greater than 0, then it will be used. At
	// least one must be chosen; it is a fatal error to make both zero. If both
	// are enabled, then the server will listen on both in separate goroutines.
	//
	// TLS will not be used if FastCGI is used.
	Port    int `default:"80"`
	TLSPort int `default:"-1"`

	// LISTEN should contain a comma-separated list of network!address pairs
	// for the server to listen on. If ";tls" is appended to the end of a
	// network:address pair, use TLS on that listener using GAS_TLS_CERT and
	// GAS_TLS_KEY. If the network isn't given, it defaults to "tcp". Example:
	//
	//     GAS_LISTEN=":80, tcp![::1]:8080, unix!/var/run/website.sock, tcp!:https;tls"
	//
	// The server will listen concurrently on all listed interfaces. LISTEN
	// supercedes PORT and TLS_PORT, which are now deprecated.
	Listen string

	// When set, the server will listen using FastCGI on the given network.
	//
	// Deprecated.
	FastCGI string `default:"false"`

	// Paths to the TLS certificate and key files, if TLS is enabled. Same
	// rules as net/http.(*Server).ListenAndServeTLS.
	TLSCert string
	TLSKey  string

	// The hostname to send in the TLS handshake
	TLSHost string
}

// EnvPrefix is the prefix append to the field name in Env, e.g. Env.DBName
// would be populated by the environment variable GAS_DB_NAME.
const EnvPrefix = "GAS_"

// EnvConf will populate a pointer to a struct that looks like Env and the
// fields will be filled in with the corresponding environment variables. Struct
// tag meanings:
//
//     envconf:"required" // an error will be returned if this var isn't given
//     default:"<default value>" // provide a default if this var isn't given
func EnvConf(conf interface{}, prefix string) error {
	val := reflect.ValueOf(conf).Elem()
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)
		name := prefix + strings.ToUpper(ToSnake(field.Name))
		v := os.Getenv(name)
		log.Printf("[envconf] %s = '%s'", name, v)

		if v == "" {
			if field.Tag.Get("envconf") == "required" {
				return fmt.Errorf("%s: required parameter not specified", name)
			} else if def := field.Tag.Get("default"); def != "" {
				if err := stringValue(def, fieldVal.Addr().Interface()); err != nil {
					return err
				}
				continue
			}
		}
		if err := stringValue(v, fieldVal.Addr().Interface()); err != nil {
			return err
		}
	}

	return nil
}

func stringValue(s string, fieldVal interface{}) error {
	var err error

	switch v := fieldVal.(type) {
	case *bool:
		*v, err = strconv.ParseBool(s)
	case *string:
		*v = s
	case *[]byte:
		*v = []byte(s)
	case *int:
		*v, err = strconv.Atoi(s)
	case *int64:
		*v, err = strconv.ParseInt(s, 10, 64)
	case *uint:
		var a uint64
		a, err = strconv.ParseUint(s, 10, 32)
		*v = uint(a)
	case *uint64:
		*v, err = strconv.ParseUint(s, 10, 64)
	case *float64:
		*v, err = strconv.ParseFloat(s, 64)
	case *time.Duration:
		*v, err = time.ParseDuration(s)
	default:
		return fmt.Errorf("unhandled parameter type %T", fieldVal)
	}

	return err
}
