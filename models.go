package gas

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"unicode"

	_ "github.com/lib/pq"
)

var (
	DB                  *sql.DB
	stmtCache           = make(map[string]*sql.Stmt)
	errNotSliceOrStruct = "gas: models: %T: target is not a pointer to a struct or a slice"
	errNotPtr           = "gas: models: %T: target is not a pointer"
	errNotStruct        = "gas: models: %T: target is not a pointer to a struct"
	errRecursiveType    = "gas: models: %T: cannot register recursive type (this must be dealt with manually)"
	errEmptyStruct      = "gas: models: %T: what's the point of registering an empty struct?"
	errNoRows           = errors.New("gas: QueryRow: no rows returned")
	errBadQueryType     = errors.New("gas: query: query must be either of type string or *sql.Stmt")
)

// Opens and initializes database connection.
//
// No-op if database connection has already been opened.
func InitDB() error {
	if DB != nil {
		return nil
	}

	var err error
	DB, err = sql.Open(Env.DB_NAME, Env.DB_PARAMS)
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

		// if the name doesn't match, there's a chance that it's because the
		// target column is in an embedded struct. If the field model is nil,
		// then that isn't the case.
		//
		// If it is indeed the case, we should recurse down into the struct
		// later.

		if !field.match((*cols)[0]) {
			if field.model == nil {
				continueLooking = true
				continue
			}
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

			// if continueLooking is true here that means it was set to true on
			// the last iteration during the attempt to match field names in
			// parent field
			continueLooking = field.model.visitAll(targetFieldVals, cols, thisField)
		} else {
			// normal value, add as scan destination
			*targetFieldVals = append(*targetFieldVals, thisField.Addr().Interface())
			continueLooking = false
		}

		if !continueLooking {
			if len(*cols) == 1 {
				// last column from query result, stop recursing
				return
			}
			// len(cols) is now guaranteed 2 or more
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
	x := self.name == column || self.originalName == column
	//	fmt.Printf("%s (%s) == %s? %v\n", self.name, self.originalName, column, x)
	return x
}

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

// Recursively populate a destination struct or slice of structs with an
// arbitrarily deeply nested one-to-many query row set. Check out
// models_test.go for exactly what this means.
//
// Caveats
//
// Scanning into a struct pointer is unimplemented and will return an error.
//
// A slice must be of pointers to structs, and the address of the slice must be
// passed in.
//
// The structs of the slice must each have a slice field at the end with their
// own slices of pointers to structs, etc.
func QueryJoin(dest interface{}, query string, args ...interface{}) error {
	t := reflect.TypeOf(dest)
	if t.Kind() != reflect.Ptr {
		return fmt.Errorf(errNotPtr, dest)
	}
	t = t.Elem()

	var f func(reflect.Type, interface{}, *sql.Rows) error

	switch t.Kind() {
	case reflect.Slice:
		t = t.Elem()
		// handle []*T
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return fmt.Errorf(errNotStruct, dest)
		}
		f = queryJoinSlice

	case reflect.Struct:
		f = queryJoinStruct

	default:
		return fmt.Errorf(errNotSliceOrStruct, dest)
	}

	stmt, err := getStmt(query)
	if err != nil {
		return err
	}
	rows, err := stmt.Query(args...)
	if err != nil {
		return err
	}

	return f(t, dest, rows)
}

func queryJoinStruct(t reflect.Type, dest interface{}, rows *sql.Rows) error {
	return errors.New("unimplemented")
}

// Recursively populate dest with the data from rows, using t as a template to
// generate a flat slice of destinations to rows.Scan into. After that, the
// values from the flat slice will be recursively copied into dest, appending
// new values to dest, its children's children, etc. as needed.
func queryJoinSlice(t reflect.Type, dest interface{}, rows *sql.Rows) error {
	dests, idIndexes := getDests(t)
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("queryJoinSlice: %v", err)
	}

	dv := reflect.ValueOf(dest)

	for rows.Next() {
		rows.Scan(dests...)
		err = insertIntoTree(dv, dests, idIndexes, columns)
		if err != nil {
			return err
		}
		if err = rows.Err(); err != nil {
			return err
		}
	}

	return nil
}

// Recursively flatten the types of each element in the tree to be used for
// scan destinations. Implementation: cache this so it doesn't have to be
// done every time? Use the *sql.Stmt as a map key?
func getDests(t reflect.Type) (dests []interface{}, idIndexes []int) {
	//fmt.Printf("getDests(%v)\n", t)
	i := 0
	dests = make([]interface{}, 0)
	idIndexes = make([]int, 0)
	var f func(t reflect.Type)

	f = func(t reflect.Type) {
		//fmt.Printf("getDests.f(%v)\n", t)
		for j := 0; j < t.NumField(); j++ {
			field := t.Field(j)
			fieldType := field.Type

			switch fieldType.Kind() {
			case reflect.Slice:
				// a: []*T
				// b: []T
				if fieldType.Elem().Kind() == reflect.Ptr {
					fieldType = fieldType.Elem()
				}
				// a: *T
				// b: []T
				fallthrough
			case reflect.Ptr:
				// a: *T
				// b: []T
				elem := fieldType.Elem()
				if elem.Kind() == reflect.Struct {
					// a: T
					// b: T
					f(elem)
					break
				}
				fallthrough
			default:
				if field.Name == "Id" {
					switch fieldType.Kind() {
					case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
						reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
						idIndexes = append(idIndexes, i)
					}
				}
				dests = append(dests, reflect.New(fieldType).Interface())
			}
			i++
		}
	}

	f(t)
	return
}

