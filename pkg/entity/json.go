package entity

import (
	"encoding/json"
)

func (v Value) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(valueEncodeTuple{
		Kind: v.Kind(), Value: v.Any(),
	})

	return b, err
}

func (v *Value) UnmarshalJSON(b []byte) error {
	var x valueDecodeTuple

	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}

	v.setFromTuple(x, jsonUnmarshaler{})

	return nil
}

type jsonUnmarshaler struct{}

func (jsonUnmarshaler) Unmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}

var _ json.Marshaler = Value{}
var _ json.Unmarshaler = &Value{}
