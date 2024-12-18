package example

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type readingData struct {
	Temperature *float32 `cbor:"0,keyasint,omitempty" json:"temperature,omitempty"`
	Seconds     *int32   `cbor:"1,keyasint,omitempty" json:"seconds,omitempty"`
	Meter       *string  `cbor:"2,keyasint,omitempty" json:"meter,omitempty"`
}

type Reading struct {
	data readingData
}

func (v *Reading) HasTemperature() bool {
	return v.data.Temperature != nil
}

func (v *Reading) Temperature() float32 {
	if v.data.Temperature == nil {
		return 0
	}
	return *v.data.Temperature
}

func (v *Reading) SetTemperature(temperature float32) {
	v.data.Temperature = &temperature
}

func (v *Reading) HasSeconds() bool {
	return v.data.Seconds != nil
}

func (v *Reading) Seconds() int32 {
	if v.data.Seconds == nil {
		return 0
	}
	return *v.data.Seconds
}

func (v *Reading) SetSeconds(seconds int32) {
	v.data.Seconds = &seconds
}

func (v *Reading) HasMeter() bool {
	return v.data.Meter != nil
}

func (v *Reading) Meter() string {
	if v.data.Meter == nil {
		return ""
	}
	return *v.data.Meter
}

func (v *Reading) SetMeter(meter string) {
	v.data.Meter = &meter
}

func (v *Reading) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Reading) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Reading) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Reading) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type meterReadTemperatureArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type MeterReadTemperatureArgs struct {
	call *rpc.Call
	data meterReadTemperatureArgsData
}

