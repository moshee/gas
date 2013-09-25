package gas

import "testing"

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
		m := new_route("GET", test.pat, nil)
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

type Bench struct {
	route *route
	url   string
}

var bb []Bench

func init() {
	bb = make([]Bench, len(tests))
	for i := range bb {
		bb[i] = Bench{new_route("GET", tests[i].pat, nil), tests[i].url}
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
