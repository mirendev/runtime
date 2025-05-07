package entity

import (
	"cmp"
	"encoding/base64"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/pkg/entity/types"
)

func TestNaturalDecode(t *testing.T) {
	t.Run("can decode according to a schema", func(t *testing.T) {
		r := require.New(t)

		es := &EncodedSchema{
			Fields: []*SchemaField{
				{
					Name: "foo",
					Type: "string",
					Id:   "test/foo",
				},
				{
					Name: "bar",
					Type: "int",
					Id:   "test/bar",
					Many: true,
				},
				{
					Name: "baz",
					Type: "bool",
					Id:   "test/baz",
				},
				{
					Name: "qux",
					Type: "float",
					Id:   "test/qux",
					Many: true,
				},
				{
					Name: "car",
					Type: "enum",
					Id:   "test/car",
					EnumValues: map[string]Id{
						"red":   "test/red",
						"green": "test/green",
					},
				},
				{
					Name: "label",
					Type: "label",
					Id:   "test/label",
				},
				{
					Name: "sub",
					Type: "component",
					Id:   "test/sub",
					Component: &EncodedSchema{
						Fields: []*SchemaField{
							{
								Name: "name",
								Type: "string",
								Id:   "test.sub/name",
							},
						},
					},
				},
			},
		}

		input := map[string]any{
			"foo":   "hello",
			"bar":   42,
			"baz":   true,
			"qux":   []float64{3.14, 2.71},
			"car":   "red",
			"label": "foo=test",
			"sub": map[string]any{
				"name": "test",
			},
		}

		attrs, err := NaturalDecode(input, es)
		r.NoError(err)

		r.Len(attrs, 8)

		slices.SortFunc(attrs, func(a, b Attr) int {
			return cmp.Compare(a.ID, b.ID)
		})

		r.Equal(attrs[0].ID, Id("test/bar"))
		r.Equal(attrs[0].Value.Any(), int64(42))

		r.Equal(attrs[1].ID, Id("test/baz"))
		r.Equal(attrs[1].Value.Any(), true)

		r.Equal(attrs[2].ID, Id("test/car"))
		r.Equal(attrs[2].Value.Any(), Id("test/red"))

		r.Equal(attrs[3].ID, Id("test/foo"))
		r.Equal(attrs[3].Value.Any(), "hello")

		r.Equal(attrs[4].ID, Id("test/label"))
		r.Equal(attrs[4].Value.Any(), types.Label{Key: "foo", Value: "test"})

		r.Equal(attrs[5].ID, Id("test/qux"))
		r.Equal(attrs[5].Value.Any(), 2.71)

		r.Equal(attrs[6].ID, Id("test/qux"))
		r.Equal(attrs[6].Value.Any(), 3.14)

		r.Equal(attrs[7].ID, Id("test/sub"))
		r.Equal(attrs[7].Value.Component(), &EntityComponent{
			Attrs: Attrs(
				Id("test.sub/name"), "test",
			),
		})

		out, err := yaml.Marshal(input)
		r.NoError(err)

		fmt.Println(string(out))

		var x any

		err = yaml.Unmarshal(out, &x)
		r.NoError(err)

		spew.Dump(x)

		t.Run("and from a list", func(t *testing.T) {
			input := []any{
				map[string]any{"foo": "hello"},
				map[string]any{"bar": 42},
				map[string]any{"baz": true},
				map[string]any{"qux": 3.14},
				map[string]any{"qux": 2.71},
				map[string]any{"car": "red"},
				map[string]any{"label": "foo=test"},
				map[string]any{"sub": map[string]any{
					"name": "test",
				}},
			}

			attrs, err := NaturalDecode(input, es)
			r.NoError(err)

			r.Len(attrs, 8)

			slices.SortFunc(attrs, func(a, b Attr) int {
				return cmp.Compare(a.ID, b.ID)
			})

			r.Equal(attrs[0].ID, Id("test/bar"))
			r.Equal(attrs[0].Value.Any(), int64(42))

			r.Equal(attrs[1].ID, Id("test/baz"))
			r.Equal(attrs[1].Value.Any(), true)

			r.Equal(attrs[2].ID, Id("test/car"))
			r.Equal(attrs[2].Value.Any(), Id("test/red"))

			r.Equal(attrs[3].ID, Id("test/foo"))
			r.Equal(attrs[3].Value.Any(), "hello")

			r.Equal(attrs[4].ID, Id("test/label"))
			r.Equal(attrs[4].Value.Any(), types.Label{Key: "foo", Value: "test"})

			r.Equal(attrs[6].ID, Id("test/qux"))
			r.Equal(attrs[6].Value.Any(), 3.14)

			r.Equal(attrs[5].ID, Id("test/qux"))
			r.Equal(attrs[5].Value.Any(), 2.71)

			r.Equal(attrs[7].ID, Id("test/sub"))
			r.Equal(attrs[7].Value.Component(), &EntityComponent{
				Attrs: Attrs(
					Id("test.sub/name"), "test",
				),
			})

			out, err := yaml.Marshal(input)
			r.NoError(err)

			fmt.Println(string(out))

			var x any

			err = yaml.Unmarshal(out, &x)
			r.NoError(err)

			spew.Dump(x)

		})
	})

	t.Run("can decode exotic types", func(t *testing.T) {
		r := require.New(t)

		es := &EncodedSchema{
			Fields: []*SchemaField{
				{
					Name: "foo",
					Type: "bytes",
					Id:   "test/foo",
				},
				{
					Name: "bar",
					Type: "time",
					Id:   "test/bar",
				},
				{
					Name: "baz",
					Type: "duration",
					Id:   "test/baz",
				},
				{
					Name: "qux",
					Type: "id",
					Id:   "test/qux",
					Many: true,
				},
				{
					Name: "car",
					Type: "keyword",
					Id:   "test/car",
				},
				{
					Name: "sub",
					Type: "any",
					Id:   "test/sub",
				},
			},
		}

		timeNow := time.Now()

		input := map[string]any{
			"foo": base64.StdEncoding.EncodeToString([]byte("hello")),
			"bar": timeNow.Format(time.RFC3339Nano),
			"baz": "10s",
			"qux": "test/id",
			"car": "red",
			"sub": map[int]int{1: 1},
		}

		attrs, err := NaturalDecode(input, es)
		r.NoError(err)

		r.Len(attrs, 6)

		slices.SortFunc(attrs, func(a, b Attr) int {
			return cmp.Compare(a.ID, b.ID)
		})

		r.Equal(attrs[0].ID, Id("test/bar"))
		r.Equal(attrs[0].Value.Any(), timeNow.UTC())

		r.Equal(attrs[1].ID, Id("test/baz"))
		r.Equal(attrs[1].Value.Any(), 10*time.Second)

		r.Equal(attrs[2].ID, Id("test/car"))
		r.Equal(attrs[2].Value.Any(), types.Keyword("red"))

		r.Equal(attrs[3].ID, Id("test/foo"))
		r.Equal(attrs[3].Value.Any(), []byte("hello"))

		r.Equal(attrs[4].ID, Id("test/qux"))
		r.Equal(attrs[4].Value.Any(), Id("test/id"))

		r.Equal(attrs[5].ID, Id("test/sub"))
		r.Equal(attrs[5].Value.Any(), map[int]int{1: 1})

		out, err := yaml.Marshal(input)
		r.NoError(err)

		fmt.Println(string(out))

		var x any

		err = yaml.Unmarshal(out, &x)
		r.NoError(err)

		spew.Dump(x)
	})
}
