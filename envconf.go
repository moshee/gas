package gas

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"time"
)

// The configuration parameters specified as environment variables. They may be
// overridden during runtime, but note that some are only used on startup
// (after init() and before Ignition).
var Env struct {
	DB_NAME   string `envconf:"required"`
	DB_PARAMS string `envconf:"required"`

	// Maximum age of a cookie before it goes stale. Syntax specified as in
	// time.ParseDuration (maximum unit is hours 'h')
	MAX_COOKIE_AGE time.Duration `default:"186h"`

	// The name of the database table in which sessions will be stored
	SESS_TABLE string `default:"gas_sessions"`

	// The length of the session ID in bytes
	SESSID_LEN int `default:"64"`

	// The port for the server to listen on
	PORT int `default:"80"`

	// HASH_COST is the cost passed into the scrypt hash function. It is
	// represented as the power of 2 (aka HASH_COST=9 means 2<<9 iterations).
	// It should be set as desired in the main() function of the importing
	// client. A value of 13 (the default) is a good number to start with, and
	// should be increased as hardware gets faster (see
	// http://www.tarsnap.com/scrypt.html for more info)
	HASH_COST uint `default:"13"`
}

// The prefix append to the field name in Env, e.g. Env.DB_NAME would be
// populated by the environment variable GAS_DB_NAME.
const EnvPrefix = "GAS_"

// Pass in a pointer to a struct that looks like Env and the fields will be
// filled in with the corresponding environment variables. Struct tag meanings:
//
//     envconf:"required" // an error will be returned if this var isn't given
//     default:"<default value>" // provide a default if this var isn't given
func EnvConf(conf interface{}, prefix string) error {
	val := reflect.ValueOf(conf).Elem()
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)
		name := prefix + field.Name
		v := os.Getenv(name)
		LogDebug("[envconf] %s = '%s'", name, v)

		if v == "" {
			if field.Tag.Get("envconf") == "required" {
				return fmt.Errorf("%s: required parameter not specified", name)
			} else if def := field.Tag.Get("default"); def != "" {
				if err := stringValue(def, fieldVal.Addr().Interface()); err != nil {
					return err
				}
			}
		} else {
			if err := stringValue(v, fieldVal.Addr().Interface()); err != nil {
				return err
			}
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
