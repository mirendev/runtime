package entity

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"time"
	"unsafe"

	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
	"miren.dev/runtime/pkg/entity/types"
)

type Attr struct {
	ID    Id    `json:"id" cbor:"id"`
	Value Value `json:"v" cbor:"v"`
}

type AttrsFunc func() []Attr

type AttrList []Attr

func (a Attr) Kind() any {
	return a.Value.any
}

func (a Attr) Sum(h io.Writer) {
	h.Write([]byte(a.ID))
	h.Write([]byte{':'})
	h.Write([]byte{byte(a.Value.Kind())})
	h.Write([]byte{':'})
	a.Value.sum(h)
}

func (a Attr) CAS() string {
	h, _ := blake2b.New256(nil)
	a.Sum(h)
	return base58.Encode(h.Sum(nil))
}

func (a Attr) Equal(b Attr) bool {
	if a.ID != b.ID {
		return false
	}

	return a.Value.Equal(b.Value)
}

func SortedAttrs(attrs []Attr) []Attr {
	slices.SortFunc(attrs, func(a, b Attr) int {
		return a.Compare(b)
	})
	return slices.CompactFunc(attrs, func(a, b Attr) bool {
		return a.Equal(b)
	})
}

type Value struct {
	_   [0]func() // disallow ==
	num uint64
	any any
}

type valueEncodeTuple struct {
	_     struct{}  `cbor:",toarray"`
	Kind  ValueKind `cbor:"0" json:"k"`
	Value any       `cbor:"1" json:"v"`
}

func (v *Value) MarshalCBOR() ([]byte, error) {
	return encoder.Marshal(valueEncodeTuple{
		Kind: v.Kind(), Value: v.Any(),
	})
}

var cborNil = []byte{0xf6}

type superRawMessage []byte

// MarshalCBOR returns m or CBOR nil if m is nil.
func (m superRawMessage) MarshalCBOR() ([]byte, error) {
	if len(m) == 0 {
		return cborNil, nil
	}
	return m, nil
}

// UnmarshalCBOR creates a copy of data and saves to *m.
func (m *superRawMessage) UnmarshalCBOR(data []byte) error {
	if m == nil {
		return errors.New("cbor.RawMessage: UnmarshalCBOR on nil pointer")
	}
	*m = append((*m)[0:0], data...)
	return nil
}

// MarshalJSON returns m as the JSON encoding of m.
func (m superRawMessage) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

// UnmarshalJSON sets *m to a copy of data.
func (m *superRawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errors.New("json.RawMessage: UnmarshalJSON on nil pointer")
	}
	*m = append((*m)[0:0], data...)
	return nil
}

type valueDecodeTuple struct {
	_     struct{}        `cbor:",toarray"`
	Kind  ValueKind       `cbor:"0" json:"k"`
	Value superRawMessage `cbor:"1" json:"v"`
}

func (v *Value) UnmarshalCBOR(b []byte) error {
	var x valueDecodeTuple

	if err := decoder.Unmarshal(b, &x); err != nil {
		return err
	}

	return v.setFromTuple(x, decoder)
}

type unmarshaler interface {
	Unmarshal(b []byte, v any) error
}

