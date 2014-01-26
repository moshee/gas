package gas

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOutputter(t *testing.T) {
	parseTemplates("testdata")
	srv := httptest.NewServer(http.HandlerFunc(dispatch))
	defer srv.Close()

	New().Get("/htmltest", func(g *Gas) (int, Outputter) {
		return 200, HTML("a/index", "world")
	}).Get("/jsontest", func(g *Gas) (int, Outputter) {
		return 200, JSON(&struct {
			A int
			B string
			C bool
		}{-203881, "asdf", true})
	}).Get("/htmltest2", func(g *Gas) (int, Outputter) {
		return 200, HTML("m/a/g/i/c", "# hi\n")
	}).Get("/htmltest3", func(g *Gas) (int, Outputter) {
		return 200, HTML("something", 123)
	})

	testGet(t, srv, "/htmltest", "Hello, world! testing!")
	testGet(t, srv, "/jsontest", `{"A":-203881,"B":"asdf","C":true}`+"\n")
	testGet(t, srv, "/htmltest2", "<h1>hi</h1>\n")
	testGet(t, srv, "/htmltest3", "123123123")
}
