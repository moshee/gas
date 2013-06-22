package gas

import (
	"database/sql"
	_ "github.com/lib/pq"
	//	"log"
	"errors"
	"fmt"
	"reflect"

//	"unsafe"
)

var (
	DB *sql.DB

//	models map[string]*Model
)

func init() {
	var err error
	DB, err = sql.Open("postgres", "user=postgres dbname=postgres sslmode=disable")
	if err != nil {
		panic(err)
	}
	DB.SetMaxIdleConns(10)
}

/*
type Model struct {
	name    string
	actions map[string]*sql.Stmt
	fields  []reflect.StructField
}

func NewModel(table string, data interface{}) (model *Model) {
	model = new(Model)
	model.name = table
	model.actions = make(map[string]*sql.Stmt)

	t := reflect.TypeOf(data)
	model.fields = make([]reflect.StructField, t.NumField())
	for i := range model.fields {
		model.fields[i] = t.Field(i)
	}

	return
}

// TODO: I'm sure this isn't all of the supported types; look in the pq source
func typeof(field reflect.StructField) string {
	switch field.Type.Kind() {
	case reflect.Int64:
		return "bigint"
	case reflect.Int32, reflect.Int:
		return "int"
	case reflect.Int16:
		return "smallint"
	case reflect.String:
		return "text"
	case reflect.Float32:
		return "real"
	case reflect.Float64:
		return "double precision"
	case reflect.Bool:
		return "boolean"
	case reflect.Invalid:
		panic("invalid kind of field")
	}
	if field.Type.PkgPath() == "time" && field.Type.Name() == "Time" {
		return "timestamp"
	}
	panic("no such type")
}

// Create the table corresponding to this model, if it doesn't exist already.
// This is called on ignition. (TODO: use something like django's syncdb
// instead of trying to create tables every time?)
func (model *Model) Create() error {
	query := "CREATE TABLE IF NOT EXISTS " + model.name + " ( "
	for i, field := range model.fields {
		if tag := field.Tag.Get("db"); tag != "" {
			query += tag
		} else {
			query += field.Name
		}
		query += " " + typeof(field)
		if i < len(model.fields)-1 {
			query += ", "
		}
	}
	query += " )"
	_, err := DB.Exec(query)
	return err
}

// Drop the table corresponding to this model
func (model *Model) Drop() error {
	_, err := DB.Exec("DROP TABLE " + model.name)
	return err
}

// Prepare and optimize a named query statement to be used later.
func (model *Model) Prepare(name, query string) *Model {
	stmt, err := DB.Prepare(query)
	if err != nil {
		log.Fatalf("Could not prepare SQL statement '%s': %v\n", query, err)
	}
	model.actions[name] = stmt
	return model
}

func (model *Model) makeslice() []interface{} {
	slice := make([]interface{}, len(model.fields))
	for i := range slice {
		slice[i] = reflect.New(model.fields[i].Type).Interface()
	}
	return slice
}

// Using the named query, query the database and fill the slice `data` with the
// resulting rows. `data` MUST be a slice of pointers to structs, and those
// structs MUST be of the same type as the one given to NewModel. If either of
// these cases are false, QueryAction will panic.
func (model *Model) Query(name string, data interface{}, args ...interface{}) error {
	// var value []*T
	value := reflect.ValueOf(data)
	rows, err := model.actions[name].Query(args...)
	if err != nil {
		return err
	}
	// for i := range value {
	for i := 0; i < value.Len(); i++ {
		if !rows.Next() {
			// XXX error: less results returned than length of slice
			return nil
		}
		// var s []*interface{}
		s := model.makeslice()
		if err := rows.Scan(s...); err != nil {
			return err
		}
		// var value_of_struct T = *value[i]
		//value_of_struct := reflect.Indirect(value.Index(i))

		// new_struct := new(typeof(*value[i]))
		new_struct := reflect.New(reflect.TypeOf(data).Elem().Elem())
		for j, new_field := range s {
			// var field interface{} = &((*T)[i])
			field := reflect.Indirect(new_struct).Field(j)
			// *field = *new_field
			field.Set(reflect.Indirect(reflect.ValueOf(new_field)))
		}
		value.Index(i).Set(new_struct)
	}
	return nil
}

// func (model *Model) QueryRow(name string, data interface{}, args ...interface{}) error

// for UPDATE, INSERT, and other things that return no rows. If `data` is not a
// pointer to a struct of the type specified in NewModel or nil, Exec will
// panic.
// If `data` is nil, just `args` will be used as query arguments. Otherwise, the fields
// of the struct will be used.
/*
func (model *Model) Exec(name string, data interface{}, args ...interface{}) (sql.Result, error) {
	stmt := model.actions[name]
	if data == nil {
		return stmt.Exec(args)
	}

	fields := model.makeslice()
	val := reflect.Indirect(reflect.ValueOf(data))
	for i, field := range fields {
		newval := reflect.ValueOf(fields[i])
		newval.Set(val.Field(i))
	}

	stmt.Exec(append(fields,
}


func (model *Model) ActionOne(

/*
func SelectSlice(model, action string, slice interface{}, args ...interface{}) {

}

func Select(model, action string, dest interface{}, args ...interface{}) {

}

func SelectSliceQuery(query string, slice interface{}, args ...interface{}) {

}

func SelectQuery(query string, slice interface{}, args ...interface{}) {

}

func InsertSlice(model, action string, slice interface{}) {

}

func Insert(model, action string, src interface{}) {

}

func InsertSliceQuery(query string, slice interface{}) {

}

func InsertQuery(query string, src interface{}) {

}
*/