// Search for an element in the dest slice where the primary key of the object
// in the returned row matches. If it doesn't, allocate a new object of the
// appropriate type and tack it onto the end of the dest slice.
//
// ASSUMPTIONS:
// - there is a primary key of type int called "Id" in every object
// - the primary key is the first field of every object
// - (maybe) the slice is the last field of every object
// - (maybe) there is only one slice per object
func insertIntoTree(dest reflect.Value, data []interface{}, idIndexes []int, columns []string) error {
	//fmt.Printf("insertIntoTree(%v, %v, %v, %v)\n", dest, data, idIndexes, columns)
	if len(idIndexes) == 0 {
		return nil
	}
	dest = reflect.Indirect(dest)

	var (
		i          = idIndexes[0]
		id         = reflect.Indirect(reflect.ValueOf(data[i])).Interface() // *int to int
		destType   = dest.Type()
		obj, found = searchForId(dest, id) // ASSUMPTION: dest is a slice
	)

	if !found {
		elem := destType.Elem()
		if elem.Kind() == reflect.Ptr {
			obj = reflect.New(elem.Elem())
			if err := copyRowData(obj, data[i:], columns[i:]); err != nil {
				return err
			}
			//fmt.Printf("appending %#v to %#v\n", obj.Interface(), dest.Interface())
			// append *T to []*T (obj is a *T)
			dest.Set(reflect.Append(dest, obj))
		} else {
			obj = reflect.New(elem)
			if err := copyRowData(obj, data[i:], columns[i:]); err != nil {
				return err
			}
			//fmt.Printf("appending %#v to %#v\n", obj.Elem().Interface(), dest.Interface())
			// indirect pointer and append T to []T
			dest.Set(reflect.Append(dest, obj.Elem()))
		}
	}

	if len(idIndexes) > 1 {
		//j := idIndexes[1]
		var err error
		dest, err = getChildren(obj)
		if err != nil {
			return err
		}

		// Advance the "viewing window" on the primary key indexes.
		idIndexes = idIndexes[1:]

		return insertIntoTree(dest, data, idIndexes, columns)
	}

	return nil
}

// return ptr to obj found with id
// if we can assume the IDs are sorted, then use binary search
func searchForId(dest reflect.Value, id interface{}) (obj reflect.Value, found bool) {
	//fmt.Printf("searchForId(%#v, %#v), len %d\n", dest, id, dest.Len())
	for i := 0; i < dest.Len(); i++ {
		obj := reflect.Indirect(dest.Index(i))
		//fmt.Printf("--- comparing %#v (%[1]T) and %#v\n", obj.Field(0).Interface(), id)
		if obj.NumField() > 0 && reflect.DeepEqual(obj.Field(0).Interface(), id) {
			//fmt.Printf("--> %v\n", obj)
			return obj.Addr(), true
		}
	}
	return
}

// Recursively copy data into fields IGNORING the slice.
// The number of columns corresponding to this object should be no greater than
// the number of the object's fields. In addition, the column names should
// match up to the order of the returned columns from the database as passed in
// by `data`.
func copyRowData(obj reflect.Value, data []interface{}, columns []string) error {
	//fmt.Printf("copyRowData(%v, %v, %v)\n", obj, data, columns)
	colIndex := 0

	var f func(val reflect.Value) error

	f = func(val reflect.Value) error {
		//fmt.Printf("copyRowData.f(%v)\n", val)
		typ := val.Type()
		model, err := Register(typ)
		if err != nil {
			return err
		}
		if typ.Kind() == reflect.Ptr {
			val = reflect.Indirect(val)
			typ = val.Type()
		}
		for i := 0; i < typ.NumField(); i++ {
			fieldType := typ.Field(i).Type
			mf := model.fields[i]
			col := columns[colIndex]
			if !mf.match(col) {
				continue
			}
			fieldVal := val.Field(i)

			switch fieldType.Kind() {
			case reflect.Slice:
				// ignore slices
			case reflect.Ptr:
				fieldVal = reflect.Indirect(fieldVal)

				if fieldVal.Kind() == reflect.Struct {
					colIndex++
					return f(fieldVal)
				}
				fallthrough

			default:
				/*
					fmt.Printf("setting %s (%#v) to %#v\n", typ.Field(i).Name, fieldVal, data[colIndex])
					if n, ok := data[colIndex].(*int); ok {
						fmt.Printf("--- value is %d\n", *n)
					}
				*/
				fieldVal.Set(reflect.Indirect(reflect.ValueOf(data[colIndex])))
			}
			colIndex++
		}

		return nil
	}

	return f(obj)
}

// find and return the last slice in the struct, or an error if there are no
// slices. If the slice is nil (which it probably is), make a new slice and set
// it.
// make sure obj is a pointer, or else the fields will not be settable
func getChildren(obj reflect.Value) (reflect.Value, error) {
	obj = reflect.Indirect(obj)
	//fmt.Printf("getChildren(%#v)\n", obj.Interface())
	for i := obj.NumField() - 1; i > 0; i-- {
		field := obj.Field(i)
		if field.Kind() == reflect.Slice {
			if field.IsNil() {
				slice := reflect.MakeSlice(field.Type(), 0, 0)
				field.Set(slice)
			}
			return field.Addr(), nil
		}
	}

	return reflect.Value{}, errors.New("no slice found in struct")
}

/*
func dump(obj reflect.Value) {
	fmt.Printf("... %#v\n", reflect.Indirect(obj).Interface())
}
*/
