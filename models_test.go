package gas

import (
//	"database/sql"
	_ "github.com/bmizerany/pq"
	"testing"
	"time"
)

type Tester struct {
	A string
	B int
	C time.Time
}

func match(t *testing.T, test *Tester, a string, b int, c time.Time) {
	if test.A != a || test.B != b || test.C != c {
		t.Error(test)
	}
}

func TestQuery(t *testing.T) {
	test := new(Tester)
	err := SelectRow(test, "SELECT * FROM test LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	match(t, test, "testing", 9001, time.Date(2013, time.January, 15, 12, 30, 0, 0, time.Local))

	test2 := []*Tester{}
	//test2 := make([]*Tester, 2)
	/*
	test2[0] = new(Tester)
	test2[1] = new(Tester)
	*/
	_, err = SelectRows(&test2, "SELECT * FROM test")
	if err != nil {
		t.Fatal(err)
	}
	match(t, test2[0], "testing", 9001, time.Date(2013, time.January, 15, 12, 30, 0, 0, time.Local))
	match(t, test2[1], "testing 2", 666, time.Date(2013, time.January, 16, 13, 25, 0, 0, time.Local))
}
