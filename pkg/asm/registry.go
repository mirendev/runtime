package asm

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
)

type builderKey struct {
	typ  reflect.Type
	name string
}

type builtValue struct {
	val  reflect.Value
	name string
}

type Registry struct {
	Log        *slog.Logger
	components map[string]interface{}

	built    []builtValue
	builders map[builderKey]reflect.Value
}

func (r *Registry) Register(name string, component interface{}) {
	if r.components == nil {
		r.components = make(map[string]interface{})
	}
	r.components[name] = component
}

func canPopulate(rv reflect.Value) bool {
	return reflect.Indirect(rv).Kind() == reflect.Struct
}

var errNoBuilder = errors.New("no builder for type")

func (r *Registry) buildByType(field reflect.Value, tag string) (reflect.Value, error) {
	for _, v := range r.built {
		if isAssignableTo(field.Type(), v.val.Type()) {
			return v.val, nil
		}
	}

	var (
		builder reflect.Value
		ok      bool
	)

	for k, v := range r.builders {
		if tag != "" && k.name != tag {
			continue
		}

		if isAssignableTo(field.Type(), k.typ) {
			ok = true
			builder = v
			break
		}
	}

	if !ok {
		// Special case: for pointers to structs, we have an implicit builder
		// that creates an empty struct BUT ONLY if the struct comes from
		// within the runtime package. This prevents us from making random
		// structs in other packages that hold a lot of private state.
		/*
			ft := field.Type()

			if ft.Kind() == reflect.Pointer && ft.Elem().Kind() == reflect.Struct &&
				isExported(ft.Elem().Name()) &&
				strings.HasPrefix(ft.Elem().PkgPath(), "miren.dev/runtime") &&

				// Everything in pkg/ is standalone and shouldn't be auto-created.
				!strings.HasPrefix(ft.Elem().PkgPath(), "miren.dev/runtime/pkg") {
				if r.Log != nil {
					r.Log.Debug("implicit builder for pointer to struct", "type", ft)
				}
				ret := reflect.New(ft.Elem())
				err := r.Populate(ret.Interface())
				if err != nil {
					if r.Log != nil {
						r.Log.Error("error populating implicit builder", "type", ft, "error", err)
					}
					return reflect.Value{}, err
				}

				return ret, nil
			}
		*/

		if tag == "" {
			return reflect.Value{}, errNoBuilder
		}

		return reflect.Value{}, errNoBuilder
	}

	var args []reflect.Value

	bt := builder.Type()

	for i := 0; i < bt.NumIn(); i++ {
		argType := bt.In(i)

		if argType.Kind() == reflect.Struct {
			arg := reflect.New(argType)
			err := r.Populate(arg.Interface())
			if err != nil {
				return reflect.Value{}, err
			}

			args = append(args, arg.Elem())
		} else {
			args = append(args, reflect.New(argType).Elem())
		}
	}

	result := builder.Call(args)
	if len(result) == 2 {
		if !result[1].IsNil() {
			return reflect.Value{}, result[1].Interface().(error)
		}
	}

	ret := result[0]

	r.built = append(r.built, builtValue{ret, tag})

	if canPopulate(ret) {
		err := r.Populate(ret.Interface())
		if err != nil {
			return reflect.Value{}, err
		}
	}

	return ret, nil
}

func isExported(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func isAssignableTo(a, b reflect.Type) bool {
	if a == b {
		return true
	}

	if a.Kind() == reflect.Interface {
		return b.Implements(a)
	}

	return b.AssignableTo(a)
}

func (r *Registry) populateByType(field reflect.Value, tag string) (bool, error) {
	if tag == "" {
		for _, v := range r.components {
			cv := reflect.ValueOf(v)

			if isAssignableTo(field.Type(), cv.Type()) {
				field.Set(cv)
				return true, nil
			}
		}
	}

	ret, err := r.buildByType(field, tag)
	if err != nil {
		if err == errNoBuilder {
			return false, nil
		}

		return false, err
	}

	if isAssignableTo(field.Type(), ret.Type()) {
		field.Set(ret)
		return true, nil
	}

	return false, nil
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

func (r *Registry) Resolve(s any) error {
	rv := reflect.ValueOf(s)

	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("expected a pointer, got %T", s)
	}

	ok, err := r.populateByType(rv.Elem(), "")
	if err != nil {
		return err
	}

	if !ok {
		return fmt.Errorf("unable to find component of type %s available", rv.Elem().Type())
	}

	return nil
}

func (r *Registry) ResolveNamed(s any, name string) error {
	rv := reflect.ValueOf(s)

	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("expected a pointer, got %T", s)
	}

	ok, err := r.populateByType(rv.Elem(), name)
	if err != nil {
		return err
	}

	if !ok {
		return fmt.Errorf("unable to find component of type %s available", rv.Elem().Type())
	}

	return nil
}