func (v *Value) setFromTuple(x valueDecodeTuple, decoder unmarshaler) error {
	kind := x.Kind

	switch kind {
	case KindString:
		var str string

		if err := decoder.Unmarshal(x.Value, &str); err != nil {
			return fmt.Errorf("bad string: %w", err)
		}

		*v = StringValue(str)
	case KindInt64, KindUint64, KindDuration:
		var num uint64

		if err := decoder.Unmarshal(x.Value, &num); err != nil {
			return fmt.Errorf("bad integer: %w", err)
		}

		v.num = num
		v.any = kind
	case KindBool:
		var b bool

		if err := decoder.Unmarshal(x.Value, &b); err != nil {
			return fmt.Errorf("bad bool: %w", err)
		}

		if b {
			v.num = 1
		} else {
			v.num = 0
		}

		v.any = kind
	case KindFloat64:
		var f float64

		if err := decoder.Unmarshal(x.Value, &f); err != nil {
			return fmt.Errorf("bad float64: %w", err)
		}

		v.num = math.Float64bits(f)
		v.any = kind
	case KindTime:
		var t time.Time

		if err := decoder.Unmarshal(x.Value, &t); err != nil {
			return fmt.Errorf("bad time: %w", err)
		}

		if t.IsZero() {
			v.any = timeLocation(nil)
		} else {
			nsec := t.UnixNano()
			t2 := time.Unix(0, nsec)
			if t.Equal(t2) {
				v.num = uint64(nsec)
				v.any = timeLocation(t.Location())
			} else {
				v.any = timeTime(t.Round(0))
			}
		}
	case KindId:
		var id types.Id

		if err := decoder.Unmarshal(x.Value, &id); err != nil {
			return fmt.Errorf("bad entity ID: %w", err)
		}

		v.any = id
	case KindKeyword:
		var kw types.Keyword

		if err := decoder.Unmarshal(x.Value, &kw); err != nil {
			return fmt.Errorf("bad keyword: %w", err)
		}

		v.any = kw
	case KindArray:
		var ary []Value

		if err := decoder.Unmarshal(x.Value, &ary); err != nil {
			return fmt.Errorf("bad array: %w", err)
		}

		v.any = ary
	case KindComponent:
		var comp EntityComponent

		if err := decoder.Unmarshal(x.Value, &comp); err != nil {
			return fmt.Errorf("bad component: %w", err)
		}

		v.any = &comp
	case KindLabel:
		var label types.Label
		if err := decoder.Unmarshal(x.Value, &label); err != nil {
			return fmt.Errorf("bad label: %w", err)
		}

		v.any = label
	default:
		err := decoder.Unmarshal(x.Value, &v.any)
		if err != nil {
			return fmt.Errorf("bad any: %w", err)
		}
	}

	return nil
}

// String returns an Attr for a string value.
func String(id Id, value string) Attr {
	return Attr{id, StringValue(value)}
}

// Int64 returns an Attr for an int64.
func Int64(id Id, value int64) Attr {
	return Attr{id, Int64Value(value)}
}

// Int converts an int to an int64 and returns
// an Attr with that value.
func Int(id Id, value int) Attr {
	return Int64(id, int64(value))
}

// Uint64 returns an Attr for a uint64.
func Uint64(id Id, v uint64) Attr {
	return Attr{id, Uint64Value(v)}
}

// Float64 returns an Attr for a floating-point number.
func Float64(id Id, v float64) Attr {
	return Attr{id, Float64Value(v)}
}

// Bool returns an Attr for a bool.
func Bool(id Id, v bool) Attr {
	return Attr{id, BoolValue(v)}
}

// Time returns an Attr for a [time.Time].
// It discards the monotonic portion.
func Time(id Id, v time.Time) Attr {
	return Attr{id, TimeValue(v)}
}

func Duration(id Id, v time.Duration) Attr {
	return Attr{id, DurationValue(v)}
}

func Any(id Id, v any) Attr {
	return Attr{id, AnyValue(v)}
}

func Ref(id Id, v Id) Attr {
	return Attr{id, RefValue(v)}
}

func Pair(id Id, x, y Value) Attr {
	return Attr{id, Value{any: []Value{x, y}}}
}

type Keywordable interface {
	string | types.Keyword
}

func Keyword[T Keywordable](id Id, v T) Attr {
	return Attr{id, KeywordValue(v)}
}

func Named[T Keywordable](v T) Attr {
	return Keyword(Ident, v)
}

func Component(id Id, attrs []Attr) Attr {
	return Attr{id, ComponentValue(attrs)}
}

func Label(id Id, key, val string) Attr {
	return Attr{id, LabelValue(key, val)}
}

func Bytes(id Id, b []byte) Attr {
	return Attr{id, BytesValue(b)}
}

type (
	stringptr *byte // used in Value.any when the Value is a string
)

// ValueKind is the kind of a [Value].
type ValueKind int

const (
	KindAny ValueKind = iota
	KindBool
	KindDuration
	KindFloat64
	KindInt64
	KindString
	KindTime
	KindUint64
	KindId
	KindKeyword
	KindArray
	KindComponent
	KindLabel
	KindBytes
)

var kindStrings = []string{
	"Any",
	"Bool",
	"Duration",
	"Float64",
	"Int64",
	"String",
	"Time",
	"Uint64",
	"Id",
	"Keyword",
	"Array",
	"Component",
	"Label",
	"Bytes",
}

func (k ValueKind) String() string {
	if k >= 0 && int(k) < len(kindStrings) {
		return kindStrings[k]
	}
	return "<unknown slog.Kind>"
}

