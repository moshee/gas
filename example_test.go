package gas

import "fmt"

type T struct { Id int }

func ExampleRegister() {
	t := new(T)
	gas.Register(t)
}

func ExampleSelectRow() {
	t := new(T)
	_, err := gas.SelectRow(t, "SELECT id FROM users WHERE name = $1", "moshee")
	if err != nil {
		// Error during query or type marshaling
	}
	fmt.Println(t.Id)
	// Output: 2
}
