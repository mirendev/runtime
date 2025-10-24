package model

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"time"

	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/mapx"
	"miren.dev/runtime/pkg/multierror"
)

type SchemaValue struct {
	Id       string         `json:"id,omitempty" yaml:"id,omitempty" cbor:"id,omitempty"`
	Kind     string         `json:"kind" yaml:"kind" cbor:"kind"`
	Version  string         `json:"version" yaml:"version" cbor:"version"`
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty" cbor:"metadata,omitempty"`
	Spec     map[string]any `json:"spec" yaml:"spec" cbor:"spec"`
}

func NaturalDecode(data any, es *entity.EncodedSchema) (*entity.Entity, error) {
	attrs, err := naturalDecode(data, es, true)
	if err != nil {
		return nil, err
	}

	return entity.New(attrs), nil
}

func naturalDecode(data any, es *entity.EncodedSchema, top bool) ([]entity.Attr, error) {
	var (
		excludedFields []string
		attrs          []entity.Attr
		err            error
	)

	switch data := data.(type) {
	case map[string]any:
		for k, v := range data {
			// Check if the key exists in the schema
			// and if the value is not null
			if v == nil {
				continue
			}

			f := es.GetField(k)
			if f == nil {
				excludedFields = append(excludedFields, k)
				continue
			}

			if f.Many {
				rv := reflect.ValueOf(v)
				if rv.Kind() == reflect.Slice {
					for i := 0; i < rv.Len(); i++ {
						subAttrs, err := decodeNaturalValue(f, rv.Index(i).Interface())
						if err != nil {
							err = multierror.Append(err, fmt.Errorf("failed to decode field %s: %w", f.Name, err))
							continue
						}
						attrs = append(attrs, subAttrs...)
					}

					continue
				}
			}

			subAttrs, err := decodeNaturalValue(f, v)
			if err != nil {
				err = multierror.Append(err, fmt.Errorf("failed to decode field %s: %w", f.Name, err))
				continue
			}
			attrs = append(attrs, subAttrs...)
		}
	case []any:
		for _, v := range data {
			m, ok := v.(map[string]any)
			if !ok {
				err = multierror.Append(err, fmt.Errorf("failed to decode field: expected map[string]any, got %T", v))
				continue
			}
			if len(m) != 1 {
				err = multierror.Append(err, fmt.Errorf("failed to decode field: expected map[string]any with one key, got %d", len(m)))
				continue
			}

			for k, v := range m {
				f := es.GetField(k)
				if f == nil {
					excludedFields = append(excludedFields, k)
					continue
				}

				if f.Many {
					rv := reflect.ValueOf(v)
					if rv.Kind() == reflect.Slice {
						for i := 0; i < rv.Len(); i++ {
							subAttrs, err := decodeNaturalValue(f, rv.Index(i).Interface())
							if err != nil {
								err = multierror.Append(err, fmt.Errorf("failed to decode field %s: %w", f.Name, err))
								continue
							}
							attrs = append(attrs, subAttrs...)
						}

						continue
					}
				}

				subAttrs, err := decodeNaturalValue(f, v)
				if err != nil {
					err = multierror.Append(err, fmt.Errorf("failed to decode field %s: %w", f.Name, err))
					continue
				}
				attrs = append(attrs, subAttrs...)
			}
		}
	}

	if len(excludedFields) > 0 {
		err = multierror.Append(err, fmt.Errorf("failed to decode fields: %s", strings.Join(excludedFields, ", ")))
	}

	if top && es.PrimaryKind != "" {
		// Add the primary kind as a label
		attrs = append(attrs,
			entity.Ref(entity.EntityKind, entity.Id(es.Domain+"/kind."+es.PrimaryKind)),
		)
	}

	return entity.SortedAttrs(attrs), err
}

func NaturalEncode(e *entity.Entity, es *entity.EncodedSchema) (*SchemaValue, error) {
	m, err := naturalEncodeMap(e, es)
	if err != nil {
		return nil, err
	}

	sv := &SchemaValue{
		Kind:    es.Name,
		Version: es.Version,
		Spec:    m,
	}

	return sv, nil
}

func naturalEncodeMap(e *entity.Entity, es *entity.EncodedSchema) (map[string]any, error) {
	m := make(map[string]any)

	// Group attributes by field ID
	attrsByField := make(map[entity.Id][]entity.Attr)
	for _, attr := range e.Attrs() {
		attrsByField[attr.ID] = append(attrsByField[attr.ID], attr)
	}

	// Process each field in the schema
	for _, field := range es.Fields {
		attrs := attrsByField[field.Id]
		if len(attrs) == 0 {
			continue
		}

		if field.Many {
			// Handle multi-value fields
			values := make([]any, 0, len(attrs))
			for _, attr := range attrs {
				val, err := encodeNaturalValue(field, attr.Value)
				if err != nil {
					return nil, fmt.Errorf("failed to encode field %s: %w", field.Name, err)
				}
				values = append(values, val)
			}
			m[field.Name] = values
		} else {
			// Handle single-value fields
			val, err := encodeNaturalValue(field, attrs[0].Value)
			if err != nil {
				return nil, fmt.Errorf("failed to encode field %s: %w", field.Name, err)
			}
			m[field.Name] = val
		}
	}

	return m, nil
}

