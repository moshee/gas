package testutil

import (
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
)

// Client is the singleton http.Client
var Client = &http.Client{}

func init() {
	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err)
	}
	Client.Jar = jar
}

type Test struct {
	Path    string
	Expect  string
	Headers []string
}

func (test *Test) Test(t *testing.T, srv *httptest.Server) {
	TestGet(t, srv, test.Path, test.Expect, test.Headers...)
}

// TestGet requests a url on a server and matches an expected response against
// the recieved response.
func TestGet(t *testing.T, srv *httptest.Server, url, expected string, headers ...string) {
	url = srv.URL + url
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) > 0 {
		if len(headers)%2 != 0 {
			t.Fatal("header key-value pairs do not match up")
		}
		for i := 0; i < len(headers)/2; i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
	}
	resp, err := Client.Do(req)
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
