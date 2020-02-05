package out

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/gas/testutil"
	"ktkr.us/pkg/vfs"
)

func TestParseTemplatePath(t *testing.T) {
	tab := []struct {
		in  string
		out templatePath
	}{
		{"", templatePath{}},
		{"a", templatePath{"", "a"}},
		{"ab", templatePath{"", "ab"}},
		{"a/a", templatePath{"a", "a"}},
		{"ab/cd", templatePath{"ab", "cd"}},
		{"/abcd", templatePath{"", "abcd"}},
		{"abcd/", templatePath{"", "abcd"}},
		{"/ab/cd/ef/", templatePath{"ab/cd", "ef"}},
	}

	for _, row := range tab {
		got := parseTemplatePath(row.in)
		if got != row.out {
			t.Errorf("%q: wanted %q, got %q", row.in, row.out, got)
		}
	}
}

func TestOutputter(t *testing.T) {
	fs, err := vfs.Native(".")
	if err != nil {
		t.Fatal(err)
	}

	err = parseTemplates(fs)
	if err != nil {
		t.Fatal(err)
	}

	r := gas.New().
		Get("/htmltest", func(g *gas.Gas) (int, gas.Outputter) {
			return 200, HTML("a/index/content", "world")
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
			return 200, HTML("something/content", 123)
		}).
		Get("/htmltest5", func(g *gas.Gas) (int, gas.Outputter) {
			return 200, HTML("something/parens", "xyz")
		}).
		Get("/htmltest7", func(g *gas.Gas) (int, gas.Outputter) {
			return 200, HTML("something/nonexistent", "asdf")
		})

	srv := httptest.NewServer(r)
	defer srv.Close()

	testutil.TestGet(t, srv, "/htmltest", "Hello, world! testing!")
	testutil.TestGet(t, srv, "/jsontest", `{"A":-203881,"B":"asdf","C":true}`+"\n")
	testutil.TestGet(t, srv, "/htmltest2", "<h1>hi</h1>\n")
	testutil.TestGet(t, srv, "/htmltest3", `"123"`)
	testutil.TestGet(t, srv, "/htmltest5", `("xyz")`)
	testutil.TestGet(t, srv, "/htmltest7", "Error: no such template: something/nonexistent")
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
