package gas

import (
	"bytes"
	"net/http"
	"net/http/httptest"
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

func TestRender(t *testing.T) {
	Templates = parse_templates("./testdata")
	New().Get("/render-test", func(g *Gas) {
		g.Render("a", "index", "tester")
	})

	srv := httptest.NewServer(http.HandlerFunc(dispatch))
	defer srv.Close()

	testGet(t, srv, "/render-test", "Hello, tester! testing!")
}
