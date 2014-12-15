package auth_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	uri "net/url"
	"testing"

	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/gas/auth"
	"ktkr.us/pkg/gas/db"
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
	err := db.Query(u, "SELECT * FROM gas_test_users WHERE name = $1", name)
	return u, err
}

func TestAuth(t *testing.T) {
	/*
		runtime.GOMAXPROCS(runtime.NumCPU())
		go func() {
			//fmt.Println(http.ListenAndServe(":6006", nil))
		}()
	*/
	testPass := "hello"
	hash, salt := auth.NewHash([]byte(testPass))

	tx, err := db.DB.Begin()
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

	r := gas.New().Get("/", func(g *gas.Gas) (int, gas.Outputter) {
		if sess, err := auth.GetSession(g); sess == nil || err != nil {
			fmt.Fprint(g, "no")
		} else {
			if u, err := new(MyUser).byUsername(sess.Username); err != nil {
				fmt.Fprint(g, "no")
			} else {
				fmt.Fprintf(g, "%d", u.Id)
			}
		}
		return -1, nil
	}).Get("/hmac", func(g *gas.Gas) (int, gas.Outputter) {
		_, err := auth.GetSession(g)
		if err != nil {
			fmt.Fprint(g, "no")
			if err != auth.ErrBadMac {
				t.Fatalf("Expected hmac error, got %v", err)
			}
		} else {
			fmt.Fprint(g, "yes")
		}
		return -1, nil
	}).Post("/login", func(g *gas.Gas) (int, gas.Outputter) {
		u, err := new(MyUser).byUsername(g.FormValue("username"))
		if err != nil {
			fmt.Fprint(g, "no")
			return -1, nil
		}
		if err = auth.SignIn(g, u, g.FormValue("pass")); err != nil {
			fmt.Fprint(g, "no")
		} else {
			fmt.Fprint(g, "yes")
		}
		return -1, nil
	}).Get("/logout", func(g *gas.Gas) (int, gas.Outputter) {
		if err := auth.SignOut(g); err != nil {
			fmt.Fprint(g, "no")
		} else {
			fmt.Fprint(g, "yes")
		}
		return -1, nil
	})

	t.Log("Testing DB session store")
	dbs, err := db.NewStore("gas_sessions")
	if err != nil {
		t.Fatal(err)
	}
	auth.UseSessionStore(dbs)
	testAuth(t, testPass, r)

	t.Log("Testing FS session store")
	fss, err := auth.NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	defer fss.Destroy()
	auth.UseSessionStore(fss)
	testAuth(t, testPass, r)
}

func testAuth(t *testing.T, testPass string, r *gas.Router) {
	srv := httptest.NewServer(r)
	defer srv.Close()

	auth.AddHMACKey([]byte("super secret key"))

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

	tester.try("/login", "yes", form)
	tester.try("/hmac", "yes", nil)
	url, _ := uri.Parse(srv.URL)
	cookies := tester.client.Jar.Cookies(url)
	if len(cookies) == 0 {
		t.Fatal("no cookies in the jar")
	}
	b, err := base64.StdEncoding.DecodeString(cookies[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	b[0] ^= 'z'
	cookies[0].Value = base64.StdEncoding.EncodeToString(b)
	tester.client.Jar.SetCookies(url, cookies)
	tester.try("/hmac", "no", nil)
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