// A Model is the description of a type as used to marshal rows returned from
// the database into types. Model only pays attention to field indices, not
// names. This may change in the future (like using struct tags for this).
// TODO: make the system ignore the existence of anonymous fields; that is,
// pretend that the fields of an anonymous field are directly inside of the
// parent. XXX only one indirection or recursive?
type Model struct {
	//table   string
	//actions map[string]*sql.Stmt
	fields []reflect.Type
}

func (self *Model) makefields() []interface{} {
	fields := make([]interface{}, len(self.fields))
	for i := range fields {
		fields[i] = reflect.New(self.fields[i]).Interface()
	}
	return fields
}

var models = make(map[reflect.Type]*Model)

// Adds the description of a type to the model cache for future use. Only
// pointers to structs or pointers to slices must be used! Anything else will
// probably panic.
func Register(model interface{}) {
	register(reflect.TypeOf(model))
}

func register(t reflect.Type) {
	model := new(Model)
	model.fields = make([]reflect.Type, t.NumField())
	for i := range model.fields {
		model.fields[i] = t.Field(i).Type
	}
	// TODO: model.table = get_table_name(t.Name())
	models[t] = model
}

func maybe_register(data interface{}) (reflect.Type, *Model, error) {
	data_type := reflect.TypeOf(data).Elem()
	// if it's a slice pointer we need to do two more indirects
	if data_type.Kind() == reflect.Slice {
		data_type = data_type.Elem().Elem()
	}
	model := models[data_type]
	if model == nil {
		register(data_type)
		model = models[data_type]
		if model == nil {
			return nil, nil, errors.New(fmt.Sprintf("Couldn't register model: %s", data_type.Name()))
		}
	}
	return data_type, model, nil
}

func marshal_row(dest interface{}, fields []interface{}, row interface {
	Scan(...interface{}) error
}) error {
	if err := row.Scan(fields...); err != nil {
		return err
	}
	dest_val := reflect.ValueOf(dest)
	if dest_val.Kind() == reflect.Ptr {
		dest_val = dest_val.Elem()
	}
	for i, field := range fields {
		val := reflect.ValueOf(field)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		dest_val.Field(i).Set(val)
	}
	return nil
}

// Select a single row into `data`, caching the type for further use if not
// already cached. SelectRow will panic if `data` does not match the data in
// the row returned by `query`.
func SelectRow(data interface{}, query string, args ...interface{}) error {
	_, model, err := maybe_register(data)
	if err != nil {
		return err
	}

	row := DB.QueryRow(query, args...)
	fields := model.makefields()
	return marshal_row(data, fields, row)
}

// Select multiple rows into `data`, caching the type for further use if not
// already cached. SelectRows will panic if `data` does not match the data in
// the rows returned by `query`.
func SelectRows(data interface{}, query string, args ...interface{}) (int, error) {
	data_type, model, err := maybe_register(data)
	if err != nil {
		return 0, err
	}

	data_val := reflect.ValueOf(data).Elem()

	rows, err := DB.Query(query, args...)
	if err != nil {
		return 0, err
	}

	slice := reflect.MakeSlice(data_val.Type(), 0, 0)

	i := 0
	for ; rows.Next(); i++ {
		fields := model.makefields()
		val := reflect.New(data_type).Interface()
		if err = marshal_row(val, fields, rows); err != nil {
			rows.Close()
			return i, err
		}
		slice = reflect.Append(slice, reflect.ValueOf(val))
	}
	data_val.Set(slice)
	return i, rows.Err()
}

/*
func InsertRow(data interface{}, table string) (sql.Result, error) {
	_, model, err := maybe_register(data)
	if err != nil {
		return nil, err
	}
	val := reflect.ValueOf(data).Elem()
	num_field := val.NumField()
	fields := make([]interface{}, num_field+1)
	fields[0] = table
	for i := 0; i < num_field; i++ {
		fields[i+1] = val.Field(i).Interface()
	}
	return model.insert.Exec(fields...)
}

*/