var shortKindStrings = []string{
	"an",
	"bo",
	"du",
	"fl",
	"in",
	"st",
	"tm",
	"ui",
	"id",
	"kw",
	"ar",
	"cp",
	"lb",
	"by",
}

func (k ValueKind) ShortString() string {
	if k >= 0 && int(k) < len(shortKindStrings) {
		return shortKindStrings[k]
	}
	return "<unknown slog.Kind>"
}

// Unexported version of Kind, just so we can store Kinds in Values.
// (No user-provided value has this type.)
type kind ValueKind

// Kind returns v's Kind.
func (v Value) Kind() ValueKind {
	switch x := v.any.(type) {
	case ValueKind:
		return x
	case stringptr:
		return KindString
	case timeLocation, timeTime:
		return KindTime
	case kind: // a kind is just a wrapper for a Kind
		return KindAny
	case types.Id:
		return KindId
	case types.Keyword:
		return KindKeyword
	case []Value:
		return KindArray
	case *EntityComponent:
		return KindComponent
	case types.Label:
		return KindLabel
	case []byte:
		return KindBytes
	default:
		return KindAny
	}
}

// StringValue returns a new [Value] for a string.
func StringValue(value string) Value {
	return Value{num: uint64(len(value)), any: stringptr(unsafe.StringData(value))}
}

// IntValue returns a [Value] for an int.
func IntValue(v int) Value {
	return Int64Value(int64(v))
}

// Int64Value returns a [Value] for an int64.
func Int64Value(v int64) Value {
	return Value{num: uint64(v), any: KindInt64}
}

// Uint64Value returns a [Value] for a uint64.
func Uint64Value(v uint64) Value {
	return Value{num: v, any: KindUint64}
}

// Float64Value returns a [Value] for a floating-point number.
func Float64Value(v float64) Value {
	return Value{num: math.Float64bits(v), any: KindFloat64}
}

// BoolValue returns a [Value] for a bool.
func BoolValue(v bool) Value {
	u := uint64(0)
	if v {
		u = 1
	}
	return Value{num: u, any: KindBool}
}

type (
	// Unexported version of *time.Location, just so we can store *time.Locations in
	// Values. (No user-provided value has this type.)
	timeLocation *time.Location

	// timeTime is for times where UnixNano is undefined.
	timeTime time.Time
)

// TimeValue returns a [Value] for a [time.Time].
// It discards the monotonic portion.
func TimeValue(v time.Time) Value {
	if v.IsZero() {
		// UnixNano on the zero time is undefined, so represent the zero time
		// with a nil *time.Location instead. time.Time.Location method never
		// returns nil, so a Value with any == timeLocation(nil) cannot be
		// mistaken for any other Value, time.Time or otherwise.
		return Value{any: timeLocation(nil)}
	}
	nsec := v.UnixNano()
	t := time.Unix(0, nsec)
	if v.Equal(t) {
		// UnixNano correctly represents the time, so use a zero-alloc representation.
		return Value{num: uint64(nsec), any: timeLocation(v.Location())}
	}
	// Fall back to the general form.
	// Strip the monotonic portion to match the other representation.
	return Value{any: timeTime(v.Round(0))}
}

// DurationValue returns a [Value] for a [time.Duration].
func DurationValue(v time.Duration) Value {
	return Value{num: uint64(v), any: KindDuration}
}

func RefValue(v Id) Value {
	return Value{any: v}
}

func KeywordValue[T Keywordable](v T) Value {
	switch v := any(v).(type) {
	case string:
		return Value{any: MustKeyword(v)}
	case types.Keyword:
		return Value{any: v}
	default:
		panic(fmt.Sprintf("bad string-like type %T", v))
	}
}

func ComponentValue(vals ...any) Value {
	var attrs []Attr

	i := 0
	for i < len(vals) {
		switch v := vals[i].(type) {
		case func() []Attr:
			attrs = append(attrs, v()...)
			i++
		case []Attr:
			attrs = append(attrs, v...)
			i++
		case Attr:
			attrs = append(attrs, v)
			i++
		case Id:
			attrs = append(attrs, Attr{
				ID:    v,
				Value: AnyValue(vals[i+1]),
			})
			i += 2
		default:
			panic(fmt.Sprintf("expected Id key, got %T", v))
		}
	}

	return Value{any: &EntityComponent{attrs: attrs}}
}

func LabelValue(key, val string) Value {
	return Value{
		any: types.Label{
			Key:   key,
			Value: val,
		},
	}
}

