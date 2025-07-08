package example

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
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

type ValueV interface {
	Which() string
	I() int64
	SetI(int64)
	S() string
	SetS(string)
}

type valueV struct {
	U_I *int64  `cbor:"0,keyasint,omitempty" json:"i,omitempty"`
	U_S *string `cbor:"1,keyasint,omitempty" json:"s,omitempty"`
}

func (v *valueV) Which() string {
	if v.U_I != nil {
		return "i"
	}
	if v.U_S != nil {
		return "s"
	}
	return ""
}

func (v *valueV) I() int64 {
	if v.U_I == nil {
		return 0
	}
	return *v.U_I
}

func (v *valueV) SetI(val int64) {
	v.U_S = nil
	v.U_I = &val
}

func (v *valueV) S() string {
	if v.U_S == nil {
		return ""
	}
	return *v.U_S
}

func (v *valueV) SetS(val string) {
	v.U_I = nil
	v.U_S = &val
}

type valueData struct {
	valueV
}

type Value struct {
	data valueData
}

func (v *Value) V() ValueV {
	return &v.data.valueV
}

func (v *Value) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Value) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Value) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Value) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type meterReadTemperatureArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type MeterReadTemperatureArgs struct {
	call rpc.Call
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
	call rpc.Call
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
	call rpc.Call
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
	Setter *rpc.Capability `cbor:"0,keyasint,omitempty" json:"setter,omitempty"`
}

type MeterGetSetterResults struct {
	call rpc.Call
	data meterGetSetterResultsData
}

