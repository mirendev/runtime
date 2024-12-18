package embed

import (
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
)

type powerData struct {
	Weakness *string  `cbor:"0,keyasint,omitempty" json:"weakness,omitempty"`
	Power    *float32 `cbor:"1,keyasint,omitempty" json:"power,omitempty"`
}

type Power struct {
	data powerData
}

func (v *Power) HasWeakness() bool {
	return v.data.Weakness != nil
}

func (v *Power) Weakness() string {
	if v.data.Weakness == nil {
		return ""
	}
	return *v.data.Weakness
}

func (v *Power) SetWeakness(weakness string) {
	v.data.Weakness = &weakness
}

func (v *Power) HasPower() bool {
	return v.data.Power != nil
}

func (v *Power) Power() float32 {
	if v.data.Power == nil {
		return 0
	}
	return *v.data.Power
}

func (v *Power) SetPower(power float32) {
	v.data.Power = &power
}

func (v *Power) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Power) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Power) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Power) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type heroData struct {
	Age   *int32 `cbor:"0,keyasint,omitempty" json:"age,omitempty"`
	Power *Power `cbor:"1,keyasint,omitempty" json:"power,omitempty"`
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

func (v *Hero) Power() *Power {
	return v.data.Power
}

func (v *Hero) SetPower(power *Power) {
	v.data.Power = power
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
