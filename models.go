package gas

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"unicode"

	_ "github.com/lib/pq"
)

var (
	DB        *sql.DB
	stmtCache = make(map[string]*sql.Stmt)
)

const (
	envDBName   = "GAS_DB_NAME"
	envDBParams = "GAS_DB_PARAMS"
)

// Opens and initializes database connection.
//
// No-op if database connection has already been opened.
func InitDB() error {
	if DB != nil {
		return nil
	}

	var (
		dbname = os.Getenv(envDBName)
		params = os.Getenv(envDBParams)
		err    error
	)

	if dbname == "" {
		return fmt.Errorf("InitDB: environment variable %s not set", envDBName)
	}

	if params == "" {
		return fmt.Errorf("InitDB: environment variable %s not set", envDBParams)
	}

	DB, err = sql.Open(dbname, params)
	return err
}

func getStmt(query string) (*sql.Stmt, error) {
	if stmt, ok := stmtCache[query]; ok {
		return stmt, nil
	}
	stmt, err := DB.Prepare(query)
	if err != nil {
		return nil, err
	}
	stmtCache[query] = stmt
	return stmt, nil
}

func toSnake(in string) string {
	if len(in) == 0 {
		return ""
	}

	out := make([]rune, 0, len(in))
	foundUpper := false

	for i, ch := range in {
		if unicode.IsUpper(ch) {
			if i > 0 && !foundUpper {
				out = append(out, '_')
			}
			out = append(out, unicode.ToLower(ch))
			foundUpper = true
		} else {
			foundUpper = false
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
func (self model) visitAll(targetFieldVals *[]interface{}, cols *[]string, val reflect.Value) (continueLooking bool) {
	var thisField reflect.Value

	for i, field := range self.fields {
		if len(*cols) == 0 {
			return
		}

		//		fmt.Printf("+ visiting %s (%s) in search of %s\n  (len(cols) == %d)\n", field.name, field.originalName, (*cols)[0], len(*cols))

		// if the name doesn't match, there's a chance that it's because the
		// target column is in an embedded struct. If the field model is nil,
		// then that isn't the case.
		//
		// If it is indeed the case, we should recurse down into the struct
		// later.

		if !field.match((*cols)[0]) {
			//			fmt.Printf("%s doesn't match %s or %s\n", (*cols)[0], field.name, field.originalName)
			//			println("[match] continueLooking = true")

			if field.model == nil {
				continueLooking = true
				continue
			}
		}

		//		fmt.Printf("%s matches %s or %s\n", (*cols)[0], field.name, field.originalName)

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
			//			println("we have to go deeper")
			// we have to move down the tree

			// if continueLooking is true here that means it was set to true on
			// the last iteration during the attempt to match field names in
			// parent field
			continueLooking = field.model.visitAll(targetFieldVals, cols, thisField)
			//			println("[visit] continue looking =", continueLooking)
		} else {
			// normal value, add as scan destination
			//			fmt.Printf("appending %T\n", thisField.Addr().Interface())
			//Log(Debug, "gas: models: visitAll: matched %s (%s) with %s", field.name, field.originalName, (*cols)[0])
			*targetFieldVals = append(*targetFieldVals, thisField.Addr().Interface())
			continueLooking = false
			//			println("[else] continueLooking = false")
		}

		if !continueLooking {
			if len(*cols) == 1 {
				// last column from query result, stop recursing
				return
			}
			// len(cols) is now guaranteed 2 or more
			//			println("advancing to next column")
			*cols = (*cols)[1:]
			continueLooking = true
		}
	}
	return
}

type field struct {
	originalName string
	name         string
	t            reflect.Type
	*model
}

func newField(s reflect.StructField) (f *field) {
	f = new(field)
	f.t = s.Type
	f.originalName = toSnake(s.Name)
	if tag := s.Tag.Get("sql"); tag != "" {
		f.name = tag
	} else {
		f.name = f.originalName
	}

	// recursively register models
	m, err := Register(s.Type)
	if err != nil {
		// We don't return the error here because an error indicates that there
		// is no model (struct pointer) associated with f and we should just
		// continue on. f is just a regular value.
		return f
	}
	f.model = m
	return f
}

func (self *field) match(column string) bool {
	return self.name == column || self.originalName == column
}

var (
	errNotSliceOrStruct = "gas: models: %T: target is not a pointer to a struct or a slice"
	errNotPtr           = "gas: models: %T: target is not a pointer"
	errNotStruct        = "gas: models: %T: target is not a pointer to a struct"
	errRecursiveType    = "gas: models: %T: cannot register recursive type (this must be dealt with manually)"
	errEmptyStruct      = "gas: models: %T: what's the point of registering an empty struct?"
	errNoRows           = errors.New("gas: QueryRow: no rows returned")
	errBadQueryType     = errors.New("gas: query: query must be either of type string or *sql.Stmt")
)

// Register a model with the system. The reflect.Type should be of a *T or
// *[]T, where T is a struct type.
//
// Register will be called automatically upon first use of a valid type within
// Query and QueryRow if it has not been registered beforehand.
//
// Register searches a struct recursively, looking for embedded structs in the
// process. If there are no embedded structs, nothing special happens. Slices
// or other pointer/reference types count as regular values; they must be
// Scannable with database/sql.
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

	//	fmt.Printf("\nregistering %s\n", t.Name())

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

// Query into a single row or a slice.
func Query(dest interface{}, query string, args ...interface{}) error {
	t := reflect.TypeOf(dest)
	model, err := Register(t)
	if err != nil {
		return err
	}

	// we use Query here instead of QueryRow because Query returns a *sql.Row,
	// which doesn't have a Columns() method. This is weird since sql.Row
	// actually contains a *sql.Rows as a field, but one that is unexported. So
	// we just have to get a Rows and only scan one row. (assuming it returns
	// just one row). This is basically what (*sql.Row).Scan does.
	stmt, err := getStmt(query)
	if err != nil {
		return err
	}

	rows, err := stmt.Query(args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	switch t.Kind() {
	case reflect.Ptr:
		if t.Elem().Kind() != reflect.Struct {
			if t.Elem().Kind() == reflect.Slice {
				return querySlice(model, dest, rows)
			} else {
				return fmt.Errorf(errNotSliceOrStruct, dest)
			}
			return fmt.Errorf(errNotStruct, dest)
		}
		return queryRow(model, dest, rows)

	case reflect.Slice:
		if elem := t.Elem(); elem.Kind() == reflect.Ptr {
			if elem.Elem().Kind() != reflect.Struct {
				return fmt.Errorf(errNotStruct, dest)
			}
		} else if elem.Kind() != reflect.Struct {
			return fmt.Errorf(errNotStruct, dest)
		}
		return querySlice(model, dest, rows)

	default:
		return fmt.Errorf(errNotSliceOrStruct, dest)
	}
}

// Query a single row into a struct. For simple primitive types, use database/sql.
func queryRow(model *model, dest interface{}, rows *sql.Rows) error {
	val := reflect.ValueOf(dest).Elem()

	if !rows.Next() {
		return errNoRows
	}

	_, err := model.scan(val, rows, 0)
	return err
}

// Query multiple rows into a slice.
func querySlice(model *model, slice interface{}, rows *sql.Rows) error {
	// var slice *[]T
	// sliceVal := *slice
	var (
		sliceVal = reflect.ValueOf(slice).Elem()
		foundCap = 0
		err      error
	)
	// first, populate the existing allocated elements in the slice. If it's an
	// empty slice, this loop will effectively be skipped.
	for i := 0; i < sliceVal.Len() && rows.Next(); i++ {
		if foundCap, err = model.scan(sliceVal.Index(i), rows, foundCap); err != nil {
			return err
		}
	}

	// now start scanning into new values and append to the slice. If we're
	// already done, this loop will effectively be skipped.
	// var sliceElemType type = T
	sliceElemType := sliceVal.Type().Elem()
	for rows.Next() {
		val := reflect.New(sliceElemType)
		if _, err := model.scan(val, rows, foundCap); err != nil {
			return err
		}
		sliceVal.Set(reflect.Append(sliceVal, val.Elem()))
	}

	return nil
}
