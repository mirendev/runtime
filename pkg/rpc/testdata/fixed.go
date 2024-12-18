package fixed

import (
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
)

type heroData struct {
	Age   *int32   `cbor:"0,keyasint,omitempty" json:"age,omitempty"`
	Power *float32 `cbor:"1,keyasint,omitempty" json:"power,omitempty"`
}

type Hero struct {
	data heroData
}

func (v *Hero) HasAge() bool {
	return v.data.Age != nil
}

func (v *Hero) Age() int32 {
	if v.data.Age == nil {
		return 0
	}
	return *v.data.Age
}

func (v *Hero) SetAge(age int32) {
	v.data.Age = &age
}

func (v *Hero) HasPower() bool {
	return v.data.Power != nil
}

func (v *Hero) Power() float32 {
	if v.data.Power == nil {
		return 0
	}
	return *v.data.Power
}

func (v *Hero) SetPower(power float32) {
	v.data.Power = &power
}

func (v *Hero) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Hero) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Hero) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Hero) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}