var injectFuncType = reflect.TypeFor[func(*Registry) error]()

func (r *Registry) Init(values ...any) error {
	for _, v := range values {
		err := r.Resolve(v)
		if err != nil {
			return err
		}
	}

	return nil
}

func parseTag(tag string) (string, bool) {
	comma := strings.IndexByte(tag, ',')
	if comma == -1 {
		return tag, false
	}

	name := tag[:comma]

	f := strings.Fields(tag[comma+1:])

	optional := slices.Contains(f, "optional")

	return name, optional
}

func (r *Registry) InferFrom(s any) error {
	rv := reflect.ValueOf(s)

	rv = reflect.Indirect(rv)

	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("expected a struct, got %T", s)
	}

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Field(i)
		fieldType := rv.Type().Field(i)

		if !fieldType.IsExported() {
			continue
		}

		tag, ok := fieldType.Tag.Lookup("asm")
		if !ok {
			continue
		}

		// Skip fields that are zero
		if field.IsZero() {
			continue
		}

		r.Register(tag, field.Interface())
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

		if !field.IsZero() {
			continue
		}

		tag, ok := fieldType.Tag.Lookup("asm")
		if !ok {
			// If the field is a struct, we can try to populate it type. We error
			// on all other types because the idea of populating, say, a string
			// with a random builder value is nonsense.

			if fieldType.Type.Kind() == reflect.Struct ||
				(fieldType.Type.Kind() == reflect.Ptr && fieldType.Type.Elem().Kind() == reflect.Struct) ||
				fieldType.Type.Kind() == reflect.Interface {
				// ok
			} else {
				return fmt.Errorf("when considering %s/%s.%s, unable to handle unnamed non-(struct || interface) value (type: %s)",
					rv.Type().PkgPath(), rv.Type().Name(), fieldType.Name,
					field.Type())
			}

			ok, err := r.populateByType(field, "")
			if err != nil {
				return err
			}

			if !ok {
				return fmt.Errorf("when considering %s/%s.%s, unable to find component of type %s available",
					rv.Type().PkgPath(), rv.Type().Name(), fieldType.Name,
					field.Type())
			}
			continue
		}

		tag, optional := parseTag(tag)

		component, ok := r.components[tag]
		if !ok {
			ok, err := r.populateByType(field, tag)
			if err != nil {
				return err
			}

			if !ok {
				if !optional {
					return fmt.Errorf("when considering %s/%s.%s, unable to find component of type %s (name %s) available",
						rv.Type().PkgPath(), rv.Type().Name(), fieldType.Name,
						field.Type(), tag)
				}
			}
			continue fields
		}

		if !isAssignableTo(field.Type(), reflect.TypeOf(component)) {
			return fmt.Errorf("component %q is of type %T, expected %s", tag, component, field.Type())
		}

		field.Set(reflect.ValueOf(component))
	}

	return r.RunHooks(s)
}

func (r *Registry) ProvideName(name string, f interface{}) {
	v := reflect.ValueOf(f)
	t := v.Type()

	if t.Kind() != reflect.Func {
		panic("expected a function")
	}

	if t.NumOut() != 1 && t.NumOut() != 2 {
		panic("expected a function with one return value (or two with the second being error)")
	}

	if r.builders == nil {
		r.builders = make(map[builderKey]reflect.Value)
	}

	if _, ok := r.builders[builderKey{t.Out(0), name}]; ok {
		panic("existing builder for type: " + t.Out(0).String())
	}

	r.builders[builderKey{t.Out(0), name}] = v
}

func (r *Registry) Provide(f interface{}) {
	r.ProvideName("", f)
}

func Pick[T any](r *Registry) (T, error) {
	var t T
	err := r.Resolve(&t)
	return t, err
}
