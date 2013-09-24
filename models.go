package gas

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"reflect"
	"strings"
)

type scanner interface {
	Scan(dest ...interface{}) error
}

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
func (self model) scan(val reflect.Value, row scanner, foundCap int) (int, error) {
	cols, err := row.Columns()
	if err != nil {
		return err
	}

	// cut down on allocations by passing the cap back in when scanning into a
	// slice. Values will still be appended but it will not have to grow. If
	// foundCap == 0, nothing special will happen.
	targetFieldVals := make([]reflect.Value, 0, foundCap)
	self.visitAll(targetFieldVals, cols, val, 0)

	foundCap = len(targetFieldVals)
	return foundCap, nil
}

// recursively populate a list of scan destinations
//
// targetFieldVals is the slice of scan destinations. They can be pointing to
// any arbitrary depth of struct field, flattened out.
//
// cols is the column names returned in the query.
//
// val is the root value that holds the struct waiting to be scanned into.
//
// fieldIndex is the current field index in the parent value, if we have
// recursed downwards in the type tree.
func (self model) visitAll(targetFieldVals []reflect.Value, cols []string, val reflect.Value, fieldIndex int) {
	for i, field := range self.fields {
		if len(cols) == 0 {
			return
		}
		if !field.Match(cols[0]) {
			continue
		}
		if field.model != nil {
			// we have to move down the tree
			field.model.visitAll(targetFieldVals, cols, val, i)
		} else {
			// normal value, add as scan destination
			targetFieldVals = append(targetFieldVals, val.Field(fieldIndex).Field(i))
		}
		if len(cols) == 1 {
			// last column from query result, stop recursing
			return
		}
		// len(cols) is now guaranteed 2 or more
		cols = cols[1:]
	}
}

type field struct {
	name string
	t    reflect.Type
	*model
	parent *model
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
	if m, err := Register(s.Type); err != nil {
		return f
	}
	f.model = m
	return f
}

func (self *field) match(column string) bool {
	return self.name == column
}

var (
	errNotPtr        = errors.New("gas: register model: target is not a pointer")
	errNotStruct     = errors.New("gas: register model: target is not a pointer to a struct")
	errRecursiveType = errors.New("gas: register model: cannot register recursive type (this must be dealt with manually)")
	errNotSlice      = errors.New("gas: query: destination is not a pointer to a slice")
)

// Register a model with the system. The reflect.Type should be of a *T or
// *[]T, where T is a struct type.
//
// Register will be called automatically upon first use of a valid type within
// Query and QueryRow if it has not been registered beforehand.
func Register(t reflect.Type) (*model, error) {
	if m, ok := modelCache[t]; ok {
		return m, nil
	}

	if t.Kind() != reflect.Ptr {
		return nil, errNotPtr
	}

	elem := t.Elem()
	switch elem.Kind() {
	case reflect.Slice:
		elem = elem.Elem()
	case reflect.Struct:
		// continue
	default:
		return nil, errNotStruct
	}

	m := new(model)
	numField := elem.NumField()
	if numField == 0 {
		return nil, fmt.Errorf("gas: register %T: what's the point of registering an empty struct?")
	}
	m.fields = make([]*field, numField)

	for i := 0; i < numField; i++ {
		structField := elem.Field(i)
		if structField.Type == elem {
			return nil, errRecursiveType
		}
		f := newField(structField)
		f.parent = m
		m.fields[i] = f
	}

	return m, nil
}

func QueryRow(dest interface{}, query string, args ...interface{}) error {
	model, err := Register(reflect.TypeOf(dest))
	if err != nil {
		return err
	}
	row, err := DB.QueryRow(query, args...)
	if err != nil {
		return err
	}

	val := reflect.ValueOf(dest).Elem()
	_, err = model.scan(val, row, 0)
	return err
}

func Query(slice interface{}, query string, args ...interface{}) error {
	t := reflect.TypeOf(dest)
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
	for i := 0; i < sliceVal.Len(); i++ {
		if foundCap, err = model.scan(sliceVal.Index(i), rows, foundCap); err != nil {
			return err
		}
	}

	return nil
}