func BytesValue(b []byte) Value {
	return Value{
		any: slices.Clone(b),
	}
}

// AnyValue returns a [Value] for the supplied value.
//
// If the supplied value is of type Value, it is returned
// unmodified.
//
// Given a value of one of Go's predeclared string, bool, or
// (non-complex) numeric types, AnyValue returns a Value of kind
// [KindString], [KindBool], [KindUint64], [KindInt64], or [KindFloat64].
// The width of the original numeric type is not preserved.
//
// Given a [time.Time] or [time.Duration] value, AnyValue returns a Value of kind
// [KindTime] or [KindDuration]. The monotonic time is not preserved.
//
// For nil, or values of all other types, including named types whose
// underlying type is numeric, AnyValue returns a value of kind [KindAny].
func AnyValue(v any) Value {
	switch v := v.(type) {
	case string:
		return StringValue(v)
	case int:
		return Int64Value(int64(v))
	case uint:
		return Uint64Value(uint64(v))
	case int64:
		return Int64Value(v)
	case uint64:
		return Uint64Value(v)
	case bool:
		return BoolValue(v)
	case time.Time:
		return TimeValue(v)
	case uint8:
		return Uint64Value(uint64(v))
	case uint16:
		return Uint64Value(uint64(v))
	case uint32:
		return Uint64Value(uint64(v))
	case uintptr:
		return Uint64Value(uint64(v))
	case int8:
		return Int64Value(int64(v))
	case int16:
		return Int64Value(int64(v))
	case int32:
		return Int64Value(int64(v))
	case float64:
		return Float64Value(v)
	case float32:
		return Float64Value(float64(v))
	case types.Id:
		return RefValue(v)
	case types.Keyword:
		return KeywordValue(v)
	case ValueKind:
		return Value{any: kind(v)}
	case Value:
		return v
	case []Value, types.Label, []byte:
		return Value{any: v}
	case *EntityComponent:
		return ComponentValue(v.attrs)
	case []Attr:
		return ComponentValue(v)
	default:
		return Value{any: v}
	}
}

func ArrayValue(values ...any) Value {
	var ary []Value

	for _, v := range values {
		ary = append(ary, AnyValue(v))
	}

	return Value{any: ary}
}

//////////////// Accessors

// Any returns v's value as an any.
func (v Value) Any() any {
	switch v.Kind() {
	case KindAny:
		if k, ok := v.any.(kind); ok {
			return ValueKind(k)
		}
		return v.any
	case KindInt64:
		return int64(v.num)
	case KindUint64:
		return v.num
	case KindFloat64:
		return v.float()
	case KindString:
		return v.str()
	case KindBool:
		return v.bool()
	case KindDuration:
		return v.duration()
	case KindTime:
		return v.time()
	case KindId:
		return v.any
	case KindKeyword:
		return v.any
	case KindArray:
		return v.any
	case KindComponent:
		return v.any
	case KindLabel:
		return v.any
	case KindBytes:
		return v.any
	default:
		panic(fmt.Sprintf("bad kind: %s", v.Kind()))
	}
}

// String returns Value's value as a string, formatted like [fmt.Sprint]. Unlike
// the methods Int64, Float64, and so on, which panic if v is of the
// wrong kind, String never panics.
func (v Value) String() string {
	if sp, ok := v.any.(stringptr); ok {
		return unsafe.String(sp, v.num)
	}

	switch v.Kind() {
	case KindInt64, KindUint64, KindFloat64, KindBool, KindString:
		var buf []byte
		return string(v.append(buf))
	default:
		var buf []byte
		return v.Kind().ShortString() + ": " + string(v.append(buf))
	}
}

func (v Value) str() string {
	return unsafe.String(v.any.(stringptr), v.num)
}

// Int64 returns v's value as an int64. It panics
// if v is not a signed integer.
func (v Value) Int64() int64 {
	if g, w := v.Kind(), KindInt64; g != w {
		panic(fmt.Sprintf("Value kind is %s, not %s", g, w))
	}
	return int64(v.num)
}

// Uint64 returns v's value as a uint64. It panics
// if v is not an unsigned integer.
func (v Value) Uint64() uint64 {
	if g, w := v.Kind(), KindUint64; g != w {
		panic(fmt.Sprintf("Value kind is %s, not %s", g, w))
	}
	return v.num
}

