// Utils for Uber-FX-based Dependency Injection (DI)
package diutils

import (
	"reflect"
	// "go.uber.org/fx"
)

// Sample usage:
//
// type DependenciesType struct {
// 	Foo Foo
// 	Bar Bar
// 	Baz Baz
// }

//	type DependenciesParams struct {
//		fx.In
//		Foo Foo
//		Bar Bar
//		Baz Baz
//	}
//
//	func NewDependencies(params DependenciesParams) *DependenciesType {
//		retval := utils.Construct[DependenciesParams, DependenciesType](params)
//		return retval
//	}
func Construct[P any, T any, PT interface{ *T }](params interface{}) PT {
	p := PT(new(T))
	construct0(params, p)
	return p
}

func construct0(params interface{}, retval interface{}) {
	// Check if retval is a pointer
	rv := reflect.ValueOf(retval)
	if rv.Kind() != reflect.Ptr {
		panic("retval is not a pointer")
	}

	// Dereference the pointer to get the underlying value
	rv = rv.Elem()

	// Check if the dereferenced value is a struct
	if rv.Kind() != reflect.Struct {
		panic("retval is not a pointer to a struct")
	}

	// Now, get the value of params
	rp := reflect.ValueOf(params)
	if rp.Kind() != reflect.Struct {
		if rp.Kind() == reflect.Ptr {
			rp = rp.Elem()
			if rp.Kind() != reflect.Struct {
				panic("params is not a struct or a pointer to a struct")
			}
		} else {
			panic("params is not a struct or a pointer to a struct")
		}
	}

	// Iterate over the fields of params and copy to retval
	for i := 0; i < rp.NumField(); i++ {
		name := rp.Type().Field(i).Name
		field, ok := rv.Type().FieldByName(name)
		if ok && field.Type == rp.Field(i).Type() {
			rv.FieldByName(name).Set(rp.Field(i))
		}
	}
}
