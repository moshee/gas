package gas

import (
	"log"
	"reflect"
	"time"
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

var g *Gas

type myAuther struct{}

func (myAuther) CreateSession(a []byte, t time.Time, s string) error { return nil }
func (myAuther) ReadSession(id []byte) (*Session, error)             { return nil, nil }
func (myAuther) UpdateSession(id []byte) error                       { return nil }
func (myAuther) DeleteSession(id []byte) error                       { return nil }
func (myAuther) UserAuthData(name string) ([]byte, []byte, error)    { return nil, nil, nil }
func (myAuther) User(name string) (User, error)                      { return nil, nil }
func (myAuther) NilUser() User                                       { return nil }

func ExampleUseCookies() {
	// During app init
	UseCookies(myAuther{})

	// to sign a user in
	if err := g.SignIn(); err != nil {
		g.Error(500, err)
	}

	// to sign them out
	if err := g.SignOut(); err != nil {
		g.Error(500, err)
	}
}

func ExampleNotify() {
	// Buffer optional, but maybe needed for high traffic
	ch := make(chan interface{}, 5)

	// Listen for events on default host
	Notify("", ch)

	// You can add the same channel to multiple hosts
	Notify("dl.displaynone.us", ch)

	// Listen for events
	go func() {
		for {
			switch event := (<-ch).(type) {
			case *Panic:
				log.Fatalf("ohnoez! %v\n", event)
			case *HTTPRequest:
				log.Printf("Request on %s\n", event.URL.Path)
			}
		}
	}()
}
