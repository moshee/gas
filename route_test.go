package gas

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func mapeq(m1, m2 map[string]string) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k, v := range m1 {
		if m2[k] != v {
			return false
		}
	}
	return true
}

type Test struct {
	pat     string
	url     string
	vals    map[string]string
	matched bool
}

var tests = []Test{
	{"/", "/", nil, true},
	{"/blog", "/blog", nil, true},
	{"/blog/view/{id}", "/blog", nil, false},
	{"/blog/view/{id}", "/files/manga/manga.html", nil, false},
	{"/blog/view/{id}", "/blog/view", nil, false},
	{"/blog/view/{id}", "/blog/view/123", map[string]string{"id": "123"}, true},
	{"/blog/view/{id}", "/blog/view/asdf/asdf", map[string]string{"id": "asdf/asdf"}, true}, // XXX: is this behavior really desired?
	{"/files", "/files", nil, true},
	{"/files{path}", "/blog/view/123", nil, false},
	// XXX: work on trailing slash handling
	{"/files{path}", "/files/", map[string]string{"path": "/"}, true},
	{"/files{path}", "/files/lol", map[string]string{"path": "/lol"}, true},
	{"/files/{a}/{b}/{c}", "/files/a/b/c/asdf/日本語/index.html", map[string]string{"a": "a", "b": "b", "c": "c/asdf/日本語/index.html"}, true},
	{"/test/{id}/asdf", "/test/a", nil, false},
	{"/test/{id}/asdf", "/test/a/a", nil, false},
	{"/test/{id}/asdf", "/test/b/asdf", map[string]string{"id": "b"}, true},
}

func TestMatch(t *testing.T) {
	for _, test := range tests {
		p := false
		m := newRoute("GET", test.pat, nil)
		vals, ok := m.match("GET", test.url)
		if !mapeq(vals, test.vals) {
			t.Log(m)
			p = true
			t.Errorf("%s → %s:\n", test.pat, test.url)
			t.Errorf("\tExpected %v, got %v\n", test.vals, vals)
		}
		if ok != test.matched {
			if !p {
				t.Log(m)
				t.Errorf("%s → %s:\n", test.pat, test.url)
			}
			t.Errorf("\tExpected %v, got %v\n", test.matched, ok)
		}
	}
}

func testGet(t *testing.T, srv *httptest.Server, url, expected string) {
	url = srv.URL + url
	resp, err := testclient.Get(url)
	if err != nil {
		t.Errorf("testGet %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("testGet %s: %v", url, err)
		return
	}

	if s := string(body); s != expected {
		t.Errorf("testGet %s: expected '%s', got '%s'", url, expected, s)
	}
}

func TestDispatch(t *testing.T) {
	New().Use(func(g *Gas) (int, Outputter) {
		g.SetData("middleware", true)
		return 0, nil
	}).Get("/test1", func(g *Gas) (int, Outputter) {
		g.Write([]byte("yes"))
		return -1, nil
	}).Get("/test2", func(g *Gas) (int, Outputter) {
		g.SetData("something", 6)
		g.SetData("something else", "test")
		return 0, nil
	}, func(g *Gas) (int, Outputter) {
		g.Write([]byte(g.Data("something else").(string)))
		return -1, nil
	}).Get("/test3", func(g *Gas) (int, Outputter) {
		g.SetData("test", 10)
		return 0, nil
	}, func(g *Gas) (int, Outputter) {
		g.Write([]byte(strconv.Itoa(g.Data("test").(int))))
		return -1, nil
	}, func(g *Gas) (int, Outputter) {
		g.Write([]byte("nope"))
		return -1, nil
	}).Get("/test4", func(g *Gas) (int, Outputter) {
		g.Write([]byte(strconv.FormatBool(g.Data("middleware").(bool))))
		return -1, nil
	}).Get("/panic", func(g *Gas) (int, Outputter) {
		panic("lol")
	})

	srv := httptest.NewServer(http.HandlerFunc(dispatch))
	defer srv.Close()

	testGet(t, srv, "/test1", "yes")
	testGet(t, srv, "/test2", "test")
	testGet(t, srv, "/test3", "10")
	testGet(t, srv, "/test4", "true")

	v := Verbosity
	Verbosity -= v
	resp, err := testclient.Get(srv.URL + "/panic")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 500 {
		t.Fatalf("expected 500 code for panic, got %d", resp.StatusCode)
	}
	Verbosity = v
}

func TestReroute(t *testing.T) {
	New().Get("/reroute1", func(g *Gas) (int, Outputter) {
		return 303, Reroute("/reroute2", map[string]string{"test": "ok"})
	}).Get("/reroute2", func(g *Gas) (int, Outputter) {
		var m map[string]string
		if err := g.Recover(&m); err != nil {
			t.Fatal(err)
			fmt.Fprint(g, "no")
		}
		fmt.Fprint(g, m["test"])
		return -1, nil
	})

	srv := httptest.NewServer(http.HandlerFunc(dispatch))
	defer srv.Close()
	testGet(t, srv, "/reroute1", "ok")
}

func TestAddrHost(t *testing.T) {
	tests := map[string]string{
		"127.0.0.1:12341":     "127.0.0.1",
		"localhost:3454":      "localhost",
		"us.dl.ktkr.us":       "us.dl.ktkr.us",
		"53.261.66.12:994":    "53.261.66.12",
		"[fe80:2001::1]:6642": "[fe80:2001::1]",
		"www.google.com:80":   "www.google.com",
	}

	for in, out := range tests {
		got := addrHost(in)
		if got != out {
			t.Errorf("addrHost: '%s': expected '%s', got '%s'", in, out, got)
		}
	}
}

type Bench struct {
	route *route
	url   string
}

var bb []Bench

func init() {
	bb = make([]Bench, len(tests))
	for i := range bb {
		bb[i] = Bench{newRoute("GET", tests[i].pat, nil), tests[i].url}
	}
}

func BenchmarkMatch(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r := bb[i%len(bb)]
		r.route.match("GET", r.url)
	}
}

func BenchmarkMatchSingle0(b *testing.B) {
	r := bb[0]
	for i := 0; i < b.N; i++ {
		r.route.match("GET", r.url)
	}
}

func BenchmarkMatchSingle11(b *testing.B) {
	r := bb[11]
	for i := 0; i < b.N; i++ {
		r.route.match("GET", r.url)
	}
}

func BenchmarkMatchSingle14(b *testing.B) {
	r := bb[14]
	for i := 0; i < b.N; i++ {
		r.route.match("GET", r.url)
	}
}
