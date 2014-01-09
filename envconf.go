package gas

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
)

// The configuration parameters specified as environment variables. They may be
// overridden during runtime, but note that some are only used on startup
// (after init() and before Ignition).
var Env struct {
	DB_NAME   string `envconf:"required"`
	DB_PARAMS string `envconf:"required"`
	PORT      int    `default:"80"`
}

// The prefix append to the field name in Env, e.g. Env.DB_NAME would be
// populated by the environment variable GAS_DB_NAME.
const EnvPrefix = "GAS_"

func init() {
	if err := envconf(); err != nil {
		LogFatal("envconf: %v", err)
	}
}

func envconf() error {
	val := reflect.ValueOf(&Env).Elem()
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)
		name := EnvPrefix + field.Name
		v := os.Getenv(name)
		LogDebug("[envconf] %s = '%s'", name, v)

		if v == "" {
			if field.Tag.Get("envconf") == "required" {
				return fmt.Errorf("%s: required parameter not specified", name)
			} else if def := field.Tag.Get("default"); def != "" {
				val, err := stringValue(def, field.Type)
				if err != nil {
					return fmt.Errorf("%s: %v", name, err)
				}
				fieldVal.Set(val)
			}
		} else {
			val, err := stringValue(v, field.Type)
			if err != nil {
				return fmt.Errorf("%s: %v", name, err)
			}
			fieldVal.Set(val)
		}
	}

	return nil
}

func stringValue(s string, typ reflect.Type) (reflect.Value, error) {
	switch kind := typ.Kind(); kind {
	case reflect.String:
		return reflect.ValueOf(s), nil
	case reflect.Int:
		n, err := strconv.Atoi(s)
		if err != nil {
			return reflect.Zero(typ), err
		}
		return reflect.ValueOf(n), nil
	case reflect.Float64:
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return reflect.Zero(typ), err
		}
		return reflect.ValueOf(n), nil
	default:
		return reflect.Zero(typ), fmt.Errorf("unhandled parameter type %v", kind)
	}
}
