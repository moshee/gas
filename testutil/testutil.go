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

// TestGet requests a url on a server and matches an expected response against
// the recieved response.
func TestGet(t *testing.T, srv *httptest.Server, url, expected string) {
	url = srv.URL + url
	resp, err := Client.Get(url)
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
