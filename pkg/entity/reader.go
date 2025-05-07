package entity

import (
	"context"
	"fmt"
	"reflect"
)

type EntityGetter interface {
	GetEntity(ctx context.Context, name Id) (*Entity, error)
}

func (e *Entity) ReadInfo(val any) error {
	return readInfo(e, val)
}

func (e *EntityComponent) ReadInfo(val any) error {
	return readInfo(e, val)
}

func readInfo(e AttrGetter, val any) error {
	// Use reflection to populate a struct point from the attrs

	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("val must be a non-nil pointer to a struct")
	}

	v := rv.Elem()

	if v.Kind() != reflect.Struct {
		return fmt.Errorf("val must be a pointer to a struct")
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		name := Id(f.Name)

		if tag, ok := f.Tag.Lookup("entity"); ok {
			name = Id(tag)
		}

		attr, ok := e.Get(name)
		if !ok {
			continue
		}

		val := attr.Value

		fieldVal := v.Field(i)
		if !fieldVal.CanSet() {
			continue
		}

		switch fieldVal.Kind() {
		case reflect.String:
			fieldVal.SetString(val.String())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fieldVal.SetInt(val.Int64())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			fieldVal.SetUint(val.Uint64())
		case reflect.Bool:
			fieldVal.SetBool(val.Bool())
		default:
			return fmt.Errorf("unsupported field type %s for field %s", fieldVal.Kind(), f.Name)
		}
	}

	return nil
}
