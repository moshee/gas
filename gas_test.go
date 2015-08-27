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

func TestUserAgents(t *testing.T) {
	tests := []struct {
		str string
		uas []UA
	}{
		{
			"",
			[]UA{},
		},
		{
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2227.0 Safari/537.36",
			[]UA{{"Mozilla", "5.0", "X11; Linux x86_64"}, {"AppleWebKit", "537.36", "KHTML, like Gecko"}, {"Chrome", "41.0.2227.0", ""}, {"Safari", "537.36", ""}},
		},
		{
			"ELinks/0.9.3 (textmode; Linux 2.6.9-kanotix-8 i686; 127x41)",
			[]UA{{"ELinks", "0.9.3", "textmode; Linux 2.6.9-kanotix-8 i686; 127x41"}},
		},
		{
			"Mozilla/5.0 (compatible; MSIE 8.0; Windows NT 6.1; Trident/4.0; GTB7.4; InfoPath.2; SV1; .NET CLR 3.3.69573; WOW64; en-US)",
			[]UA{{"Mozilla", "5.0", "compatible; MSIE 8.0; Windows NT 6.1; Trident/4.0; GTB7.4; InfoPath.2; SV1; .NET CLR 3.3.69573; WOW64; en-US"}},
		},
		{
			"Twitterbot/1.0",
			[]UA{{"Twitterbot", "1.0", ""}},
		},
		{
			"Galaxy/1.0 [en] (Mac OS X 10.5.6; U; en)",
			[]UA{{"Galaxy", "1.0 [en]", "Mac OS X 10.5.6; U; en"}},
		},
		{
			"Mozilla/4.0 (compatible; MSIE 7.0; Windows NT 5.1; Mozilla/4.0 (compatible; MSIE 6.0; Windows NT 5.1; SV1) ; .NET CLR 1.0.3705; .NET CLR 1.1.4322; Media Center PC 4.0; .NET CLR 2.0.50727; InfoPath.1; GreenBrowser)",
			[]UA{{"Mozilla", "4.0", "compatible; MSIE 7.0; Windows NT 5.1; Mozilla/4.0 (compatible; MSIE 6.0; Windows NT 5.1; SV1) ; .NET CLR 1.0.3705; .NET CLR 1.1.4322; Media Center PC 4.0; .NET CLR 2.0.50727; InfoPath.1; GreenBrowser"}},
		},
	}

	for _, test := range tests {
		uas := ParseUserAgents(test.str)
		if len(uas) == 0 && len(test.uas) == 0 {
			continue
		}
		if !reflect.DeepEqual(uas, test.uas) {
			t.Fatalf("got: %#v, expected: %#v", uas, test.uas)
		}
	}
}