func (v *MeterReadTemperatureArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *MeterReadTemperatureArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *MeterReadTemperatureArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *MeterReadTemperatureArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *MeterReadTemperatureArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *MeterReadTemperatureArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type meterReadTemperatureResultsData struct {
	Reading *Reading `cbor:"0,keyasint,omitempty" json:"reading,omitempty"`
}

type MeterReadTemperatureResults struct {
	call *rpc.Call
	data meterReadTemperatureResultsData
}

func (v *MeterReadTemperatureResults) SetReading(reading *Reading) {
	v.data.Reading = reading
}

func (v *MeterReadTemperatureResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *MeterReadTemperatureResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *MeterReadTemperatureResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *MeterReadTemperatureResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type meterGetSetterArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type MeterGetSetterArgs struct {
	call *rpc.Call
	data meterGetSetterArgsData
}

func (v *MeterGetSetterArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *MeterGetSetterArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *MeterGetSetterArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *MeterGetSetterArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *MeterGetSetterArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *MeterGetSetterArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type meterGetSetterResultsData struct {
	Setter rpc.OID `cbor:"0,keyasint,omitempty" json:"setter,omitempty"`
}

type MeterGetSetterResults struct {
	call *rpc.Call
	data meterGetSetterResultsData
}

func (v *MeterGetSetterResults) SetSetter(setter SetTemp) {
	v.data.Setter = v.call.NewOID(AdaptSetTemp(setter))
}

func (v *MeterGetSetterResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *MeterGetSetterResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *MeterGetSetterResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *MeterGetSetterResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type MeterReadTemperature struct {
	*rpc.Call
	args    MeterReadTemperatureArgs
	results MeterReadTemperatureResults
}

func (t *MeterReadTemperature) Args() *MeterReadTemperatureArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *MeterReadTemperature) Results() *MeterReadTemperatureResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type MeterGetSetter struct {
	*rpc.Call
	args    MeterGetSetterArgs
	results MeterGetSetterResults
}

func (t *MeterGetSetter) Args() *MeterGetSetterArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *MeterGetSetter) Results() *MeterGetSetterResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Meter interface {
	ReadTemperature(ctx context.Context, state *MeterReadTemperature) error
	GetSetter(ctx context.Context, state *MeterGetSetter) error
}

func AdaptMeter(t Meter) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:  "readTemperature",
			Index: 0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.ReadTemperature(ctx, &MeterReadTemperature{Call: call})
			},
		},
		{
			Name:  "getSetter",
			Index: 1,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.GetSetter(ctx, &MeterGetSetter{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods)
}

type MeterClient struct {
	*rpc.Client
}

type MeterClientReadTemperatureResults struct {
	client *rpc.Client
	data   meterReadTemperatureResultsData
}

func (v *MeterClientReadTemperatureResults) HasReading() bool {
	return v.data.Reading != nil
}

func (v *MeterClientReadTemperatureResults) Reading() *Reading {
	return v.data.Reading
}

func (v MeterClient) ReadTemperature(ctx context.Context, name string) (*MeterClientReadTemperatureResults, error) {
	args := MeterReadTemperatureArgs{}
	args.data.Name = &name

	var ret meterReadTemperatureResultsData

	err := v.Client.Call(ctx, "readTemperature", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &MeterClientReadTemperatureResults{client: v.Client, data: ret}, nil
}

type MeterClientGetSetterResults struct {
	client *rpc.Client
	data   meterGetSetterResultsData
}

func (v *MeterClientGetSetterResults) Setter() SetTempClient {
	return SetTempClient{
		Client: v.client.NewClient(v.data.Setter),
	}
}

func (v MeterClient) GetSetter(ctx context.Context, name string) (*MeterClientGetSetterResults, error) {
	args := MeterGetSetterArgs{}
	args.data.Name = &name

	var ret meterGetSetterResultsData

	err := v.Client.Call(ctx, "getSetter", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &MeterClientGetSetterResults{client: v.Client, data: ret}, nil
}

type setTempSetTempArgsData struct {
	Temp *int32 `cbor:"0,keyasint,omitempty" json:"temp,omitempty"`
}

type SetTempSetTempArgs struct {
	call *rpc.Call
	data setTempSetTempArgsData
}

func (v *SetTempSetTempArgs) HasTemp() bool {
	return v.data.Temp != nil
}

func (v *SetTempSetTempArgs) Temp() int32 {
	if v.data.Temp == nil {
		return 0
	}
	return *v.data.Temp
}

func (v *SetTempSetTempArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SetTempSetTempArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SetTempSetTempArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SetTempSetTempArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type setTempSetTempResultsData struct {
	Temp *int32 `cbor:"0,keyasint,omitempty" json:"temp,omitempty"`
}

type SetTempSetTempResults struct {
	call *rpc.Call
	data setTempSetTempResultsData
}

func (v *SetTempSetTempResults) SetTemp(temp int32) {
	v.data.Temp = &temp
}

func (v *SetTempSetTempResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SetTempSetTempResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SetTempSetTempResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SetTempSetTempResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SetTempSetTemp struct {
	*rpc.Call
	args    SetTempSetTempArgs
	results SetTempSetTempResults
}

func (t *SetTempSetTemp) Args() *SetTempSetTempArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SetTempSetTemp) Results() *SetTempSetTempResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SetTemp interface {
	SetTemp(ctx context.Context, state *SetTempSetTemp) error
}

func AdaptSetTemp(t SetTemp) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:  "setTemp",
			Index: 0,
			Handler: func(ctx context.Context, call *rpc.Call) error {
				return t.SetTemp(ctx, &SetTempSetTemp{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods)
}

type SetTempClient struct {
	*rpc.Client
}

type SetTempClientSetTempResults struct {
	client *rpc.Client
	data   setTempSetTempResultsData
}

func (v *SetTempClientSetTempResults) HasTemp() bool {
	return v.data.Temp != nil
}

func (v *SetTempClientSetTempResults) Temp() int32 {
	if v.data.Temp == nil {
		return 0
	}
	return *v.data.Temp
}

func (v SetTempClient) SetTemp(ctx context.Context, temp int32) (*SetTempClientSetTempResults, error) {
	args := SetTempSetTempArgs{}
	args.data.Temp = &temp

	var ret setTempSetTempResultsData

	err := v.Client.Call(ctx, "setTemp", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SetTempClientSetTempResults{client: v.Client, data: ret}, nil
}
