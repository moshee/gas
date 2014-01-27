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
	}).Get("/htmltest4", func(g *Gas) (int, Outputter) {
		return 200, HTML("something", "abc", "layouts/parens", "layouts/brackets", "layouts/quotes")
	}).Get("/htmltest5", func(g *Gas) (int, Outputter) {
		return 200, HTML("something", "xyz", "layouts/parens")
	}).Get("/htmltest6", func(g *Gas) (int, Outputter) {
		return 200, HTML("donottrythisathome", 3, "layouts/wat")
	}).Get("/htmltest7", func(g *Gas) (int, Outputter) {
		return 200, HTML("something", "asdfasdf", "layouts/nonexistent")
	})

	testGet(t, srv, "/htmltest", "Hello, world! testing!")
	testGet(t, srv, "/jsontest", `{"A":-203881,"B":"asdf","C":true}`+"\n")
	testGet(t, srv, "/htmltest2", "<h1>hi</h1>\n")
	testGet(t, srv, "/htmltest3", "123123123")
	testGet(t, srv, "/htmltest4", "([\"abcabcabc\"])")
	testGet(t, srv, "/htmltest5", "(xyzxyzxyz)")
	testGet(t, srv, "/htmltest6", "3(3)3")
	v := Verbosity
	Verbosity -= v
	testGet(t, srv, "/htmltest7", "No such layout nonexistent in path layouts")
	Verbosity = v
}
