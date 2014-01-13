package gas

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	uri "net/url"
	"testing"
)

var testclient *http.Client

func init() {
	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err)
	}
	testclient = &http.Client{Jar: jar}
}

type MyUser struct {
	Id   int
	Name string
	Pass []byte
	Salt []byte
}

func (u *MyUser) Username() string {
	if u == nil {
		return ""
	}
	return u.Name
}

func (u *MyUser) Secrets() ([]byte, []byte, error) {
	if u == nil {
		return nil, nil, errors.New("secrets: nil user")
	}
	return u.Pass, u.Salt, nil
}

func (u *MyUser) byUsername(name string) (*MyUser, error) {
	if name == "" {
		return nil, errors.New("byUsername: empty name")
	}
	u = new(MyUser)
	err := Query(u, "SELECT * FROM gas_test_users WHERE name = $1", name)
	return u, err
}

func TestAuth(t *testing.T) {
	/*
		runtime.GOMAXPROCS(runtime.NumCPU())
		go func() {
			//fmt.Println(http.ListenAndServe(":6006", nil))
		}()
	*/

	if err := InitDB(); err != nil {
		t.Fatal(err)
	}
	defer DB.Close()
	initThings()

	testPass := "hello"
	hash, salt := NewHash([]byte(testPass))

	tx, err := DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	tx.Exec(`
	CREATE TEMP TABLE gas_test_users (
		id serial PRIMARY KEY,
		name text NOT NULL,
		pass bytea NOT NULL,
		salt bytea NOT NULL
	)`)
	tx.Exec(`INSERT INTO gas_test_users VALUES ( DEFAULT, 'moshee', $1, $2 )`, hash, salt)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	New().Get("/", func(g *Gas) {
		if sess, _ := g.Session(); sess == nil {
			fmt.Fprint(g, "no")
		} else {
			if u, err := new(MyUser).byUsername(sess.Username); err != nil {
				fmt.Fprint(g, "no")
			} else {
				fmt.Fprintf(g, "%d", u.Id)
			}
		}
	}).Post("/login", func(g *Gas) {
		u, err := new(MyUser).byUsername(g.FormValue("username"))
		if err != nil {
			fmt.Fprint(g, "no")
			return
		}
		if err = g.SignIn(u); err != nil {
			fmt.Fprint(g, "no")
		} else {
			fmt.Fprint(g, "yes")
		}
	}).Get("/logout", func(g *Gas) {
		if err := g.SignOut(); err != nil {
			fmt.Fprint(g, "no")
		} else {
			fmt.Fprint(g, "yes")
		}
	})

	srv := httptest.NewServer(http.HandlerFunc(dispatch))
	defer srv.Close()

	tester := &authTester{srv, testclient, t}
	form := make(uri.Values)
	form.Set("username", "moshee")
	form.Set("pass", testPass)

	form2 := make(uri.Values)
	form2.Set("username", "nobody")
	form2.Set("pass", "nothing")

	tester.try("/", "no", nil)
	tester.try("/login", "yes", form)
	tester.try("/login", "yes", form)
	tester.try("/", "1", nil)
	tester.try("/logout", "yes", nil)
	tester.try("/", "no", nil)
	tester.try("/login", "no", form2)
}

type authTester struct {
	srv    *httptest.Server
	client *http.Client
	*testing.T
}

func (t *authTester) try(url, expected string, form uri.Values) {
	url = t.srv.URL + url

	var (
		resp *http.Response
		err  error
	)
	if form == nil {
		resp, err = t.client.Get(url)
	} else {
		resp, err = t.client.PostForm(url, form)
	}
	if err != nil {
		t.Error(err)
		return
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
		return
	}
	if s := string(body); s != expected {
		t.Errorf("Get %s: expected '%s', got '%s'", url, expected, s)
	}
}
