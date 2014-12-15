package gas

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"ktkr.us/pkg/gas/testutil"
)

var acceptTests = []*testutil.Test{
	{"/asdf", "text/html", nil},
	{"/asdf", "*/*", []string{"Accept", "*/*;q=0.6"}},
	{"/asdf", "application/json", []string{"Accept", "application/json,text/html;q=1.0,text/plain;q=0.9"}},
	{"/asdf.html", "text/html", nil},
	{"/asdf.json", "application/json", nil},
	{"/asdf.json", "text/html", []string{"Accept", "text/html,text/xhtml,application/json;q=0.9"}},
	{"/asdf.png", "image/png", nil},
	{"/asdf.png", "image/png", []string{"Accept", "image/png"}},
	{"/asdf.png", "text/html", []string{"Accept", "Text/Html;q=1.0,image/png;q=0.9"}},
}

func TestAccept(t *testing.T) {
	r := New().Get("/{*}", func(g *Gas) (int, Outputter) {
		fmt.Fprint(g, g.Wants())
		return 0, nil
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	for _, test := range acceptTests {
		test.Test(t, srv)
	}
}

type T struct {
	s string
}

func (t *T) UnmarshalText(b []byte) error {
	t.s = string(b) + " lmao"
	return nil
}

type unmarshalFormTest struct {
	Int         int
	String      string
	Time        time.Time
	Float       float64   `form:"f"`
	Time2       time.Time `form:"t" timeFormat:"Mon, 02 Jan 2006 15:04:05 MST"`
	EmptyNumber uint64
	Bool        bool
	T           *T
}

func TestUnmarshalForm(t *testing.T) {
	now := time.Now()
	now1123 := url.QueryEscape(now.Format(time.RFC1123))
	nowUnix := url.QueryEscape(strconv.FormatInt(now.Unix(), 10))

	expected := unmarshalFormTest{42, "asdf", now, 3.1415, now, 0, true, &T{"ayy lmao"}}

	r := New().Get("/", func(g *Gas) (int, Outputter) {
		var v unmarshalFormTest
		if err := g.UnmarshalForm(&v); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(v, expected) {
			t.Fatal("got: %#v, expected: %#v", v, expected)
		}
		return g.Stop()
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	http.Get(srv.URL + "?Int=42&String=asdf&Time=" + nowUnix + "&f=3.1415&t=" + now1123 + "&Bool=1&T=ayy")
}
