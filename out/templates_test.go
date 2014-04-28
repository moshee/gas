package out

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/gas/testutil"
)

func TestOutputter(t *testing.T) {
	parseTemplates(templateDir)
	r := gas.New().Get("/htmltest", func(g *gas.Gas) (int, gas.Outputter) {
		return 200, HTML("a/index", "world")
	}).
		Get("/jsontest", func(g *gas.Gas) (int, gas.Outputter) {
		return 200, JSON(&struct {
			A int
			B string
			C bool
		}{-203881, "asdf", true})
	}).
		Get("/htmltest2", func(g *gas.Gas) (int, gas.Outputter) {
		return 200, HTML("m/a/g/i/c", "# hi\n")
	}).
		Get("/htmltest3", func(g *gas.Gas) (int, gas.Outputter) {
		return 200, HTML("something", 123)
	}).
		Get("/htmltest4", func(g *gas.Gas) (int, gas.Outputter) {
		return 200, HTML("something", "abc", "layouts/parens", "layouts/brackets", "layouts/quotes")
	}).
		Get("/htmltest5", func(g *gas.Gas) (int, gas.Outputter) {
		return 200, HTML("something", "xyz", "layouts/parens")
	}).
		Get("/htmltest6", func(g *gas.Gas) (int, gas.Outputter) {
		return 200, HTML("donottrythisathome", 3, "layouts/wat")
	}).
		Get("/htmltest7", func(g *gas.Gas) (int, gas.Outputter) {
		return 200, HTML("something", "asdfasdf", "layouts/nonexistent")
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	testutil.TestGet(t, srv, "/htmltest", "Hello, world! testing!")
	testutil.TestGet(t, srv, "/jsontest", `{"A":-203881,"B":"asdf","C":true}`+"\n")
	testutil.TestGet(t, srv, "/htmltest2", "<h1>hi</h1>\n")
	testutil.TestGet(t, srv, "/htmltest3", "123123123")
	testutil.TestGet(t, srv, "/htmltest4", `(["abcabcabc"])`)
	testutil.TestGet(t, srv, "/htmltest5", "(xyzxyzxyz)")
	testutil.TestGet(t, srv, "/htmltest6", "3(3)3")
	testutil.TestGet(t, srv, "/htmltest7", "no such layout nonexistent in path layouts")
}

func TestReroute(t *testing.T) {
	r := gas.New().Get("/reroute1", func(g *gas.Gas) (int, gas.Outputter) {
		return 303, Reroute("/reroute2", map[string]string{"test": "ok"})
	}).Get("/reroute2", CheckReroute, func(g *gas.Gas) (int, gas.Outputter) {
		var m map[string]string
		if err := Recover(g, &m); err != nil {
			t.Fatal(err)
			fmt.Fprint(g, "no")
		}
		fmt.Fprint(g, m["test"])
		return -1, nil
	})

	srv := httptest.NewServer(r)
	defer srv.Close()
	testutil.TestGet(t, srv, "/reroute1", "ok")
}
