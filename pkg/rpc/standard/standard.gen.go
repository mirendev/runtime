package standard

import (
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
)

type timestampData struct {
	Seconds     *int64 `cbor:"0,keyasint,omitempty" json:"seconds,omitempty"`
	Nanoseconds *int32 `cbor:"1,keyasint,omitempty" json:"nanoseconds,omitempty"`
}

type Timestamp struct {
	data timestampData
}

func (v *Timestamp) HasSeconds() bool {
	return v.data.Seconds != nil
}

func (v *Timestamp) Seconds() int64 {
	if v.data.Seconds == nil {
		return 0
	}
	return *v.data.Seconds
}

func (v *Timestamp) SetSeconds(seconds int64) {
	v.data.Seconds = &seconds
}

func (v *Timestamp) HasNanoseconds() bool {
	return v.data.Nanoseconds != nil
}

func (v *Timestamp) Nanoseconds() int32 {
	if v.data.Nanoseconds == nil {
		return 0
	}
	return *v.data.Nanoseconds
}

func (v *Timestamp) SetNanoseconds(nanoseconds int32) {
	v.data.Nanoseconds = &nanoseconds
}

func (v *Timestamp) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Timestamp) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Timestamp) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type durationData struct {
	Nanoseconds *uint64 `cbor:"0,keyasint,omitempty" json:"nanoseconds,omitempty"`
}

type Duration struct {
	data durationData
}

func (v *Duration) HasNanoseconds() bool {
	return v.data.Nanoseconds != nil
}

func (v *Duration) Nanoseconds() uint64 {
	if v.data.Nanoseconds == nil {
		return 0
	}
	return *v.data.Nanoseconds
}

func (v *Duration) SetNanoseconds(nanoseconds uint64) {
	v.data.Nanoseconds = &nanoseconds
}

func (v *Duration) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Duration) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Duration) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}
