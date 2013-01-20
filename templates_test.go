package gas

import (
	"testing"
	"bytes"
)

func TestExecTemplates(t *testing.T) {
	parse_templates("testdata")
	w := new(bytes.Buffer)
	exec_template("a", "index", w, "world")
	got := string(w.Bytes())
	exp := "Hello, world! testing!"
	if got != exp {
		t.Fatalf("templates: expected '%s', got '%s'\n", exp, got)
	}
}
