package gas

import (
	"bytes"
	"testing"
)

func TestExecTemplates(t *testing.T) {
	Templates = parse_templates("./testdata")
	w := new(bytes.Buffer)
	if err := ExecTemplate(w, "a", "index", "world"); err != nil {
		t.Fatal(err)
	}
	got := string(w.Bytes())
	exp := "Hello, world! testing!"
	if got != exp {
		t.Fatalf("templates: expected '%s', got '%s'\n", exp, got)
	}
}
