package certrevocation_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type revocationData struct {
	Serial    *string `cbor:"0,keyasint,omitempty" json:"serial,omitempty"`
	RevokedAt *string `cbor:"1,keyasint,omitempty" json:"revoked_at,omitempty"`
	RevokedBy *string `cbor:"2,keyasint,omitempty" json:"revoked_by,omitempty"`
	Reason    *string `cbor:"3,keyasint,omitempty" json:"reason,omitempty"`
}

type Revocation struct {
	data revocationData
}

func (v *Revocation) HasSerial() bool {
	return v.data.Serial != nil
}

func (v *Revocation) Serial() string {
	if v.data.Serial == nil {
		return ""
	}
	return *v.data.Serial
}

func (v *Revocation) SetSerial(serial string) {
	v.data.Serial = &serial
}

func (v *Revocation) HasRevokedAt() bool {
	return v.data.RevokedAt != nil
}

func (v *Revocation) RevokedAt() string {
	if v.data.RevokedAt == nil {
		return ""
	}
	return *v.data.RevokedAt
}

func (v *Revocation) SetRevokedAt(revoked_at string) {
	v.data.RevokedAt = &revoked_at
}

func (v *Revocation) HasRevokedBy() bool {
	return v.data.RevokedBy != nil
}

func (v *Revocation) RevokedBy() string {
	if v.data.RevokedBy == nil {
		return ""
	}
	return *v.data.RevokedBy
}

func (v *Revocation) SetRevokedBy(revoked_by string) {
	v.data.RevokedBy = &revoked_by
}

func (v *Revocation) HasReason() bool {
	return v.data.Reason != nil
}

func (v *Revocation) Reason() string {
	if v.data.Reason == nil {
		return ""
	}
	return *v.data.Reason
}

func (v *Revocation) SetReason(reason string) {
	v.data.Reason = &reason
}

func (v *Revocation) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Revocation) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Revocation) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Revocation) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type certRevocationsRevokeArgsData struct {
	Serial *string `cbor:"0,keyasint,omitempty" json:"serial,omitempty"`
	Reason *string `cbor:"1,keyasint,omitempty" json:"reason,omitempty"`
}

type CertRevocationsRevokeArgs struct {
	call rpc.Call
	data certRevocationsRevokeArgsData
}

func (v *CertRevocationsRevokeArgs) HasSerial() bool {
	return v.data.Serial != nil
}

func (v *CertRevocationsRevokeArgs) Serial() string {
	if v.data.Serial == nil {
		return ""
	}
	return *v.data.Serial
}

func (v *CertRevocationsRevokeArgs) HasReason() bool {
	return v.data.Reason != nil
}

func (v *CertRevocationsRevokeArgs) Reason() string {
	if v.data.Reason == nil {
		return ""
	}
	return *v.data.Reason
}