func (v *MeterGetSetterResults) SetSetter(setter SetTemp) {
	v.data.Setter = v.call.NewCapability(AdaptSetTemp(setter))
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
	rpc.Call
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
	rpc.Call
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

type reexportMeter struct {
	client rpc.Client
}

func (_ reexportMeter) ReadTemperature(ctx context.Context, state *MeterReadTemperature) error {
	panic("not implemented")
}

func (_ reexportMeter) GetSetter(ctx context.Context, state *MeterGetSetter) error {
	panic("not implemented")
}

func (t reexportMeter) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptMeter(t Meter) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "readTemperature",
			InterfaceName: "Meter",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ReadTemperature(ctx, &MeterReadTemperature{Call: call})
			},
		},
		{
			Name:          "getSetter",
			InterfaceName: "Meter",
			Index:         1,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetSetter(ctx, &MeterGetSetter{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type MeterClient struct {
	rpc.Client
}

func NewMeterClient(client rpc.Client) *MeterClient {
	return &MeterClient{Client: client}
}

func (c MeterClient) Export() Meter {
	return reexportMeter{client: c.Client}
}

type MeterClientReadTemperatureResults struct {
	client rpc.Client
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

	err := v.Call(ctx, "readTemperature", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &MeterClientReadTemperatureResults{client: v.Client, data: ret}, nil
}

type MeterClientGetSetterResults struct {
	client rpc.Client
	data   meterGetSetterResultsData
}

func (v *MeterClientGetSetterResults) Setter() *SetTempClient {
	return &SetTempClient{
		Client: v.client.NewClient(v.data.Setter),
	}
}

func (v MeterClient) GetSetter(ctx context.Context, name string) (*MeterClientGetSetterResults, error) {
	args := MeterGetSetterArgs{}
	args.data.Name = &name

	var ret meterGetSetterResultsData

	err := v.Call(ctx, "getSetter", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &MeterClientGetSetterResults{client: v.Client, data: ret}, nil
}

type setTempSetTempArgsData struct {
	Temp *int32 `cbor:"0,keyasint,omitempty" json:"temp,omitempty"`
}

type SetTempSetTempArgs struct {
	call rpc.Call
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
	call rpc.Call
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
	rpc.Call
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

type reexportSetTemp struct {
	client rpc.Client
}

func (_ reexportSetTemp) SetTemp(ctx context.Context, state *SetTempSetTemp) error {
	panic("not implemented")
}

func (t reexportSetTemp) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSetTemp(t SetTemp) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "setTemp",
			InterfaceName: "SetTemp",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetTemp(ctx, &SetTempSetTemp{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SetTempClient struct {
	rpc.Client
}

func NewSetTempClient(client rpc.Client) *SetTempClient {
	return &SetTempClient{Client: client}
}

func (c SetTempClient) Export() SetTemp {
	return reexportSetTemp{client: c.Client}
}

type SetTempClientSetTempResults struct {
	client rpc.Client
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

	err := v.Call(ctx, "setTemp", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SetTempClientSetTempResults{client: v.Client, data: ret}, nil
}

type updateReceiverUpdateArgsData struct {
	Reading *Reading `cbor:"0,keyasint,omitempty" json:"reading,omitempty"`
}

type UpdateReceiverUpdateArgs struct {
	call rpc.Call
	data updateReceiverUpdateArgsData
}

func (v *UpdateReceiverUpdateArgs) HasReading() bool {
	return v.data.Reading != nil
}

func (v *UpdateReceiverUpdateArgs) Reading() *Reading {
	return v.data.Reading
}

func (v *UpdateReceiverUpdateArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UpdateReceiverUpdateArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UpdateReceiverUpdateArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UpdateReceiverUpdateArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type updateReceiverUpdateResultsData struct{}

type UpdateReceiverUpdateResults struct {
	call rpc.Call
	data updateReceiverUpdateResultsData
}

func (v *UpdateReceiverUpdateResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UpdateReceiverUpdateResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UpdateReceiverUpdateResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UpdateReceiverUpdateResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type UpdateReceiverUpdate struct {
	rpc.Call
	args    UpdateReceiverUpdateArgs
	results UpdateReceiverUpdateResults
}

func (t *UpdateReceiverUpdate) Args() *UpdateReceiverUpdateArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *UpdateReceiverUpdate) Results() *UpdateReceiverUpdateResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type UpdateReceiver interface {
	Update(ctx context.Context, state *UpdateReceiverUpdate) error
}

type reexportUpdateReceiver struct {
	client rpc.Client
}

func (_ reexportUpdateReceiver) Update(ctx context.Context, state *UpdateReceiverUpdate) error {
	panic("not implemented")
}

func (t reexportUpdateReceiver) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptUpdateReceiver(t UpdateReceiver) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "update",
			InterfaceName: "UpdateReceiver",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Update(ctx, &UpdateReceiverUpdate{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type UpdateReceiverClient struct {
	rpc.Client
}

func NewUpdateReceiverClient(client rpc.Client) *UpdateReceiverClient {
	return &UpdateReceiverClient{Client: client}
}

func (c UpdateReceiverClient) Export() UpdateReceiver {
	return reexportUpdateReceiver{client: c.Client}
}

type UpdateReceiverClientUpdateResults struct {
	client rpc.Client
	data   updateReceiverUpdateResultsData
}

func (v UpdateReceiverClient) Update(ctx context.Context, reading *Reading) (*UpdateReceiverClientUpdateResults, error) {
	args := UpdateReceiverUpdateArgs{}
	args.data.Reading = reading

	var ret updateReceiverUpdateResultsData

	err := v.Call(ctx, "update", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &UpdateReceiverClientUpdateResults{client: v.Client, data: ret}, nil
}

type meterUpdatesRegisterUpdatesArgsData struct {
	Recv *rpc.Capability `cbor:"0,keyasint,omitempty" json:"recv,omitempty"`
}

type MeterUpdatesRegisterUpdatesArgs struct {
	call rpc.Call
	data meterUpdatesRegisterUpdatesArgsData
}

func (v *MeterUpdatesRegisterUpdatesArgs) HasRecv() bool {
	return v.data.Recv != nil
}

func (v *MeterUpdatesRegisterUpdatesArgs) Recv() *UpdateReceiverClient {
	if v.data.Recv == nil {
		return nil
	}
	return &UpdateReceiverClient{Client: v.call.NewClient(v.data.Recv)}
}

func (v *MeterUpdatesRegisterUpdatesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *MeterUpdatesRegisterUpdatesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *MeterUpdatesRegisterUpdatesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *MeterUpdatesRegisterUpdatesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type meterUpdatesRegisterUpdatesResultsData struct{}

type MeterUpdatesRegisterUpdatesResults struct {
	call rpc.Call
	data meterUpdatesRegisterUpdatesResultsData
}

func (v *MeterUpdatesRegisterUpdatesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *MeterUpdatesRegisterUpdatesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *MeterUpdatesRegisterUpdatesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *MeterUpdatesRegisterUpdatesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type MeterUpdatesRegisterUpdates struct {
	rpc.Call
	args    MeterUpdatesRegisterUpdatesArgs
	results MeterUpdatesRegisterUpdatesResults
}

func (t *MeterUpdatesRegisterUpdates) Args() *MeterUpdatesRegisterUpdatesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *MeterUpdatesRegisterUpdates) Results() *MeterUpdatesRegisterUpdatesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type MeterUpdates interface {
	RegisterUpdates(ctx context.Context, state *MeterUpdatesRegisterUpdates) error
}

type reexportMeterUpdates struct {
	client rpc.Client
}

func (_ reexportMeterUpdates) RegisterUpdates(ctx context.Context, state *MeterUpdatesRegisterUpdates) error {
	panic("not implemented")
}

func (t reexportMeterUpdates) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptMeterUpdates(t MeterUpdates) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "registerUpdates",
			InterfaceName: "MeterUpdates",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.RegisterUpdates(ctx, &MeterUpdatesRegisterUpdates{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type MeterUpdatesClient struct {
	rpc.Client
}

func NewMeterUpdatesClient(client rpc.Client) *MeterUpdatesClient {
	return &MeterUpdatesClient{Client: client}
}

func (c MeterUpdatesClient) Export() MeterUpdates {
	return reexportMeterUpdates{client: c.Client}
}

type MeterUpdatesClientRegisterUpdatesResults struct {
	client rpc.Client
	data   meterUpdatesRegisterUpdatesResultsData
}

func (v MeterUpdatesClient) RegisterUpdates(ctx context.Context, recv UpdateReceiver) (*MeterUpdatesClientRegisterUpdatesResults, error) {
	args := MeterUpdatesRegisterUpdatesArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	{
		ic, oid, c := v.NewInlineCapability(AdaptUpdateReceiver(recv), recv)
		args.data.Recv = c
		caps[oid] = ic
	}

	var ret meterUpdatesRegisterUpdatesResultsData

	err := v.CallWithCaps(ctx, "registerUpdates", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &MeterUpdatesClientRegisterUpdatesResults{client: v.Client, data: ret}, nil
}

type adjustTempAdjustArgsData struct {
	Setter *rpc.Capability `cbor:"0,keyasint,omitempty" json:"setter,omitempty"`
}

type AdjustTempAdjustArgs struct {
	call rpc.Call
	data adjustTempAdjustArgsData
}

func (v *AdjustTempAdjustArgs) HasSetter() bool {
	return v.data.Setter != nil
}

func (v *AdjustTempAdjustArgs) Setter() *SetTempClient {
	if v.data.Setter == nil {
		return nil
	}
	return &SetTempClient{Client: v.call.NewClient(v.data.Setter)}
}

func (v *AdjustTempAdjustArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdjustTempAdjustArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdjustTempAdjustArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdjustTempAdjustArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type adjustTempAdjustResultsData struct{}

type AdjustTempAdjustResults struct {
	call rpc.Call
	data adjustTempAdjustResultsData
}

func (v *AdjustTempAdjustResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AdjustTempAdjustResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AdjustTempAdjustResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AdjustTempAdjustResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type AdjustTempAdjust struct {
	rpc.Call
	args    AdjustTempAdjustArgs
	results AdjustTempAdjustResults
}

func (t *AdjustTempAdjust) Args() *AdjustTempAdjustArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AdjustTempAdjust) Results() *AdjustTempAdjustResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AdjustTemp interface {
	Adjust(ctx context.Context, state *AdjustTempAdjust) error
}

type reexportAdjustTemp struct {
	client rpc.Client
}

func (_ reexportAdjustTemp) Adjust(ctx context.Context, state *AdjustTempAdjust) error {
	panic("not implemented")
}

func (t reexportAdjustTemp) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptAdjustTemp(t AdjustTemp) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "adjust",
			InterfaceName: "AdjustTemp",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Adjust(ctx, &AdjustTempAdjust{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type AdjustTempClient struct {
	rpc.Client
}

func NewAdjustTempClient(client rpc.Client) *AdjustTempClient {
	return &AdjustTempClient{Client: client}
}

func (c AdjustTempClient) Export() AdjustTemp {
	return reexportAdjustTemp{client: c.Client}
}

type AdjustTempClientAdjustResults struct {
	client rpc.Client
	data   adjustTempAdjustResultsData
}

func (v AdjustTempClient) Adjust(ctx context.Context, setter SetTemp) (*AdjustTempClientAdjustResults, error) {
	args := AdjustTempAdjustArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	{
		ic, oid, c := v.NewInlineCapability(AdaptSetTemp(setter), setter)
		args.data.Setter = c
		caps[oid] = ic
	}

	var ret adjustTempAdjustResultsData

	err := v.CallWithCaps(ctx, "adjust", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &AdjustTempClientAdjustResults{client: v.Client, data: ret}, nil
}

type setTempGSetTempArgsData[T any] struct {
	Temp *T `cbor:"0,keyasint,omitempty" json:"temp,omitempty"`
}

type SetTempGSetTempArgs[T any] struct {
	call rpc.Call
	data setTempGSetTempArgsData[T]
}

func (v *SetTempGSetTempArgs[T]) HasTemp() bool {
	return v.data.Temp != nil
}

func (v *SetTempGSetTempArgs[T]) Temp() T {
	if v.data.Temp == nil {
		return rpc.Zero[T]()
	}
	return *v.data.Temp
}

func (v *SetTempGSetTempArgs[T]) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SetTempGSetTempArgs[T]) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SetTempGSetTempArgs[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SetTempGSetTempArgs[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type setTempGSetTempResultsData[T any] struct{}

type SetTempGSetTempResults[T any] struct {
	call rpc.Call
	data setTempGSetTempResultsData[T]
}

func (v *SetTempGSetTempResults[T]) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SetTempGSetTempResults[T]) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SetTempGSetTempResults[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SetTempGSetTempResults[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SetTempGSetTemp[T any] struct {
	rpc.Call
	args    SetTempGSetTempArgs[T]
	results SetTempGSetTempResults[T]
}

func (t *SetTempGSetTemp[T]) Args() *SetTempGSetTempArgs[T] {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SetTempGSetTemp[T]) Results() *SetTempGSetTempResults[T] {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SetTempG[T any] interface {
	SetTemp(ctx context.Context, state *SetTempGSetTemp[T]) error
}

type reexportSetTempG[T any] struct {
	client rpc.Client
}

func (_ reexportSetTempG[T]) SetTemp(ctx context.Context, state *SetTempGSetTemp[T]) error {
	panic("not implemented")
}

func (t reexportSetTempG[T]) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSetTempG[T any](t SetTempG[T]) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "setTemp",
			InterfaceName: "SetTempG",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetTemp(ctx, &SetTempGSetTemp[T]{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SetTempGClient[T any] struct {
	rpc.Client
}

func NewSetTempGClient[T any](client rpc.Client) *SetTempGClient[T] {
	return &SetTempGClient[T]{Client: client}
}

func (c SetTempGClient[T]) Export() SetTempG[T] {
	return reexportSetTempG[T]{client: c.Client}
}

type SetTempGClientSetTempResults[T any] struct {
	client rpc.Client
	data   setTempGSetTempResultsData[T]
}

func (v SetTempGClient[T]) SetTemp(ctx context.Context, temp T) (*SetTempGClientSetTempResults[T], error) {
	args := SetTempGSetTempArgs[T]{}
	args.data.Temp = &temp

	var ret setTempGSetTempResultsData[T]

	err := v.Call(ctx, "setTemp", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SetTempGClientSetTempResults[T]{client: v.Client, data: ret}, nil
}

type emitTempsEmitArgsData struct {
	Emitter *rpc.Capability `cbor:"0,keyasint,omitempty" json:"emitter,omitempty"`
}

type EmitTempsEmitArgs struct {
	call rpc.Call
	data emitTempsEmitArgsData
}

func (v *EmitTempsEmitArgs) HasEmitter() bool {
	return v.data.Emitter != nil
}

func (v *EmitTempsEmitArgs) Emitter() *stream.SendStreamClient[float32] {
	if v.data.Emitter == nil {
		return nil
	}
	return &stream.SendStreamClient[float32]{Client: v.call.NewClient(v.data.Emitter)}
}

func (v *EmitTempsEmitArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EmitTempsEmitArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EmitTempsEmitArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EmitTempsEmitArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type emitTempsEmitResultsData struct{}

type EmitTempsEmitResults struct {
	call rpc.Call
	data emitTempsEmitResultsData
}

func (v *EmitTempsEmitResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EmitTempsEmitResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EmitTempsEmitResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EmitTempsEmitResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type EmitTempsEmit struct {
	rpc.Call
	args    EmitTempsEmitArgs
	results EmitTempsEmitResults
}

func (t *EmitTempsEmit) Args() *EmitTempsEmitArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EmitTempsEmit) Results() *EmitTempsEmitResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type EmitTemps interface {
	Emit(ctx context.Context, state *EmitTempsEmit) error
}

type reexportEmitTemps struct {
	client rpc.Client
}

func (_ reexportEmitTemps) Emit(ctx context.Context, state *EmitTempsEmit) error {
	panic("not implemented")
}

func (t reexportEmitTemps) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptEmitTemps(t EmitTemps) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "emit",
			InterfaceName: "EmitTemps",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Emit(ctx, &EmitTempsEmit{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type EmitTempsClient struct {
	rpc.Client
}

func NewEmitTempsClient(client rpc.Client) *EmitTempsClient {
	return &EmitTempsClient{Client: client}
}

func (c EmitTempsClient) Export() EmitTemps {
	return reexportEmitTemps{client: c.Client}
}

type EmitTempsClientEmitResults struct {
	client rpc.Client
	data   emitTempsEmitResultsData
}

func (v EmitTempsClient) Emit(ctx context.Context, emitter stream.SendStream[float32]) (*EmitTempsClientEmitResults, error) {
	args := EmitTempsEmitArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[float32](emitter), emitter)
		args.data.Emitter = c
		caps[oid] = ic
	}

	var ret emitTempsEmitResultsData

	err := v.CallWithCaps(ctx, "emit", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &EmitTempsClientEmitResults{client: v.Client, data: ret}, nil
}
