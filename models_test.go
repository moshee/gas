package gas

import (
	//	"database/sql"
	_ "github.com/lib/pq"
	"testing"
	"time"
)

type Tester struct {
	TextField          string
	B                  int
	SomeSortOfDateTime time.Time `sql:"c"`
}

func match(t *testing.T, test *Tester, a string, b int, c time.Time) {
	if test.TextField != a || test.B != b || test.SomeSortOfDateTime != c {
		t.Error(test)
	}
}

func exec(t *testing.T, query string) {
	_, err := DB.Exec(query)
	if err != nil {
		t.Fatal(err)
	}
}

func TestQuery(t *testing.T) {
	InitDB("postgres", "user=postgres dbname=postgres sslmode=disable")
	defer DB.Close()

	exec(t, "CREATE TEMP TABLE go_test ( text_field text, b integer, c timestamptz )")
	exec(t, "INSERT INTO go_test VALUES ( 'testing', 9001, '2013-09-24 17:27:00 PST' )")
	exec(t, "INSERT INTO go_test VALUES ( 'testing 2', 666, '2012-12-12 12:12:12 PST' )")

	test := new(Tester)
	err := QueryRow(test, "SELECT * FROM go_test LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	match(t, test, "testing", 9001, time.Date(2013, time.September, 24, 17, 27, 0, 0, time.Local))

	test2 := []Tester{}
	err = Query(&test2, "SELECT * FROM go_test")
	if err != nil {
		t.Fatal(err)
	}
	match(t, &test2[0], "testing", 9001, time.Date(2013, time.September, 24, 17, 27, 0, 0, time.Local))
	match(t, &test2[1], "testing 2", 666, time.Date(2012, time.December, 12, 12, 12, 12, 12, time.Local))
}
