package gas

import (
	"os"
	"testing"
)

func TestEnvConf(t *testing.T) {
	// save env vars
	env := make(map[string]string)
	for _, key := range []string{"DB_NAME", "DB_PARAMS", "PORT"} {
		name := EnvPrefix + key
		env[name] = os.Getenv(name)
	}

	os.Setenv("GAS_DB_NAME", "")
	if err := envconf(); err == nil {
		t.Error("Expected envconf error 'required parameter not given', got nothing")
	}

	os.Setenv("GAS_DB_NAME", env["GAS_DB_NAME"])
	os.Setenv("GAS_PORT", "abc")
	if err := envconf(); err == nil {
		t.Error("Expected strconv error in envconf, got nothing")
	}

	os.Setenv("GAS_DB_PARAMS", env["GAS_DB_PARAMS"])
	os.Setenv("GAS_PORT", "")
	if err := envconf(); err != nil {
		t.Errorf("Expected no envconf error, got %v", err)
	}

	if Env.PORT != 80 {
		t.Errorf("Expected default value PORT = 80, got %d", Env.PORT)
	}
	os.Setenv("GAS_PORT", env["GAS_PORT"])
}
