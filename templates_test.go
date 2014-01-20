package gas

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

var templateOnce sync.Once

func TestExecTemplates(t *testing.T) {
	templateOnce.Do(func() {
		Templates = parse_templates("./testdata")
	})
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

func TestOutputter(t *testing.T) {
	templateOnce.Do(func() {
		Templates = parse_templates("./testdata")
	})

	srv := httptest.NewServer(http.HandlerFunc(dispatch))
	defer srv.Close()

	New().Get("/htmltest", func(g *Gas) (int, Outputter) {
		return 200, HTML("a", "index", "tester")
	}).Get("/jsontest", func(g *Gas) (int, Outputter) {
		return 200, JSON(&struct {
			A int
			B string
			C bool
		}{-203881, "asdf", true})
	})

	testGet(t, srv, "/htmltest", "Hello, tester! testing!")
	testGet(t, srv, "/jsontest", `{"A":-203881,"B":"asdf","C":true}`+"\n")
}