// Bool returns v's value as a bool. It panics
// if v is not a bool.
func (v Value) Bool() bool {
	if g, w := v.Kind(), KindBool; g != w {
		panic(fmt.Sprintf("Value kind is %s, not %s", g, w))
	}
	return v.bool()
}

func (v Value) bool() bool {
	return v.num == 1
}

// Duration returns v's value as a [time.Duration]. It panics
// if v is not a time.Duration.
func (v Value) Duration() time.Duration {
	if g, w := v.Kind(), KindDuration; g != w {
		panic(fmt.Sprintf("Value kind is %s, not %s", g, w))
	}

	return v.duration()
}

func (v Value) duration() time.Duration {
	return time.Duration(int64(v.num))
}

// Float64 returns v's value as a float64. It panics
// if v is not a float64.
func (v Value) Float64() float64 {
	if g, w := v.Kind(), KindFloat64; g != w {
		panic(fmt.Sprintf("Value kind is %s, not %s", g, w))
	}

	return v.float()
}

func (v Value) float() float64 {
	return math.Float64frombits(v.num)
}

// Time returns v's value as a [time.Time]. It panics
// if v is not a time.Time.
func (v Value) Time() time.Time {
	if g, w := v.Kind(), KindTime; g != w {
		panic(fmt.Sprintf("Value kind is %s, not %s", g, w))
	}
	return v.time()
}

// See TimeValue to understand how times are represented.
func (v Value) time() time.Time {
	switch a := v.any.(type) {
	case timeLocation:
		if a == nil {
			return time.Time{}
		}
		return time.Unix(0, int64(v.num)).In(a)
	case timeTime:
		return time.Time(a)
	default:
		panic(fmt.Sprintf("bad time type %T", v.any))
	}
}

func (v Value) Id() types.Id {
	if v, ok := v.any.(types.Id); ok {
		return v
	}

	panic(fmt.Sprintf("Value kind is %s, not %s", v.Kind(), KindId))
}

func (v Value) Keyword() types.Keyword {
	if v, ok := v.any.(types.Keyword); ok {
		return v
	}

	panic(fmt.Sprintf("Value kind is %s, not %s", v.Kind(), KindKeyword))
}

func (v Value) Array() []Value {
	if v, ok := v.any.([]Value); ok {
		return v
	}

	panic(fmt.Sprintf("Value kind is %s, not %s", v.Kind(), KindArray))
}

func (v Value) Component() *EntityComponent {
	if v, ok := v.any.(*EntityComponent); ok {
		return v
	}

	panic(fmt.Sprintf("Value kind is %s, not %s", v.Kind(), KindComponent))
}

func (v Value) Label() types.Label {
	if v, ok := v.any.(types.Label); ok {
		return v
	}

	panic(fmt.Sprintf("Value kind is %s, not %s", v.Kind(), KindLabel))
}

func (v Value) Bytes() []byte {
	if v, ok := v.any.([]byte); ok {
		return v
	}

	panic(fmt.Sprintf("Value kind is %s, not %s", v.Kind(), KindBytes))
}

//////////////// Other

// Clone returns a deep copy of the Value.
// For bytes, arrays, and components, this creates new copies of the underlying data.
// For other types (strings, primitives, immutable types), a shallow copy is sufficient.
func (v Value) Clone() Value {
	switch v.Kind() {
	case KindBytes:
		return BytesValue(slices.Clone(v.Bytes()))
	case KindArray:
		arr := v.Array()
		cloned := make([]Value, len(arr))
		for i, val := range arr {
			cloned[i] = val.Clone()
		}
		return Value{any: cloned}
	case KindComponent:
		comp := v.Component()
		clonedAttrs := make([]Attr, len(comp.attrs))
		for i, attr := range comp.attrs {
			clonedAttrs[i] = Attr{
				ID:    attr.ID,
				Value: attr.Value.Clone(),
			}
		}
		return Value{any: &EntityComponent{attrs: clonedAttrs}}
	default:
		// For all other types (strings, primitives, time, id, keyword, label),
		// the value is either stored in num or is an immutable type, so shallow copy is fine
		return v
	}
}

// Equal reports whether v and w represent the same Go value.
func (v Value) Equal(w Value) bool {
	return v.Compare(w) == 0
}

func (a Attr) Compare(b Attr) int {
	if a.ID != b.ID {
		return cmp.Compare(a.ID, b.ID)
	}

	return a.Value.Compare(b.Value)
}

