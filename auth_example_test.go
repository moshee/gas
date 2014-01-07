package gas

type M map[string]interface{}

type myUser struct{}

func (*myUser) Username() string                 { return "" }
func (*myUser) Secrets() ([]byte, []byte, error) { return nil, nil, nil }
func (*myUser) byUsername(n string) *myUser      { return nil }

func ExampleRouter() {
	// A simple "static" route.
	loginForm := func(g *Gas) {
		g.Render("example", "login-form", nil)
	}

	// JSON REST? Sure.
	login := func(g *Gas) {
		u := new(myUser).byUsername(g.FormValue("user"))
		if err := g.SignIn(u); err != nil {
			g.WriteHeader(403)
			g.JSON(M{"error": err.Error()})
		} else {
			g.WriteHeader(204)
		}
	}

	// Reroute users (+ a cookie with the path data) if not logged in
	checkLogin := func(path string) func(g *Gas) {
		return func(g *Gas) {
			if sess, err := g.Session(); sess == nil || err != nil {
				g.Reroute(path, 303, map[string]string{"path": g.URL.Path})
			} else {
				g.SetData("user", new(myUser).byUsername(sess.Username))
			}
		}
	}

	// A page behind the login wall
	profile := func(g *Gas) {
		user := g.Data("user").(*myUser)
		g.Render("example", "profile", user)
	}

	// The router
	New("asdf").
		Get("/profile", checkLogin("/login"), profile).
		Get("/login", loginForm).
		Post("/login", login)
}
