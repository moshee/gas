package gas

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

type Tester struct {
	TextField          string
	B                  int
	SomeSortOfDateTime time.Time `sql:"c"`
}

type Tester2 struct {
	FieldA int
	*Tester3
	*Tester4
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

type A struct {
	Id   int
	Data int
	Bs   []*B
}

type B struct {
	Id   int
	AId  int `sql:"a_id"`
	Data float64
	Cs   []C
}

type C struct {
	Id   int
	BId  int `sql:"b_id"`
	Data sql.NullString
}

func match(t *testing.T, test *Tester, a string, b int, c time.Time) {
	ts := test.SomeSortOfDateTime
	if test.TextField != a ||
		test.B != b ||
		ts.Hour() != c.Hour() ||
		ts.Minute() != c.Minute() ||
		ts.Second() != c.Second() ||
		ts.Day() != c.Day() ||
		ts.Month() != c.Month() ||
		ts.Year() != c.Year() {
		t.Errorf("Got: %v\nExpected: %v %v %v", test, a, b, c)
	}
}

func exec(t *testing.T, query string) {
	_, err := DB.Exec(query)
	if err != nil {
		t.Logf("in query: %s", query)
		t.Fatal(err)
	}
}

func TestCamelToSnake(t *testing.T) {
	try := func(camel, snake string) {
		if got := toSnake(camel); got != snake {
			t.Errorf("expected '%s', got '%s'", snake, got)
		}
	}
	for _, test := range []struct{ camel, snake string }{
		{"A", "a"},
		{"AId", "aid"},
		{"MacBookPro", "mac_book_pro"},
		{"ABC", "abc"},
		{"OneTwoThreeFour", "one_two_three_four"},
		{"", ""},
	} {
		try(test.camel, test.snake)
	}
}

func TestDBInit(t *testing.T) {
	if err := InitDB(); err != nil {
		t.Error(err)
	}

	if DB == nil {
		t.Error("DB is nil")
	} else {
		DB.Close()
		DB = nil
	}
}

func TestDBRegister(t *testing.T) {
	if err := InitDB(); err != nil {
		t.Error(err)
	}

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
	if err := InitDB(); err != nil {
		t.Error(err)
	}

	defer DB.Close()

	dateFmt := "2006-01-02 15:04:05"
	t1, _ := time.Parse(dateFmt, "2013-09-24 17:27:00")
	t2, _ := time.Parse(dateFmt, "2012-12-12 12:12:12")

	exec(t, "CREATE TEMP TABLE go_test ( text_field text, b integer, c timestamp )")
	exec(t, "INSERT INTO go_test VALUES ( 'testing', 9001, '2013-09-24 17:27:00' )")
	exec(t, "INSERT INTO go_test VALUES ( 'testing 2', 666, '2012-12-12 12:12:12' )")

	test := new(Tester)
	err := Query(test, "SELECT * FROM go_test LIMIT 1")
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
	err = Query(test3, "SELECT field_a, b, b FROM go_test_2 LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}

	if test3.FieldA != 10 || test3.Tester3.Tester4.FieldB != 66 || test3.Tester4.FieldB != 66 {
		t.Error("fail: embedded structs")
	}

	// missing fields in target struct
	exec(t, "CREATE TEMP TABLE go_test_3 ( first_column text, not_included text, third_column text )")
	exec(t, "INSERT INTO go_test_3 VALUES ( 'first', 'nope', 'third' )")

	test4 := new(Tester5)
	err = Query(test4, "SELECT first_column, third_column AS test FROM go_test_3 LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if test4.FirstColumn != "first" || test4.ThirdColumn != "third" {
		t.Error("fail: missing fields in target struct")
	}

	// joins
	exec(t, `CREATE TEMP TABLE a (
		id   serial PRIMARY KEY,
		data int    NOT NULL
	)`)
	exec(t, `CREATE TEMP TABLE b (
		id   serial PRIMARY KEY,
		a_id int    NOT NULL REFERENCES a,
		data float8 NOT NULL
	)`)
	exec(t, `CREATE TEMP TABLE c (
		id   serial PRIMARY KEY,
		b_id int    NOT NULL REFERENCES b,
		data text   NOT NULL
	)`)
	exec(t, `INSERT INTO a(data) VALUES (1),(3),(5)`)
	exec(t, `INSERT INTO b(a_id, data) SELECT id, (data^2)::float8 as data FROM a`)
	exec(t, `INSERT INTO b(a_id, data) SELECT id, (data^3)::float8 as data FROM a`)
	exec(t, `INSERT INTO b(a_id, data) SELECT id, (data^4)::float8 as data FROM a`)
	exec(t, `INSERT INTO c(b_id,data) SELECT id, data::text FROM b`)
	exec(t, `INSERT INTO a(data) VALUES (7)`)

	a := make([]*A, 0, 3)
	if err := QueryJoin(&a, "SELECT * FROM a LEFT JOIN b ON a.id = b.a_id LEFT JOIN c ON c.b_id = b.id ORDER BY a.id, b.id, c.id"); err != nil {
		t.Fatal(err)
	}

	assertEqual := func(a, b interface{}) {
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("want %T %[1]v != got %T %[2]v", b, a)
		}
	}

	assertEqual(len(a), 4)
	assertEqual(a[0].Data, 1)
	//fmt.Printf("%#v\n", a[0].Bs)
	assertEqual(len(a[0].Bs), 3)
	assertEqual(a[1].Bs[2].Data, 81.0)
	//fmt.Printf("%#v\n", a[0].Bs[0])
	assertEqual(len(a[0].Bs[0].Cs), 1)
	assertEqual(a[2].Bs[2].Cs[0].Data.String, "625")
	assertEqual(a[3].Data, 7)
	assertEqual(a[3].Bs, []*B(nil))
}
