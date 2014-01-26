package gas

import "reflect"

type T struct{ Id int }

func ExampleRegister() {
	t := new(T)
	Register(reflect.TypeOf(t))
}

type M map[string]interface{}

type myUser struct{}

func (*myUser) Username() string                 { return "" }
func (*myUser) Secrets() ([]byte, []byte, error) { return nil, nil, nil }
func (*myUser) byUsername(n string) *myUser      { return nil }

func ExampleRouter() {
	// A simple "static" route.
	loginForm := func(g *Gas) (int, Outputter) {
		return 200, HTML("example/login-form", nil)
	}

	// JSON REST? Sure.
	login := func(g *Gas) (int, Outputter) {
		u := new(myUser).byUsername(g.FormValue("user"))
		if err := g.SignIn(u); err != nil {
			return 403, JSON(M{"error": err.Error()})
		} else {
			return 204, nil
		}
	}

	// Reroute users (+ a cookie with the path data) if not logged in
	checkLogin := func(path string) func(g *Gas) (int, Outputter) {
		return func(g *Gas) (int, Outputter) {
			if sess, err := g.Session(); sess == nil || err != nil {
				return 303, Reroute(path, map[string]string{"path": g.URL.Path})
			} else {
				g.SetData("user", new(myUser).byUsername(sess.Username))
			}
			return 0, nil
		}
	}

	// A page behind the login wall
	profile := func(g *Gas) (int, Outputter) {
		user := g.Data("user").(*myUser)
		return 200, HTML("example", user)
	}

	// The router
	New("asdf").
		Get("/profile", checkLogin("/login"), profile).
		Get("/login", loginForm).
		Post("/login", login)
}
