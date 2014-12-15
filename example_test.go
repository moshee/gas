package gas_test

import (
	"reflect"

	"ktkr.us/pkg/gas"
	"ktkr.us/pkg/gas/auth"
	"ktkr.us/pkg/gas/db"
	"ktkr.us/pkg/gas/out"
)

type T struct{ Id int }

func ExampleRegister() {
	t := new(T)
	db.Register(reflect.TypeOf(t))
}

type M map[string]interface{}

type myUser struct{}

func (*myUser) Username() string                 { return "" }
func (*myUser) Secrets() ([]byte, []byte, error) { return nil, nil, nil }
func (*myUser) byUsername(n string) *myUser      { return nil }

func ExampleRouter() {
	// A simple "static" route.
	loginForm := func(g *gas.Gas) (int, gas.Outputter) {
		return 200, out.HTML("example/login-form", nil)
	}

	// JSON REST? Sure.
	login := func(g *gas.Gas) (int, gas.Outputter) {
		u := new(myUser).byUsername(g.FormValue("user"))
		if err := auth.SignIn(g, u, g.FormValue("pass")); err != nil {
			return 403, out.JSON(M{"error": err.Error()})
		} else {
			return 204, nil
		}
	}

	// Reroute users (+ a cookie with the path data) if not logged in
	checkLogin := func(path string) func(g *gas.Gas) (int, gas.Outputter) {
		return func(g *gas.Gas) (int, gas.Outputter) {
			if sess, err := auth.GetSession(g); sess == nil || err != nil {
				return 303, out.Reroute(path, map[string]string{"path": g.URL.Path})
			} else {
				g.SetData("user", new(myUser).byUsername(sess.Username))
			}
			return 0, nil
		}
	}

	// A page behind the login wall
	profile := func(g *gas.Gas) (int, gas.Outputter) {
		user := g.Data("user").(*myUser)
		return 200, out.HTML("example", user)
	}

	// The router
	gas.New().
		Get("/profile", checkLogin("/login"), profile).
		Get("/login", loginForm).
		Post("/login", login)
}
