package rpc

import (
	"context"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
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

type townGetHeroArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type TownGetHeroArgs struct {
	call rpc.Call
	data townGetHeroArgsData
}

func (v *TownGetHeroArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *TownGetHeroArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *TownGetHeroArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *TownGetHeroArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *TownGetHeroArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *TownGetHeroArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type townGetHeroResultsData struct {
	Hero    *Hero           `cbor:"0,keyasint,omitempty" json:"hero,omitempty"`
	Empower *rpc.Capability `cbor:"1,keyasint,omitempty" json:"empower,omitempty"`
}

type TownGetHeroResults struct {
	call rpc.Call
	data townGetHeroResultsData
}

func (v *TownGetHeroResults) SetHero(hero *Hero) {
	v.data.Hero = hero
}

func (v *TownGetHeroResults) SetEmpower(empower Empower) {
	v.data.Empower = v.call.NewCapability(AdaptEmpower(empower))
}

func (v *TownGetHeroResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *TownGetHeroResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *TownGetHeroResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *TownGetHeroResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type townHireHeroArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type TownHireHeroArgs struct {
	call rpc.Call
	data townHireHeroArgsData
}

func (v *TownHireHeroArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *TownHireHeroArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *TownHireHeroArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *TownHireHeroArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *TownHireHeroArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *TownHireHeroArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type townHireHeroResultsData struct {
	Assigned *bool `cbor:"0,keyasint,omitempty" json:"assigned,omitempty"`
}

type TownHireHeroResults struct {
	call rpc.Call
	data townHireHeroResultsData
}

func (v *TownHireHeroResults) SetAssigned(assigned bool) {
	v.data.Assigned = &assigned
}

func (v *TownHireHeroResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *TownHireHeroResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *TownHireHeroResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *TownHireHeroResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type TownGetHero struct {
	rpc.Call
	args    TownGetHeroArgs
	results TownGetHeroResults
}

func (t *TownGetHero) Args() *TownGetHeroArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *TownGetHero) Results() *TownGetHeroResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type TownHireHero struct {
	rpc.Call
	args    TownHireHeroArgs
	results TownHireHeroResults
}

func (t *TownHireHero) Args() *TownHireHeroArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *TownHireHero) Results() *TownHireHeroResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Town interface {
	GetHero(ctx context.Context, state *TownGetHero) error
	HireHero(ctx context.Context, state *TownHireHero) error
}

type reexportTown struct {
	client rpc.Client
}

func (_ reexportTown) GetHero(ctx context.Context, state *TownGetHero) error {
	panic("not implemented")
}

func (_ reexportTown) HireHero(ctx context.Context, state *TownHireHero) error {
	panic("not implemented")
}

func (t reexportTown) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptTown(t Town) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "getHero",
			InterfaceName: "Town",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetHero(ctx, &TownGetHero{Call: call})
			},
		},
		{
			Name:          "hireHero",
			InterfaceName: "Town",
			Index:         1,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.HireHero(ctx, &TownHireHero{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type TownClient struct {
	rpc.Client
}

func NewTownClient(client rpc.Client) *TownClient {
	return &TownClient{Client: client}
}

func (c TownClient) Export() Town {
	return reexportTown{client: c.Client}
}

type TownClientGetHeroResults struct {
	client rpc.Client
	data   townGetHeroResultsData
}

func (v *TownClientGetHeroResults) HasHero() bool {
	return v.data.Hero != nil
}

func (v *TownClientGetHeroResults) Hero() *Hero {
	return v.data.Hero
}

func (v *TownClientGetHeroResults) Empower() *EmpowerClient {
	return &EmpowerClient{
		Client: v.client.NewClient(v.data.Empower),
	}
}

func (v TownClient) GetHero(ctx context.Context, name string) (*TownClientGetHeroResults, error) {
	args := TownGetHeroArgs{}
	args.data.Name = &name

	var ret townGetHeroResultsData

	err := v.Call(ctx, "getHero", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &TownClientGetHeroResults{client: v.Client, data: ret}, nil
}

type TownClientHireHeroResults struct {
	client rpc.Client
	data   townHireHeroResultsData
}

func (v *TownClientHireHeroResults) HasAssigned() bool {
	return v.data.Assigned != nil
}

func (v *TownClientHireHeroResults) Assigned() bool {
	if v.data.Assigned == nil {
		return false
	}
	return *v.data.Assigned
}

func (v TownClient) HireHero(ctx context.Context, name string) (*TownClientHireHeroResults, error) {
	args := TownHireHeroArgs{}
	args.data.Name = &name

	var ret townHireHeroResultsData

	err := v.Call(ctx, "hireHero", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &TownClientHireHeroResults{client: v.Client, data: ret}, nil
}

type empowerIncreasePowerArgsData struct {
	Power *int32 `cbor:"0,keyasint,omitempty" json:"power,omitempty"`
}

type EmpowerIncreasePowerArgs struct {
	call rpc.Call
	data empowerIncreasePowerArgsData
}

func (v *EmpowerIncreasePowerArgs) HasPower() bool {
	return v.data.Power != nil
}

func (v *EmpowerIncreasePowerArgs) Power() int32 {
	if v.data.Power == nil {
		return 0
	}
	return *v.data.Power
}

func (v *EmpowerIncreasePowerArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EmpowerIncreasePowerArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EmpowerIncreasePowerArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EmpowerIncreasePowerArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type empowerIncreasePowerResultsData struct {
	Level *int32 `cbor:"0,keyasint,omitempty" json:"level,omitempty"`
}

type EmpowerIncreasePowerResults struct {
	call rpc.Call
	data empowerIncreasePowerResultsData
}

func (v *EmpowerIncreasePowerResults) SetLevel(level int32) {
	v.data.Level = &level
}

func (v *EmpowerIncreasePowerResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EmpowerIncreasePowerResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EmpowerIncreasePowerResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EmpowerIncreasePowerResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type EmpowerIncreasePower struct {
	rpc.Call
	args    EmpowerIncreasePowerArgs
	results EmpowerIncreasePowerResults
}

func (t *EmpowerIncreasePower) Args() *EmpowerIncreasePowerArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *EmpowerIncreasePower) Results() *EmpowerIncreasePowerResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Empower interface {
	IncreasePower(ctx context.Context, state *EmpowerIncreasePower) error
}

type reexportEmpower struct {
	client rpc.Client
}

func (_ reexportEmpower) IncreasePower(ctx context.Context, state *EmpowerIncreasePower) error {
	panic("not implemented")
}

func (t reexportEmpower) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptEmpower(t Empower) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "increasePower",
			InterfaceName: "Empower",
			Index:         0,
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.IncreasePower(ctx, &EmpowerIncreasePower{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type EmpowerClient struct {
	rpc.Client
}

func NewEmpowerClient(client rpc.Client) *EmpowerClient {
	return &EmpowerClient{Client: client}
}

func (c EmpowerClient) Export() Empower {
	return reexportEmpower{client: c.Client}
}

type EmpowerClientIncreasePowerResults struct {
	client rpc.Client
	data   empowerIncreasePowerResultsData
}

func (v *EmpowerClientIncreasePowerResults) HasLevel() bool {
	return v.data.Level != nil
}

func (v *EmpowerClientIncreasePowerResults) Level() int32 {
	if v.data.Level == nil {
		return 0
	}
	return *v.data.Level
}

func (v EmpowerClient) IncreasePower(ctx context.Context, power int32) (*EmpowerClientIncreasePowerResults, error) {
	args := EmpowerIncreasePowerArgs{}
	args.data.Power = &power

	var ret empowerIncreasePowerResultsData

	err := v.Call(ctx, "increasePower", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &EmpowerClientIncreasePowerResults{client: v.Client, data: ret}, nil
}
