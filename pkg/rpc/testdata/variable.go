package variable

import (
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
)

type heroData struct {
	Age    *int32    `cbor:"0,keyasint,omitempty" json:"age,omitempty"`
	Name   *string   `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	People *[]string `cbor:"2,keyasint,omitempty" json:"people,omitempty"`
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

func (v *Hero) HasName() bool {
	return v.data.Name != nil
}

func (v *Hero) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *Hero) SetName(name string) {
	v.data.Name = &name
}

func (v *Hero) HasPeople() bool {
	return v.data.People != nil
}

func (v *Hero) People() []string {
	if v.data.People == nil {
		return nil
	}
	return *v.data.People
}

func (v *Hero) SetPeople(people []string) {
	x := slices.Clone(people)
	v.data.People = &x
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
