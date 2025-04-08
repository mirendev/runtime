package entity

import (
	"gopkg.in/yaml.v3"
)

func (v Value) MarshalYAML() (any, error) {
	return v.Any(), nil
}

func (v *Value) UnmarshalYAML(node *yaml.Node) error {
	var x valueDecodeTuple

	if err := node.Decode(&x); err != nil {
		return err
	}

	v.setFromTuple(x, yamlUnmarshaler{})

	return nil
}

type yamlUnmarshaler struct{}

func (yamlUnmarshaler) Unmarshal(b []byte, v any) error {
	return yaml.Unmarshal(b, v)
}

var _ yaml.Marshaler = Value{}
var _ yaml.Unmarshaler = &Value{}
