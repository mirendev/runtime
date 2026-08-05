package model

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"time"

	"miren.dev/runtime/pkg/entity"
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
