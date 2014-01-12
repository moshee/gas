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
	New().Use(func(g *Gas) {
		g.SetData("middleware", true)
	}).Get("/test1", func(g *Gas) {
		g.Write([]byte("yes"))
	}).Get("/test2", func(g *Gas) {
		g.SetData("something", 6)
		g.SetData("something else", "test")
	}, func(g *Gas) {
		g.Write([]byte(g.Data("something else").(string)))
	}).Get("/test3", func(g *Gas) {
		g.SetData("test", 10)
	}, func(g *Gas) {
		g.Write([]byte(strconv.Itoa(g.Data("test").(int))))
	}, func(g *Gas) {
		g.Write([]byte("nope"))
	}).Get("/test4", func(g *Gas) {
		g.Write([]byte(strconv.FormatBool(g.Data("middleware").(bool))))
	})

	srv := httptest.NewServer(http.HandlerFunc(dispatch))
	defer srv.Close()

	testGet(t, srv, "/test1", "yes")
	testGet(t, srv, "/test2", "test")
	testGet(t, srv, "/test3", "10")
	testGet(t, srv, "/test4", "true")
}

func TestReroute(t *testing.T) {
	New().Get("/reroute1", func(g *Gas) {
		g.Reroute("/reroute2", 303, map[string]string{"test": "ok"})
	}).Get("/reroute2", func(g *Gas) {
		var m map[string]string
		if err := g.Recover(&m); err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(g, m["test"])
	})

	srv := httptest.NewServer(http.HandlerFunc(dispatch))
	defer srv.Close()
	testGet(t, srv, "/reroute1", "ok")
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
