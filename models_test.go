package gas

import (
	//	"database/sql"
	_ "github.com/lib/pq"
	"reflect"
	"testing"
	"time"
)

type Tester struct {
	TextField          string
	B                  int
	SomeSortOfDateTime time.Time `sql:"c"`
}

type Tester2 struct {
	FieldA int
	*Tester3
}

type Tester3 struct {
	*Tester4
}

type Tester4 struct {
	FieldB int `sql:"b"`
}

type Tester5 struct {
	FirstColumn string
	NotIncluded string
	ThirdColumn string `sql:"test"`
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

func TestDBRegister(t *testing.T) {
	test := new(Tester)
	model, err := Register(reflect.TypeOf(test))
	if err != nil {
		t.Fatal(err)
	}

	if len(model.fields) != 3 {
		t.Fatal("wrong number of model fields")
	}
}

func TestDBQuery(t *testing.T) {
	InitDB("postgres", "user=postgres dbname=postgres sslmode=disable")
	defer DB.Close()

	dateFmt := "2006-01-02 15:04:05 -0700"
	t1, _ := time.Parse(dateFmt, "2013-09-24 17:27:00 -0700")
	t2, _ := time.Parse(dateFmt, "2012-12-12 12:12:12 -0800")

	exec(t, "CREATE TEMP TABLE go_test ( text_field text, b integer, c timestamptz )")
	exec(t, "INSERT INTO go_test VALUES ( 'testing', 9001, '2013-09-24 17:27:00-07' )")
	exec(t, "INSERT INTO go_test VALUES ( 'testing 2', 666, '2012-12-12 12:12:12-08' )")

	test := new(Tester)
	err := QueryRow(test, "SELECT * FROM go_test LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	match(t, test, "testing", 9001, t1)

	test2 := make([]Tester, 2)
	err = Query(&test2, "SELECT * FROM go_test")
	if err != nil {
		t.Fatal(err)
	}
	match(t, &test2[0], "testing", 9001, t1)
	match(t, &test2[1], "testing 2", 666, t2)

	// embedded
	exec(t, "CREATE TEMP TABLE go_test_2 ( field_a integer, b integer )")
	exec(t, "INSERT INTO go_test_2 VALUES ( 10, 66 )")

	test3 := new(Tester2)
	err = QueryRow(test3, "SELECT * FROM go_test_2 LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}

	if test3.FieldA != 10 || test3.Tester3.Tester4.FieldB != 66 {
		t.Error("fail: embedded structs")
	}

	// missing fields in target struct
	exec(t, "CREATE TEMP TABLE go_test_3 ( first_column text, not_included text, third_column text )")
	exec(t, "INSERT INTO go_test_3 VALUES ( 'first', 'nope', 'third' )")

	test4 := new(Tester5)
	err = QueryRow(test4, "SELECT first_column, third_column AS test FROM go_test_3 LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if test4.FirstColumn != "first" || test4.ThirdColumn != "third" {
		t.Error("fail: missing fields in target struct")
	}
}
