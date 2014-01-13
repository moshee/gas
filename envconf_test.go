package gas

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestEnvConf(t *testing.T) {
	// save env vars
	env := make(map[string]string)
	for _, key := range []string{"DB_NAME", "DB_PARAMS", "PORT"} {
		name := EnvPrefix + key
		env[name] = os.Getenv(name)
	}

	os.Setenv("GAS_DB_NAME", "")
	if err := EnvConf(&Env, EnvPrefix); err == nil {
		t.Error("Expected envconf error 'required parameter not given', got nothing")
	}

	os.Setenv("GAS_DB_NAME", env["GAS_DB_NAME"])
	os.Setenv("GAS_PORT", "abc")
	if err := EnvConf(&Env, EnvPrefix); err == nil {
		t.Error("Expected strconv error in envconf, got nothing")
	}

	os.Setenv("GAS_DB_PARAMS", env["GAS_DB_PARAMS"])
	os.Setenv("GAS_PORT", "")
	if err := EnvConf(&Env, EnvPrefix); err != nil {
		t.Errorf("Expected no envconf error, got %v", err)
	}

	if Env.PORT != 80 {
		t.Errorf("Expected default value PORT = 80, got %d", Env.PORT)
	}
	os.Setenv("GAS_PORT", env["GAS_PORT"])

	conf := struct {
		BOOL     bool
		STRING   string
		INT      int
		INT64    int64
		UINT     uint
		UINT64   uint64
		FLOAT64  float64
		DURATION time.Duration
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

	assertEqual := func(a, b interface{}) {
		if !reflect.DeepEqual(a, b) {
			t.Errorf("Expected %v (%[1]T) != got %v (%[2]T)", b, a)
		}
	}

	assertEqual(conf.BOOL, true)
	assertEqual(conf.STRING, "testing")
	assertEqual(conf.INT, 9553325)
	assertEqual(conf.INT64, int64(-3453466699214))
	assertEqual(conf.UINT, uint(22299929))
	assertEqual(conf.UINT64, uint64(9325324234324324))
	assertEqual(conf.FLOAT64, 3.14159265358979323846264833)
	assertEqual(conf.DURATION, time.Hour+2*time.Second+3*time.Millisecond+4*time.Microsecond+5*time.Nanosecond)
}
