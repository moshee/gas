package gas

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/moshee/gas/testutil"
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
