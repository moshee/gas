package gas

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestEnvConf(t *testing.T) {
	assertEqual := func(a, b interface{}) {
		if !reflect.DeepEqual(a, b) {
			t.Errorf("Expected %v (%[1]T) != got %v (%[2]T)", b, a)
		}
	}
	// save env vars
	env := make(map[string]string)
	for _, key := range []string{"DB_NAME", "DB_PARAMS", "PORT"} {
		name := EnvPrefix + key
		env[name] = os.Getenv(name)
	}

	os.Setenv("GAS_DB_NAME", env["GAS_DB_NAME"])
	os.Setenv("GAS_COOKIE_AUTH_KEY", "asdfasdf")
	os.Setenv("GAS_PORT", "abc")
	if err := EnvConf(&Env, EnvPrefix); err == nil {
		t.Error("Expected strconv error in envconf, got nothing")
	}

	os.Setenv("GAS_DB_PARAMS", env["GAS_DB_PARAMS"])
	os.Setenv("GAS_PORT", "")
	os.Setenv("GAS_COOKIE_AUTH_KEY", "")
	if err := EnvConf(&Env, EnvPrefix); err != nil {
		t.Errorf("Expected no envconf error, got %v", err)
	}

	assertEqual(Env.Port, 80)
	for key, val := range env {
		os.Setenv(EnvPrefix+key, val)
	}

	conf := struct {
		Bool     bool `envconf:"required"`
		String   string
		Int      int
		Int64    int64
		Uint     uint
		Uint64   uint64
		Float64  float64
		Duration time.Duration
	}{}

	env = map[string]string{
		"BOOL":     "t",
		"STRING":   "testing",
		"INT":      "9553325",
		"INT64":    "-3453466699214",
		"UINT":     "22299929",
		"UINT64":   "9325324234324324",
		"FLOAT64":  "3.14159265358979323846264833",
		"DURATION": "1h2s3ms4µs5ns",
	}

	prefix := "GAS_TEST_"

	for key := range env {
		os.Setenv(prefix+key, "_")
	}

	if err := EnvConf(&conf, prefix); err == nil {
		t.Errorf("Expected some sort of error, got nothing")
	}

	for key, val := range env {
		os.Setenv(prefix+key, val)
	}

	if err := EnvConf(&conf, prefix); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	os.Setenv("GAS_TEST_BOOL", "")
	if err := EnvConf(&conf, prefix); err == nil {
		t.Error("Expected envconf error 'required parameter not given', got nothing")
	}

	assertEqual(conf.Bool, true)
	assertEqual(conf.String, "testing")
	assertEqual(conf.Int, 9553325)
	assertEqual(conf.Int64, int64(-3453466699214))
	assertEqual(conf.Uint, uint(22299929))
	assertEqual(conf.Uint64, uint64(9325324234324324))
	assertEqual(conf.Float64, 3.14159265358979323846264833)
	assertEqual(conf.Duration, time.Hour+2*time.Second+3*time.Millisecond+4*time.Microsecond+5*time.Nanosecond)
}
