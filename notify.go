package gas

import (
	"time"
)

// A map of site names to channels. If a channel exists in listeners addressed
// by a site name, all events from that site will be directed towards the
// channel. Otherwise, that site's events will be ignored. A site is specified
// as a host name. The empty host is a valid site, the default (empty) host.
var listeners map[string]chan interface{}

// Instruct the package to send events for the named site to the given channel.
func Notify(name string, ch chan interface{}) {
	if listeners == nil {
		listeners = make(map[string]chan interface{})
	}
	listeners[name] = ch
}

func notify(name string, event interface{}) {
	if ch, ok := listeners[name]; ok {
		ch <- event
	}
}

// TODO: send parsed stacks with the panic

type Panic struct {
	Error interface{}
	Time  time.Time
	*Gas
}

type HTTPRequest struct {
	Elapsed time.Duration
	Time    time.Time
	*Gas
}
