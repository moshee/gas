package gas

import (
	"reflect"
)

//import "fmt"

type T struct{ Id int }

func ExampleRegister() {
	t := new(T)
	Register(reflect.TypeOf(t))
}

/*
func ExampleSelectRow() {
	t := new(T)
	_, err := SelectRow(t, "SELECT id FROM users WHERE name = $1", "moshee")
	if err != nil {
		// Error during query or type marshaling
	}
	fmt.Println(t.Id)
	// Output: 2
}
*/

var g *gas.Gas

type mySessionStore struct{}

func (mySessionStore) CreateSession(a, b []byte, t time.Time, s string) error { return nil }
func (mySessionStore) ReadSession(name, id []byte) (*Session, error)          { return nil, nil }
func (mySessionStore) UpdateSession(name, id []byte) error                    { return nil }
func (mySessionStore) DeleteSession(name, id []byte) error                    { return nil }
func (mySessionStore) UserAuthData(string) (pass, salt []byte, err error)     { return nil, nil, nil }

func ExampleUseCookies() {
	// During app init
	store := mySessionStore{}
	UseCookies(store)

	// to sign a user in
	if err := g.SignIn(); err != nil {
		g.Error(500, err)
	}

	// to sign them out
	if err := g.SignOut(); err != nil {
		g.Error(500, err)
	}
}
