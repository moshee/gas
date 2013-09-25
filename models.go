package gas

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"reflect"
	"unicode"
)

var DB *sql.DB

// Opens and initializes database connection.
//
// No-op if database connection has already been opened.
func InitDB(dbname, params string) {
	if DB != nil {
		return
	}
	var err error
	DB, err = sql.Open(dbname, params)
	if err != nil {
		panic(err)
	}
	DB.SetMaxIdleConns(*flag_db_idle_conns)
	DB.SetMaxOpenConns(*flag_db_conns)
}

func toSnake(in string) string {
	if len(in) == 0 {
		return ""
	}
	out := make([]rune, 0, len(in))
	for i, ch := range in {
		if unicode.IsUpper(ch) {
			if i > 0 {
				out = append(out, '_')
			}
			out = append(out, unicode.ToLower(ch))
		} else {
			out = append(out, ch)
		}
	}
	return string(out)
}

var modelCache = make(map[reflect.Type]model)

type model struct {
	fields []*field
}

// val must be a pointer to a struct
func (self model) scan(val reflect.Value, row *sql.Rows, foundCap int) (int, error) {
	cols, err := row.Columns()
	if err != nil {
		return 0, err
	}

	// cut down on allocations by passing the cap back in when scanning into a
	// slice. Values will still be appended but it will not have to grow. If
	// foundCap == 0, nothing special will happen.
	targetFieldVals := make([]interface{}, 0, foundCap)
	self.visitAll(&targetFieldVals, &cols, val)
	foundCap = len(targetFieldVals)

	return foundCap, row.Scan(targetFieldVals...)
}

// recursively populate a list of scan destinations
//
// targetFieldVals is the slice of scan destinations. They can be pointing to
// any arbitrary depth of struct field, flattened out.
//
// cols is the column names returned in the query.
//
// val is the root value that holds the struct waiting to be scanned into.
func (self model) visitAll(targetFieldVals *[]interface{}, cols *[]string, val reflect.Value) {
	var thisField reflect.Value

	for i, field := range self.fields {
		if len(*cols) == 0 {
			return
		}
		if !field.match((*cols)[0]) {
			continue
		}

		if val.Kind() == reflect.Ptr {
			thisField = val.Elem().Field(i)
		} else {
			thisField = val.Field(i)
		}

		if field.t.Kind() == reflect.Ptr {
			if thisField.IsNil() {
				thisField.Set(reflect.New(field.t.Elem()))
			}
		}

		if field.model != nil {
			// we have to move down the tree
			field.model.visitAll(targetFieldVals, cols, thisField)
		} else {
			// normal value, add as scan destination
			*targetFieldVals = append(*targetFieldVals, thisField.Addr().Interface())
		}
		if len(*cols) == 1 {
			// last column from query result, stop recursing
			return
		}
		// len(cols) is now guaranteed 2 or more
		*cols = (*cols)[1:]
	}
}

type field struct {
	name string
	t    reflect.Type
	*model
}

func newField(s reflect.StructField) (f *field) {
	f = new(field)
	f.t = s.Type
	if tag := s.Tag.Get("sql"); tag != "" {
		f.name = tag
	} else {
		f.name = toSnake(s.Name)
	}

	// recursively register models
	m, err := Register(s.Type)
	if err != nil {
		return f
	}
	f.model = m
	return f
}

func (self *field) match(column string) bool {
	return self.name == column
}

var (
	errNotPtr        = "gas: register %T: target is not a pointer"
	errNotStruct     = "gas: register %T: target is not a pointer to a struct"
	errRecursiveType = "gas: register %T: cannot register recursive type (this must be dealt with manually)"
	errEmptyStruct   = "gas: register %T: what's the point of registering an empty struct?"
	errNotSlice      = errors.New("gas: query: destination is not a pointer to a slice")
)

// Register a model with the system. The reflect.Type should be of a *T or
// *[]T, where T is a struct type.
//
// Register will be called automatically upon first use of a valid type within
// Query and QueryRow if it has not been registered beforehand.
func Register(t reflect.Type) (*model, error) {
	if m, ok := modelCache[t]; ok {
		return &m, nil
	}

	if t.Kind() != reflect.Ptr {
		return nil, fmt.Errorf(errNotPtr, t)
	}

	elem := t.Elem()
	switch elem.Kind() {
	case reflect.Slice:
		elem = elem.Elem()
	case reflect.Struct:
		// continue
	default:
		return nil, fmt.Errorf(errNotStruct, t)
	}

	m := new(model)
	numField := elem.NumField()
	if numField == 0 {
		return nil, fmt.Errorf(errEmptyStruct, t)
	}
	m.fields = make([]*field, numField)

	for i := 0; i < numField; i++ {
		structField := elem.Field(i)
		if structField.Type == elem {
			return nil, fmt.Errorf(errRecursiveType, t)
		}
		f := newField(structField)
		m.fields[i] = f
	}

	return m, nil
}

func QueryRow(dest interface{}, query string, args ...interface{}) error {
	model, err := Register(reflect.TypeOf(dest))
	if err != nil {
		return err
	}

	// we use Query here instead of QueryRow because Query returns a *sql.Row,
	// which doesn't have a Columns() method. This is weird since sql.Row
	// actually contains a *sql.Rows as a field, but one that is unexported. So
	// we just have to get a Rows and only scan one row. (assuming it returns
	// just one row). This is basically what (*sql.Row).Scan does.
	rows, err := DB.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	val := reflect.ValueOf(dest).Elem()

	rows.Next()
	_, err = model.scan(val, rows, 0)
	return err
}

func Query(slice interface{}, query string, args ...interface{}) error {
	t := reflect.TypeOf(slice)
	if t.Kind() != reflect.Ptr && t.Elem().Kind() != reflect.Slice {
		return errNotSlice
	}

	model, err := Register(t)
	if err != nil {
		return err
	}

	rows, err := DB.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	sliceVal := reflect.ValueOf(slice).Elem()
	foundCap := 0
	for i := 0; i < sliceVal.Len() && rows.Next(); i++ {
		if foundCap, err = model.scan(sliceVal.Index(i), rows, foundCap); err != nil {
			return err
		}
	}

	return nil
}
