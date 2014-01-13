package gas

import "reflect"

type T struct{ Id int }

func ExampleRegister() {
	t := new(T)
	Register(reflect.TypeOf(t))
}