// Equal reports whether v and w represent the same Go value.
func (v Value) Compare(w Value) int {
	k1 := v.Kind()
	k2 := w.Kind()
	if k1 != k2 {
		if k1 < k2 {
			return -1
		}

		return 1
	}
	switch k1 {
	case KindInt64, KindUint64, KindBool, KindDuration:
		return cmp.Compare(v.num, w.num)
	case KindString:
		return cmp.Compare(v.str(), w.str())
	case KindFloat64:
		return cmp.Compare(v.float(), w.float())
	case KindTime:
		return v.time().Compare(w.time())
	case KindBytes:
		return bytes.Compare(v.Bytes(), w.Bytes())
	case KindId:
		return cmp.Compare(v.Id(), w.Id())
	case KindKeyword:
		return cmp.Compare(v.Keyword(), w.Keyword())
	case KindComponent:
		return v.Component().Compare(w.Component())
	case KindArray:
		if len(v.Array()) != len(w.Array()) {
			return cmp.Compare(len(v.Array()), len(w.Array()))
		}
		if len(v.Array()) == 0 {
			return 0
		}

		for i := 0; i < len(v.Array()); i++ {
			if cmp := v.Array()[i].Compare(w.Array()[i]); cmp != 0 {
				return cmp
			}
		}

		return 0
	case KindLabel:
		if v.Label().Key != w.Label().Key {
			return cmp.Compare(v.Label().Key, w.Label().Key)
		}

		return cmp.Compare(v.Label().Value, w.Label().Value)

	case KindAny:
		if v.any == w.any {
			return 0
		}

		return -1
	default:
		panic(fmt.Sprintf("bad kind: %s", k1))
	}
}

func (e *EntityComponent) Compare(other *EntityComponent) int {
	if len(e.attrs) != len(other.attrs) {
		return cmp.Compare(len(e.attrs), len(other.attrs))
	}

	for i := 0; i < len(e.attrs); i++ {
		if cmp := e.attrs[i].Compare(other.attrs[i]); cmp != 0 {
			return cmp
		}
	}

	return 0
}

func (e *EntityComponent) Equal(other *EntityComponent) bool {
	return e.Compare(other) == 0
}

// append appends a text representation of v to dst.
// v is formatted as with fmt.Sprint.
func (v Value) append(dst []byte) []byte {
	switch v.Kind() {
	case KindString:
		return append(dst, v.str()...)
	case KindInt64:
		return strconv.AppendInt(dst, int64(v.num), 10)
	case KindUint64:
		return strconv.AppendUint(dst, v.num, 10)
	case KindFloat64:
		return strconv.AppendFloat(dst, v.float(), 'g', -1, 64)
	case KindBool:
		return strconv.AppendBool(dst, v.bool())
	case KindDuration:
		return append(dst, v.duration().String()...)
	case KindTime:
		return append(dst, v.time().String()...)
	case KindArray:
		values := v.Array()
		for i, v := range values {
			dst = v.append(dst)
			if i < len(values)-1 {
				dst = append(dst, []byte(", ")...)
			}
		}
		return dst
	case KindAny, KindId, KindKeyword, KindComponent:
		return fmt.Append(dst, v.any)
	case KindLabel:
		label := v.Label()
		dst = append(dst, label.Key...)
		dst = append(dst, '=')
		dst = append(dst, label.Value...)
		return dst
	case KindBytes:
		dst = append(dst, base58.Encode(v.Bytes())...)
		return dst
	default:
		panic(fmt.Sprintf("bad kind: %s", v.Kind()))
	}
}

// append appends a text representation of v to dst.
// v is formatted as with fmt.Sprint.
//
//nolint:errcheck
func (v Value) sum(w io.Writer) {
	switch v.Kind() {
	case KindString:
		w.Write([]byte(v.str()))
	case KindInt64, KindUint64, KindFloat64, KindDuration:
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], v.num)
		w.Write(b[:])
	case KindBool:
		if v.bool() {
			w.Write([]byte{1})
		} else {
			w.Write([]byte{0})
		}
	case KindTime:
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(v.time().UnixNano()))
		w.Write(b[:])
	case KindId, KindKeyword, KindAny:
		fmt.Fprint(w, v.any)
	case KindComponent:
		for _, a := range v.Component().attrs {
			a.Sum(w)
			w.Write([]byte{';'})
		}
	case KindArray:
		for _, v := range v.Array() {
			v.sum(w)
			w.Write([]byte{','})
		}
	default:
		w.Write(v.append(nil))
	}
}