func (v *CertRevocationsRevokeArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CertRevocationsRevokeArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CertRevocationsRevokeArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CertRevocationsRevokeArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type certRevocationsRevokeResultsData struct {
	Revocation     *Revocation `cbor:"0,keyasint,omitempty" json:"revocation,omitempty"`
	AlreadyRevoked *bool       `cbor:"1,keyasint,omitempty" json:"already_revoked,omitempty"`
}

type CertRevocationsRevokeResults struct {
	call rpc.Call
	data certRevocationsRevokeResultsData
}

func (v *CertRevocationsRevokeResults) SetRevocation(revocation *Revocation) {
	v.data.Revocation = revocation
}

func (v *CertRevocationsRevokeResults) SetAlreadyRevoked(already_revoked bool) {
	v.data.AlreadyRevoked = &already_revoked
}

func (v *CertRevocationsRevokeResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CertRevocationsRevokeResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CertRevocationsRevokeResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CertRevocationsRevokeResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type certRevocationsListArgsData struct{}

type CertRevocationsListArgs struct {
	call rpc.Call
	data certRevocationsListArgsData
}

func (v *CertRevocationsListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CertRevocationsListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CertRevocationsListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CertRevocationsListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type certRevocationsListResultsData struct {
	Revocations *[]*Revocation `cbor:"0,keyasint,omitempty" json:"revocations,omitempty"`
}

type CertRevocationsListResults struct {
	call rpc.Call
	data certRevocationsListResultsData
}

func (v *CertRevocationsListResults) SetRevocations(revocations []*Revocation) {
	x := slices.Clone(revocations)
	v.data.Revocations = &x
}

func (v *CertRevocationsListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CertRevocationsListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CertRevocationsListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CertRevocationsListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type CertRevocationsRevoke struct {
	rpc.Call
	args    CertRevocationsRevokeArgs
	results CertRevocationsRevokeResults
}

func (t *CertRevocationsRevoke) Args() *CertRevocationsRevokeArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CertRevocationsRevoke) Results() *CertRevocationsRevokeResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CertRevocationsList struct {
	rpc.Call
	args    CertRevocationsListArgs
	results CertRevocationsListResults
}

func (t *CertRevocationsList) Args() *CertRevocationsListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CertRevocationsList) Results() *CertRevocationsListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CertRevocations interface {
	Revoke(ctx context.Context, state *CertRevocationsRevoke) error
	List(ctx context.Context, state *CertRevocationsList) error
}

type reexportCertRevocations struct {
	client rpc.Client
}

func (reexportCertRevocations) Revoke(ctx context.Context, state *CertRevocationsRevoke) error {
	panic("not implemented")
}

func (reexportCertRevocations) List(ctx context.Context, state *CertRevocationsList) error {
	panic("not implemented")
}

func (t reexportCertRevocations) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptCertRevocations(t CertRevocations) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "Revoke",
			InterfaceName: "CertRevocations",
			Index:         0,
			Public:        false,
			Params:        []string{"serial", "reason"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Revoke(ctx, &CertRevocationsRevoke{Call: call})
			},
		},
		{
			Name:          "List",
			InterfaceName: "CertRevocations",
			Index:         1,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &CertRevocationsList{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type CertRevocationsClient struct {
	rpc.Client
}

func NewCertRevocationsClient(client rpc.Client) *CertRevocationsClient {
	return &CertRevocationsClient{Client: client}
}

func (c CertRevocationsClient) Export() CertRevocations {
	return reexportCertRevocations{client: c.Client}
}

type CertRevocationsClientRevokeResults struct {
	client rpc.Client
	data   certRevocationsRevokeResultsData
}

func (v *CertRevocationsClientRevokeResults) HasRevocation() bool {
	return v.data.Revocation != nil
}

func (v *CertRevocationsClientRevokeResults) Revocation() *Revocation {
	return v.data.Revocation
}

func (v *CertRevocationsClientRevokeResults) HasAlreadyRevoked() bool {
	return v.data.AlreadyRevoked != nil
}

func (v *CertRevocationsClientRevokeResults) AlreadyRevoked() bool {
	if v.data.AlreadyRevoked == nil {
		return false
	}
	return *v.data.AlreadyRevoked
}

func (v CertRevocationsClient) Revoke(ctx context.Context, serial string, reason string) (*CertRevocationsClientRevokeResults, error) {
	args := CertRevocationsRevokeArgs{}
	args.data.Serial = &serial
	args.data.Reason = &reason

	var ret certRevocationsRevokeResultsData

	err := v.Call(ctx, "Revoke", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CertRevocationsClientRevokeResults{client: v.Client, data: ret}, nil
}

type CertRevocationsClientListResults struct {
	client rpc.Client
	data   certRevocationsListResultsData
}

func (v *CertRevocationsClientListResults) HasRevocations() bool {
	return v.data.Revocations != nil
}

func (v *CertRevocationsClientListResults) Revocations() []*Revocation {
	if v.data.Revocations == nil {
		return nil
	}
	return *v.data.Revocations
}

func (v CertRevocationsClient) List(ctx context.Context) (*CertRevocationsClientListResults, error) {
	args := CertRevocationsListArgs{}

	var ret certRevocationsListResultsData

	err := v.Call(ctx, "List", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CertRevocationsClientListResults{client: v.Client, data: ret}, nil
}
