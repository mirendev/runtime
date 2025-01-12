package union

import (
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
)

type MessageUser interface {
	Which() string
	Name() string
	SetName(string)
	Uid() int64
	SetUid(int64)
	Deleted() bool
	SetDeleted(bool)
}

type messageUser struct {
	U_Name    *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	U_Uid     *int64  `cbor:"1,keyasint,omitempty" json:"uid,omitempty"`
	U_Deleted *bool   `cbor:"2,keyasint,omitempty" json:"deleted,omitempty"`
}

func (v *messageUser) Which() string {
	if v.U_Name != nil {
		return "name"
	}
	if v.U_Uid != nil {
		return "uid"
	}
	if v.U_Deleted != nil {
		return "deleted"
	}
	return ""
}

func (v *messageUser) Name() string {
	if v.U_Name == nil {
		return ""
	}
	return *v.U_Name
}

func (v *messageUser) SetName(val string) {
	v.U_Uid = nil
	v.U_Deleted = nil
	v.U_Name = &val
}

func (v *messageUser) Uid() int64 {
	if v.U_Uid == nil {
		return 0
	}
	return *v.U_Uid
}

func (v *messageUser) SetUid(val int64) {
	v.U_Name = nil
	v.U_Deleted = nil
	v.U_Uid = &val
}

func (v *messageUser) Deleted() bool {
	if v.U_Deleted == nil {
		return false
	}
	return *v.U_Deleted
}

func (v *messageUser) SetDeleted(val bool) {
	v.U_Name = nil
	v.U_Uid = nil
	v.U_Deleted = &val
}

type MessageList interface {
	Which() string
	Strings() []string
	SetStrings([]string)
	Numbers() []int64
	SetNumbers([]int64)
}

type messageList struct {
	U_Strings *[]string `cbor:"3,keyasint,omitempty" json:"strings,omitempty"`
	U_Numbers *[]int64  `cbor:"4,keyasint,omitempty" json:"numbers,omitempty"`
}

func (v *messageList) Which() string {
	if v.U_Strings != nil {
		return "strings"
	}
	if v.U_Numbers != nil {
		return "numbers"
	}
	return ""
}

func (v *messageList) Strings() []string {
	if v.U_Strings == nil {
		return nil
	}
	return *v.U_Strings
}

func (v *messageList) SetStrings(val []string) {
	v.U_Numbers = nil
	x := slices.Clone(val)
	v.U_Strings = &x
}

func (v *messageList) Numbers() []int64 {
	if v.U_Numbers == nil {
		return nil
	}
	return *v.U_Numbers
}

func (v *messageList) SetNumbers(val []int64) {
	v.U_Strings = nil
	x := slices.Clone(val)
	v.U_Numbers = &x
}

type messageData struct {
	messageUser
	messageList
}

type Message struct {
	data messageData
}

func (v *Message) User() MessageUser {
	return &v.data.messageUser
}

func (v *Message) List() MessageList {
	return &v.data.messageList
}

func (v *Message) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Message) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Message) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Message) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}