func encodeNaturalValue(f *entity.SchemaField, val entity.Value) (any, error) {
	switch f.Type {
	case "string":
		return val.String(), nil
	case "int":
		return val.Int64(), nil
	case "bool":
		return val.Bool(), nil
	case "float":
		return val.Float64(), nil
	case "enum":
		// Reverse lookup enum value
		id := val.Id()
		for name, enumId := range f.EnumValues {
			if enumId == id {
				return name, nil
			}
		}
		return nil, fmt.Errorf("enum value not found for id %s (possible: %s)", id, mapx.Values(f.EnumValues))
	case "label":
		lbl := val.Label()
		return fmt.Sprintf("%s=%s", lbl.Key, lbl.Value), nil
	case "bytes":
		return base64.StdEncoding.EncodeToString(val.Bytes()), nil
	case "time":
		return val.Time().Format(time.RFC3339Nano), nil
	case "duration":
		return val.Duration().String(), nil
	case "id":
		return string(val.Id()), nil
	case "keyword":
		return val.Keyword(), nil
	case "any":
		return val.Any(), nil
	case "component":
		comp := val.Component()
		if comp == nil {
			return nil, nil
		}
		if f.Component == nil {
			return nil, fmt.Errorf("component field %s has nil Component schema", f.Name)
		}
		return naturalEncodeMap(entity.New(comp.Attrs()), f.Component)
	default:
		return nil, fmt.Errorf("unsupported type: %s", f.Type)
	}
}

func decodeNaturalValue(f *entity.SchemaField, v any) ([]entity.Attr, error) {
	var (
		attrs []entity.Attr
		err   error
	)

	switch f.Type {
	case "string":
		str, ok := v.(string)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected string, got %T", f.Name, v))
		} else {
			attrs = append(attrs, entity.String(f.Id, str))
		}
	case "int":
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Int {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected int, got %T", f.Name, v))

		} else {
			attrs = append(attrs, entity.Int(f.Id, int(rv.Int())))
		}
	case "bool":
		b, ok := v.(bool)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected bool, got %T", f.Name, v))
		} else {
			attrs = append(attrs, entity.Bool(f.Id, b))
		}
	case "float":
		d, ok := v.(float64)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected float64, got %T", f.Name, v))
		} else {
			attrs = append(attrs, entity.Float64(f.Id, d))
		}
	case "enum":
		enum, ok := v.(string)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected string, got %T", f.Name, v))
		} else {
			id, ok := f.EnumValues[enum]
			if !ok {
				err = multierror.Append(err, fmt.Errorf("enum %s not found in schema", enum))
			}

			attrs = append(attrs, entity.Ref(f.Id, id))
		}
	case "label":
		switch label := v.(type) {
		case string:
			k, v, ok := strings.Cut(label, "=")
			if ok {
				attrs = append(attrs, entity.Label(f.Id, k, v))
			} else {
				err = multierror.Append(err, fmt.Errorf("invalid label used: %s ", label))
			}
		case map[string]any:
			for k, v := range label {
				attrs = append(attrs, entity.Label(f.Id, k, fmt.Sprint(v)))
			}
		default:
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected string, got %T", f.Name, v))
		}
	case "bytes":
		b, ok := v.(string)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected string, got %T", f.Name, v))
		} else {
			data, err := base64.StdEncoding.DecodeString(b)
			if err != nil {
				err = multierror.Append(err, fmt.Errorf("failed to decode field %s: %w", f.Name, err))
			}
			attrs = append(attrs, entity.Bytes(f.Id, data))
		}
	case "time":
		t, ok := v.(string)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected string, got %T", f.Name, v))
		} else {
			tm, err := time.Parse(time.RFC3339Nano, t)
			if err != nil {
				err = multierror.Append(err, fmt.Errorf("failed to decode field %s: %w", f.Name, err))
			}

			attrs = append(attrs, entity.Time(f.Id, tm))
		}
	case "duration":
		d, ok := v.(string)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected string, got %T", f.Name, v))
		} else {
			dur, err := time.ParseDuration(d)
			if err != nil {
				err = multierror.Append(err, fmt.Errorf("failed to decode field %s: %w", f.Name, err))
			} else {
				attrs = append(attrs, entity.Duration(f.Id, dur))
			}
		}
	// TODO: list
	case "id":
		id, ok := v.(string)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected string, got %T", f.Name, v))
		} else {
			attrs = append(attrs, entity.Ref(f.Id, entity.Id(id)))
		}
	case "keyword":
		kw, ok := v.(string)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected string, got %T", f.Name, v))
		} else {
			if !entity.ValidKeyword(kw) {
				err = multierror.Append(err, fmt.Errorf("failed to decode field %s: %w", f.Name, err))
			} else {
				attrs = append(attrs, entity.Keyword(f.Id, kw))
			}
		}
	case "any":
		attrs = append(attrs, entity.Any(f.Id, v))
	case "component":
		m, ok := v.(map[string]any)
		if !ok {
			err = multierror.Append(err, fmt.Errorf("failed to decode field %s: expected map[string]any, got %T", f.Name, v))
		} else {
			sub, err := naturalDecode(m, f.Component, false)
			if err != nil {
				err = multierror.Append(err, fmt.Errorf("failed to decode component %s: %w", f.Name, err))
			} else {
				attrs = append(attrs, entity.Component(f.Id, sub))
			}
		}
	}

	return attrs, err
}
