package gas

import (
	"log"
	"reflect"
)

type T struct{ Id int }

func ExampleRegister() {
	t := new(T)
	Register(reflect.TypeOf(t))
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
