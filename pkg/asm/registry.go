package asm

import (
	"fmt"
	"reflect"
)

type Registry struct {
	components map[string]interface{}
	builders   map[reflect.Type]reflect.Value
}

func (r *Registry) Register(name string, component interface{}) {
	if r.components == nil {
		r.components = make(map[string]interface{})
	}
	r.components[name] = component
}

func (r *Registry) buildByType(field reflect.Value) (reflect.Value, error) {
	builder, ok := r.builders[field.Type()]
	if !ok {
		return reflect.Value{}, fmt.Errorf("no builder for %q", field.Type())
	}

	result := builder.Call(nil)
	if len(result) == 2 {
		if !result[1].IsNil() {
			return reflect.Value{}, result[1].Interface().(error)
		}
	}

	ret := result[0]

	err := r.Populate(ret.Interface())
	if err != nil {
		return reflect.Value{}, err
	}

	return ret, nil
}

func isAssignableTo(a, b reflect.Type) bool {
	if a == b {
		return true
	}

	if a.Kind() == reflect.Interface {
		return b.Implements(a)
	}

	return a.AssignableTo(b)
}

func (r *Registry) populateByType(field reflect.Value) error {
	for _, v := range r.components {
		cv := reflect.ValueOf(v)

		if isAssignableTo(cv.Type(), field.Type()) {
			field.Set(cv)
			return nil
		}
	}

	ret, err := r.buildByType(field)
	if err == nil {
		field.Set(ret)
		return nil
	}

	return fmt.Errorf("no component of type %s available", field.Type())
}

type HasPopulated interface {
	Populated() error
}

func (r *Registry) RunHooks(s any) error {
	if pop, ok := s.(HasPopulated); ok {
		return pop.Populated()
	}

	return nil
}

func (r *Registry) Populate(s interface{}) error {
	rv := reflect.ValueOf(s)

	rv = reflect.Indirect(rv)

	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("expected a struct, got %T", s)
	}

fields:
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		fieldType := rv.Type().Field(i)

		if !fieldType.IsExported() {
			continue
		}

		tag, ok := fieldType.Tag.Lookup("asm")
		if !ok {
			err := r.populateByType(field)
			if err != nil {
				return err
			}
			continue
		}

		component, ok := r.components[tag]
		if !ok {
			err := r.populateByType(field)
			if err != nil {
				return err
			}
			continue fields
		}

		if !isAssignableTo(reflect.TypeOf(component), field.Type()) {
			return fmt.Errorf("component %q is of type %T, expected %s", tag, component, field.Type())
		}

		field.Set(reflect.ValueOf(component))
	}

	return r.RunHooks(s)
}

func (r *Registry) Add(f interface{}) {
	v := reflect.ValueOf(f)
	t := v.Type()

	if t.Kind() != reflect.Func {
		panic("expected a function")
	}

	if t.NumOut() != 1 && t.NumOut() != 2 {
		panic("expected a function with one return value")
	}

	if r.builders == nil {
		r.builders = make(map[reflect.Type]reflect.Value)
	}

	r.builders[t.Out(0)] = v
}
