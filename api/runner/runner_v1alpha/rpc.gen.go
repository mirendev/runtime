package runner_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

type inviteInfoData struct {
	Id              *string             `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Status          *string             `cbor:"1,keyasint,omitempty" json:"status,omitempty"`
	Labels          *[]string           `cbor:"2,keyasint,omitempty" json:"labels,omitempty"`
	ExpiresAt       *standard.Timestamp `cbor:"3,keyasint,omitempty" json:"expires_at,omitempty"`
	CreatedAt       *standard.Timestamp `cbor:"4,keyasint,omitempty" json:"created_at,omitempty"`
	ClaimedBy       *string             `cbor:"5,keyasint,omitempty" json:"claimed_by,omitempty"`
	ClaimedAt       *standard.Timestamp `cbor:"6,keyasint,omitempty" json:"claimed_at,omitempty"`
	Name            *string             `cbor:"7,keyasint,omitempty" json:"name,omitempty"`
	Reusable        *bool               `cbor:"8,keyasint,omitempty" json:"reusable,omitempty"`
	EnrollmentCount *int64              `cbor:"9,keyasint,omitempty" json:"enrollment_count,omitempty"`
}

type InviteInfo struct {
	data inviteInfoData
}

func (v *InviteInfo) HasId() bool {
	return v.data.Id != nil
}

func (v *InviteInfo) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *InviteInfo) SetId(id string) {
	v.data.Id = &id
}

func (v *InviteInfo) HasStatus() bool {
	return v.data.Status != nil
}

func (v *InviteInfo) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *InviteInfo) SetStatus(status string) {
	v.data.Status = &status
}

func (v *InviteInfo) HasLabels() bool {
	return v.data.Labels != nil
}

func (v *InviteInfo) Labels() []string {
	if v.data.Labels == nil {
		return nil
	}
	return *v.data.Labels
}

func (v *InviteInfo) SetLabels(labels []string) {
	x := slices.Clone(labels)
	v.data.Labels = &x
}

func (v *InviteInfo) HasExpiresAt() bool {
	return v.data.ExpiresAt != nil
}

func (v *InviteInfo) ExpiresAt() *standard.Timestamp {
	return v.data.ExpiresAt
}

func (v *InviteInfo) SetExpiresAt(expires_at *standard.Timestamp) {
	v.data.ExpiresAt = expires_at
}

func (v *InviteInfo) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *InviteInfo) CreatedAt() *standard.Timestamp {
	return v.data.CreatedAt
}

func (v *InviteInfo) SetCreatedAt(created_at *standard.Timestamp) {
	v.data.CreatedAt = created_at
}

func (v *InviteInfo) HasClaimedBy() bool {
	return v.data.ClaimedBy != nil
}

func (v *InviteInfo) ClaimedBy() string {
	if v.data.ClaimedBy == nil {
		return ""
	}
	return *v.data.ClaimedBy
}

func (v *InviteInfo) SetClaimedBy(claimed_by string) {
	v.data.ClaimedBy = &claimed_by
}

func (v *InviteInfo) HasClaimedAt() bool {
	return v.data.ClaimedAt != nil
}

func (v *InviteInfo) ClaimedAt() *standard.Timestamp {
	return v.data.ClaimedAt
}

func (v *InviteInfo) SetClaimedAt(claimed_at *standard.Timestamp) {
	v.data.ClaimedAt = claimed_at
}

func (v *InviteInfo) HasName() bool {
	return v.data.Name != nil
}

func (v *InviteInfo) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *InviteInfo) SetName(name string) {
	v.data.Name = &name
}

func (v *InviteInfo) HasReusable() bool {
	return v.data.Reusable != nil
}

func (v *InviteInfo) Reusable() bool {
	if v.data.Reusable == nil {
		return false
	}
	return *v.data.Reusable
}

func (v *InviteInfo) SetReusable(reusable bool) {
	v.data.Reusable = &reusable
}

func (v *InviteInfo) HasEnrollmentCount() bool {
	return v.data.EnrollmentCount != nil
}

func (v *InviteInfo) EnrollmentCount() int64 {
	if v.data.EnrollmentCount == nil {
		return 0
	}
	return *v.data.EnrollmentCount
}

func (v *InviteInfo) SetEnrollmentCount(enrollment_count int64) {
	v.data.EnrollmentCount = &enrollment_count
}

func (v *InviteInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *InviteInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *InviteInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *InviteInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerInfoData struct {
	Id           *string             `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	RunnerId     *string             `cbor:"1,keyasint,omitempty" json:"runner_id,omitempty"`
	Name         *string             `cbor:"2,keyasint,omitempty" json:"name,omitempty"`
	Status       *string             `cbor:"3,keyasint,omitempty" json:"status,omitempty"`
	Version      *string             `cbor:"4,keyasint,omitempty" json:"version,omitempty"`
	ApiAddress   *string             `cbor:"5,keyasint,omitempty" json:"api_address,omitempty"`
	Labels       *[]string           `cbor:"6,keyasint,omitempty" json:"labels,omitempty"`
	RegisteredAt *standard.Timestamp `cbor:"7,keyasint,omitempty" json:"registered_at,omitempty"`
	ShortId      *string             `cbor:"8,keyasint,omitempty" json:"short_id,omitempty"`
	Scheduling   *string             `cbor:"9,keyasint,omitempty" json:"scheduling,omitempty"`
}

type RunnerInfo struct {
	data runnerInfoData
}

func (v *RunnerInfo) HasId() bool {
	return v.data.Id != nil
}

func (v *RunnerInfo) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *RunnerInfo) SetId(id string) {
	v.data.Id = &id
}

func (v *RunnerInfo) HasRunnerId() bool {
	return v.data.RunnerId != nil
}

func (v *RunnerInfo) RunnerId() string {
	if v.data.RunnerId == nil {
		return ""
	}
	return *v.data.RunnerId
}

func (v *RunnerInfo) SetRunnerId(runner_id string) {
	v.data.RunnerId = &runner_id
}

func (v *RunnerInfo) HasName() bool {
	return v.data.Name != nil
}

func (v *RunnerInfo) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *RunnerInfo) SetName(name string) {
	v.data.Name = &name
}

func (v *RunnerInfo) HasStatus() bool {
	return v.data.Status != nil
}

func (v *RunnerInfo) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *RunnerInfo) SetStatus(status string) {
	v.data.Status = &status
}

func (v *RunnerInfo) HasVersion() bool {
	return v.data.Version != nil
}

func (v *RunnerInfo) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *RunnerInfo) SetVersion(version string) {
	v.data.Version = &version
}

func (v *RunnerInfo) HasApiAddress() bool {
	return v.data.ApiAddress != nil
}

func (v *RunnerInfo) ApiAddress() string {
	if v.data.ApiAddress == nil {
		return ""
	}
	return *v.data.ApiAddress
}

func (v *RunnerInfo) SetApiAddress(api_address string) {
	v.data.ApiAddress = &api_address
}

func (v *RunnerInfo) HasLabels() bool {
	return v.data.Labels != nil
}

func (v *RunnerInfo) Labels() []string {
	if v.data.Labels == nil {
		return nil
	}
	return *v.data.Labels
}

func (v *RunnerInfo) SetLabels(labels []string) {
	x := slices.Clone(labels)
	v.data.Labels = &x
}

func (v *RunnerInfo) HasRegisteredAt() bool {
	return v.data.RegisteredAt != nil
}

func (v *RunnerInfo) RegisteredAt() *standard.Timestamp {
	return v.data.RegisteredAt
}

func (v *RunnerInfo) SetRegisteredAt(registered_at *standard.Timestamp) {
	v.data.RegisteredAt = registered_at
}

func (v *RunnerInfo) HasShortId() bool {
	return v.data.ShortId != nil
}

func (v *RunnerInfo) ShortId() string {
	if v.data.ShortId == nil {
		return ""
	}
	return *v.data.ShortId
}

func (v *RunnerInfo) SetShortId(short_id string) {
	v.data.ShortId = &short_id
}

func (v *RunnerInfo) HasScheduling() bool {
	return v.data.Scheduling != nil
}

func (v *RunnerInfo) Scheduling() string {
	if v.data.Scheduling == nil {
		return ""
	}
	return *v.data.Scheduling
}

func (v *RunnerInfo) SetScheduling(scheduling string) {
	v.data.Scheduling = &scheduling
}

func (v *RunnerInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationCreateInviteArgsData struct {
	Labels          *[]string `cbor:"0,keyasint,omitempty" json:"labels,omitempty"`
	ExpiresInHours  *int32    `cbor:"1,keyasint,omitempty" json:"expires_in_hours,omitempty"`
	Name            *string   `cbor:"2,keyasint,omitempty" json:"name,omitempty"`
	Reusable        *bool     `cbor:"3,keyasint,omitempty" json:"reusable,omitempty"`
	TtlSeconds      *int64    `cbor:"4,keyasint,omitempty" json:"ttl_seconds,omitempty"`
	CoordinatorAddr *string   `cbor:"5,keyasint,omitempty" json:"coordinator_addr,omitempty"`
}

type RunnerRegistrationCreateInviteArgs struct {
	call rpc.Call
	data runnerRegistrationCreateInviteArgsData
}

func (v *RunnerRegistrationCreateInviteArgs) HasLabels() bool {
	return v.data.Labels != nil
}

func (v *RunnerRegistrationCreateInviteArgs) Labels() []string {
	if v.data.Labels == nil {
		return nil
	}
	return *v.data.Labels
}

func (v *RunnerRegistrationCreateInviteArgs) HasExpiresInHours() bool {
	return v.data.ExpiresInHours != nil
}

func (v *RunnerRegistrationCreateInviteArgs) ExpiresInHours() int32 {
	if v.data.ExpiresInHours == nil {
		return 0
	}
	return *v.data.ExpiresInHours
}

func (v *RunnerRegistrationCreateInviteArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *RunnerRegistrationCreateInviteArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *RunnerRegistrationCreateInviteArgs) HasReusable() bool {
	return v.data.Reusable != nil
}

func (v *RunnerRegistrationCreateInviteArgs) Reusable() bool {
	if v.data.Reusable == nil {
		return false
	}
	return *v.data.Reusable
}

func (v *RunnerRegistrationCreateInviteArgs) HasTtlSeconds() bool {
	return v.data.TtlSeconds != nil
}

func (v *RunnerRegistrationCreateInviteArgs) TtlSeconds() int64 {
	if v.data.TtlSeconds == nil {
		return 0
	}
	return *v.data.TtlSeconds
}

func (v *RunnerRegistrationCreateInviteArgs) HasCoordinatorAddr() bool {
	return v.data.CoordinatorAddr != nil
}

func (v *RunnerRegistrationCreateInviteArgs) CoordinatorAddr() string {
	if v.data.CoordinatorAddr == nil {
		return ""
	}
	return *v.data.CoordinatorAddr
}

func (v *RunnerRegistrationCreateInviteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationCreateInviteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationCreateInviteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationCreateInviteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationCreateInviteResultsData struct {
	Code      *string             `cbor:"0,keyasint,omitempty" json:"code,omitempty"`
	ExpiresAt *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"expires_at,omitempty"`
}

type RunnerRegistrationCreateInviteResults struct {
	call rpc.Call
	data runnerRegistrationCreateInviteResultsData
}

func (v *RunnerRegistrationCreateInviteResults) SetCode(code string) {
	v.data.Code = &code
}

func (v *RunnerRegistrationCreateInviteResults) SetExpiresAt(expires_at *standard.Timestamp) {
	v.data.ExpiresAt = expires_at
}

func (v *RunnerRegistrationCreateInviteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationCreateInviteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationCreateInviteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationCreateInviteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationJoinArgsData struct {
	Code       *string   `cbor:"0,keyasint,omitempty" json:"code,omitempty"`
	RunnerId   *string   `cbor:"1,keyasint,omitempty" json:"runner_id,omitempty"`
	ListenAddr *string   `cbor:"2,keyasint,omitempty" json:"listen_addr,omitempty"`
	Version    *string   `cbor:"3,keyasint,omitempty" json:"version,omitempty"`
	Labels     *[]string `cbor:"4,keyasint,omitempty" json:"labels,omitempty"`
	Name       *string   `cbor:"5,keyasint,omitempty" json:"name,omitempty"`
}

type RunnerRegistrationJoinArgs struct {
	call rpc.Call
	data runnerRegistrationJoinArgsData
}

func (v *RunnerRegistrationJoinArgs) HasCode() bool {
	return v.data.Code != nil
}

func (v *RunnerRegistrationJoinArgs) Code() string {
	if v.data.Code == nil {
		return ""
	}
	return *v.data.Code
}

func (v *RunnerRegistrationJoinArgs) HasRunnerId() bool {
	return v.data.RunnerId != nil
}

func (v *RunnerRegistrationJoinArgs) RunnerId() string {
	if v.data.RunnerId == nil {
		return ""
	}
	return *v.data.RunnerId
}

func (v *RunnerRegistrationJoinArgs) HasListenAddr() bool {
	return v.data.ListenAddr != nil
}

func (v *RunnerRegistrationJoinArgs) ListenAddr() string {
	if v.data.ListenAddr == nil {
		return ""
	}
	return *v.data.ListenAddr
}

func (v *RunnerRegistrationJoinArgs) HasVersion() bool {
	return v.data.Version != nil
}

func (v *RunnerRegistrationJoinArgs) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *RunnerRegistrationJoinArgs) HasLabels() bool {
	return v.data.Labels != nil
}

func (v *RunnerRegistrationJoinArgs) Labels() []string {
	if v.data.Labels == nil {
		return nil
	}
	return *v.data.Labels
}

func (v *RunnerRegistrationJoinArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *RunnerRegistrationJoinArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *RunnerRegistrationJoinArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationJoinArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationJoinArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationJoinArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationJoinResultsData struct {
	CertPem                *[]byte   `cbor:"0,keyasint,omitempty" json:"cert_pem,omitempty"`
	KeyPem                 *[]byte   `cbor:"1,keyasint,omitempty" json:"key_pem,omitempty"`
	CaPem                  *[]byte   `cbor:"2,keyasint,omitempty" json:"ca_pem,omitempty"`
	CoordinatorAddr        *string   `cbor:"3,keyasint,omitempty" json:"coordinator_addr,omitempty"`
	RunnerId               *string   `cbor:"4,keyasint,omitempty" json:"runner_id,omitempty"`
	EtcdEndpoints          *[]string `cbor:"5,keyasint,omitempty" json:"etcd_endpoints,omitempty"`
	EtcdPrefix             *string   `cbor:"6,keyasint,omitempty" json:"etcd_prefix,omitempty"`
	NetworkBackend         *string   `cbor:"7,keyasint,omitempty" json:"network_backend,omitempty"`
	VictoriametricsAddress *string   `cbor:"8,keyasint,omitempty" json:"victoriametrics_address,omitempty"`
	VictorialogsAddress    *string   `cbor:"9,keyasint,omitempty" json:"victorialogs_address,omitempty"`
	Error                  *string   `cbor:"10,keyasint,omitempty" json:"error,omitempty"`
}

type RunnerRegistrationJoinResults struct {
	call rpc.Call
	data runnerRegistrationJoinResultsData
}

func (v *RunnerRegistrationJoinResults) SetCertPem(cert_pem []byte) {
	x := slices.Clone(cert_pem)
	v.data.CertPem = &x
}

func (v *RunnerRegistrationJoinResults) SetKeyPem(key_pem []byte) {
	x := slices.Clone(key_pem)
	v.data.KeyPem = &x
}

func (v *RunnerRegistrationJoinResults) SetCaPem(ca_pem []byte) {
	x := slices.Clone(ca_pem)
	v.data.CaPem = &x
}

func (v *RunnerRegistrationJoinResults) SetCoordinatorAddr(coordinator_addr string) {
	v.data.CoordinatorAddr = &coordinator_addr
}

func (v *RunnerRegistrationJoinResults) SetRunnerId(runner_id string) {
	v.data.RunnerId = &runner_id
}

func (v *RunnerRegistrationJoinResults) SetEtcdEndpoints(etcd_endpoints []string) {
	x := slices.Clone(etcd_endpoints)
	v.data.EtcdEndpoints = &x
}

func (v *RunnerRegistrationJoinResults) SetEtcdPrefix(etcd_prefix string) {
	v.data.EtcdPrefix = &etcd_prefix
}

func (v *RunnerRegistrationJoinResults) SetNetworkBackend(network_backend string) {
	v.data.NetworkBackend = &network_backend
}

func (v *RunnerRegistrationJoinResults) SetVictoriametricsAddress(victoriametrics_address string) {
	v.data.VictoriametricsAddress = &victoriametrics_address
}

func (v *RunnerRegistrationJoinResults) SetVictorialogsAddress(victorialogs_address string) {
	v.data.VictorialogsAddress = &victorialogs_address
}

func (v *RunnerRegistrationJoinResults) SetError(error string) {
	v.data.Error = &error
}

func (v *RunnerRegistrationJoinResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationJoinResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationJoinResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationJoinResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationListInvitesArgsData struct{}

type RunnerRegistrationListInvitesArgs struct {
	call rpc.Call
	data runnerRegistrationListInvitesArgsData
}

func (v *RunnerRegistrationListInvitesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationListInvitesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationListInvitesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationListInvitesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationListInvitesResultsData struct {
	Invites *[]*InviteInfo `cbor:"0,keyasint,omitempty" json:"invites,omitempty"`
}

type RunnerRegistrationListInvitesResults struct {
	call rpc.Call
	data runnerRegistrationListInvitesResultsData
}

func (v *RunnerRegistrationListInvitesResults) SetInvites(invites []*InviteInfo) {
	x := slices.Clone(invites)
	v.data.Invites = &x
}

func (v *RunnerRegistrationListInvitesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationListInvitesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationListInvitesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationListInvitesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationRevokeInviteArgsData struct {
	InviteId *string `cbor:"0,keyasint,omitempty" json:"invite_id,omitempty"`
}

type RunnerRegistrationRevokeInviteArgs struct {
	call rpc.Call
	data runnerRegistrationRevokeInviteArgsData
}

func (v *RunnerRegistrationRevokeInviteArgs) HasInviteId() bool {
	return v.data.InviteId != nil
}

func (v *RunnerRegistrationRevokeInviteArgs) InviteId() string {
	if v.data.InviteId == nil {
		return ""
	}
	return *v.data.InviteId
}

func (v *RunnerRegistrationRevokeInviteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationRevokeInviteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationRevokeInviteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationRevokeInviteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationRevokeInviteResultsData struct {
	Success *bool   `cbor:"0,keyasint,omitempty" json:"success,omitempty"`
	Error   *string `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
}

type RunnerRegistrationRevokeInviteResults struct {
	call rpc.Call
	data runnerRegistrationRevokeInviteResultsData
}

func (v *RunnerRegistrationRevokeInviteResults) SetSuccess(success bool) {
	v.data.Success = &success
}

func (v *RunnerRegistrationRevokeInviteResults) SetError(error string) {
	v.data.Error = &error
}

func (v *RunnerRegistrationRevokeInviteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationRevokeInviteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationRevokeInviteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationRevokeInviteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationListRunnersArgsData struct{}

type RunnerRegistrationListRunnersArgs struct {
	call rpc.Call
	data runnerRegistrationListRunnersArgsData
}

func (v *RunnerRegistrationListRunnersArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationListRunnersArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationListRunnersArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationListRunnersArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationListRunnersResultsData struct {
	Runners *[]*RunnerInfo `cbor:"0,keyasint,omitempty" json:"runners,omitempty"`
}

type RunnerRegistrationListRunnersResults struct {
	call rpc.Call
	data runnerRegistrationListRunnersResultsData
}

func (v *RunnerRegistrationListRunnersResults) SetRunners(runners []*RunnerInfo) {
	x := slices.Clone(runners)
	v.data.Runners = &x
}

func (v *RunnerRegistrationListRunnersResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationListRunnersResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationListRunnersResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationListRunnersResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationRemoveRunnerArgsData struct {
	Query *string `cbor:"0,keyasint,omitempty" json:"query,omitempty"`
	Force *bool   `cbor:"1,keyasint,omitempty" json:"force,omitempty"`
}

type RunnerRegistrationRemoveRunnerArgs struct {
	call rpc.Call
	data runnerRegistrationRemoveRunnerArgsData
}

func (v *RunnerRegistrationRemoveRunnerArgs) HasQuery() bool {
	return v.data.Query != nil
}

func (v *RunnerRegistrationRemoveRunnerArgs) Query() string {
	if v.data.Query == nil {
		return ""
	}
	return *v.data.Query
}

func (v *RunnerRegistrationRemoveRunnerArgs) HasForce() bool {
	return v.data.Force != nil
}

func (v *RunnerRegistrationRemoveRunnerArgs) Force() bool {
	if v.data.Force == nil {
		return false
	}
	return *v.data.Force
}

func (v *RunnerRegistrationRemoveRunnerArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationRemoveRunnerArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationRemoveRunnerArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationRemoveRunnerArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationRemoveRunnerResultsData struct {
	Name             *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	RunnerId         *string `cbor:"1,keyasint,omitempty" json:"runner_id,omitempty"`
	RemovedResources *int32  `cbor:"2,keyasint,omitempty" json:"removed_resources,omitempty"`
	Error            *string `cbor:"3,keyasint,omitempty" json:"error,omitempty"`
}

type RunnerRegistrationRemoveRunnerResults struct {
	call rpc.Call
	data runnerRegistrationRemoveRunnerResultsData
}

func (v *RunnerRegistrationRemoveRunnerResults) SetName(name string) {
	v.data.Name = &name
}

func (v *RunnerRegistrationRemoveRunnerResults) SetRunnerId(runner_id string) {
	v.data.RunnerId = &runner_id
}

func (v *RunnerRegistrationRemoveRunnerResults) SetRemovedResources(removed_resources int32) {
	v.data.RemovedResources = &removed_resources
}

func (v *RunnerRegistrationRemoveRunnerResults) SetError(error string) {
	v.data.Error = &error
}

func (v *RunnerRegistrationRemoveRunnerResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationRemoveRunnerResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationRemoveRunnerResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationRemoveRunnerResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationWorkloadIssuerInfoArgsData struct{}

type RunnerRegistrationWorkloadIssuerInfoArgs struct {
	call rpc.Call
	data runnerRegistrationWorkloadIssuerInfoArgsData
}

func (v *RunnerRegistrationWorkloadIssuerInfoArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationWorkloadIssuerInfoArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationWorkloadIssuerInfoArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationWorkloadIssuerInfoArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationWorkloadIssuerInfoResultsData struct {
	Enabled   *bool   `cbor:"0,keyasint,omitempty" json:"enabled,omitempty"`
	IssuerUrl *string `cbor:"1,keyasint,omitempty" json:"issuer_url,omitempty"`
}

type RunnerRegistrationWorkloadIssuerInfoResults struct {
	call rpc.Call
	data runnerRegistrationWorkloadIssuerInfoResultsData
}

func (v *RunnerRegistrationWorkloadIssuerInfoResults) SetEnabled(enabled bool) {
	v.data.Enabled = &enabled
}

func (v *RunnerRegistrationWorkloadIssuerInfoResults) SetIssuerUrl(issuer_url string) {
	v.data.IssuerUrl = &issuer_url
}

func (v *RunnerRegistrationWorkloadIssuerInfoResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationWorkloadIssuerInfoResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationWorkloadIssuerInfoResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationWorkloadIssuerInfoResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationIssueWorkloadTokenArgsData struct {
	SandboxId  *string   `cbor:"0,keyasint,omitempty" json:"sandbox_id,omitempty"`
	Audience   *[]string `cbor:"1,keyasint,omitempty" json:"audience,omitempty"`
	TtlSeconds *int64    `cbor:"2,keyasint,omitempty" json:"ttl_seconds,omitempty"`
}

type RunnerRegistrationIssueWorkloadTokenArgs struct {
	call rpc.Call
	data runnerRegistrationIssueWorkloadTokenArgsData
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) HasSandboxId() bool {
	return v.data.SandboxId != nil
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) SandboxId() string {
	if v.data.SandboxId == nil {
		return ""
	}
	return *v.data.SandboxId
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) HasAudience() bool {
	return v.data.Audience != nil
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) Audience() []string {
	if v.data.Audience == nil {
		return nil
	}
	return *v.data.Audience
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) HasTtlSeconds() bool {
	return v.data.TtlSeconds != nil
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) TtlSeconds() int64 {
	if v.data.TtlSeconds == nil {
		return 0
	}
	return *v.data.TtlSeconds
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationIssueWorkloadTokenArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationIssueWorkloadTokenResultsData struct {
	Token *string `cbor:"0,keyasint,omitempty" json:"token,omitempty"`
	Error *string `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
}

type RunnerRegistrationIssueWorkloadTokenResults struct {
	call rpc.Call
	data runnerRegistrationIssueWorkloadTokenResultsData
}

func (v *RunnerRegistrationIssueWorkloadTokenResults) SetToken(token string) {
	v.data.Token = &token
}

func (v *RunnerRegistrationIssueWorkloadTokenResults) SetError(error string) {
	v.data.Error = &error
}

func (v *RunnerRegistrationIssueWorkloadTokenResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationIssueWorkloadTokenResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationIssueWorkloadTokenResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationIssueWorkloadTokenResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationRefreshCertificateArgsData struct {
	ListenAddr *string `cbor:"0,keyasint,omitempty" json:"listen_addr,omitempty"`
}

type RunnerRegistrationRefreshCertificateArgs struct {
	call rpc.Call
	data runnerRegistrationRefreshCertificateArgsData
}

func (v *RunnerRegistrationRefreshCertificateArgs) HasListenAddr() bool {
	return v.data.ListenAddr != nil
}

func (v *RunnerRegistrationRefreshCertificateArgs) ListenAddr() string {
	if v.data.ListenAddr == nil {
		return ""
	}
	return *v.data.ListenAddr
}

func (v *RunnerRegistrationRefreshCertificateArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationRefreshCertificateArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationRefreshCertificateArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationRefreshCertificateArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationRefreshCertificateResultsData struct {
	CertPem *[]byte `cbor:"0,keyasint,omitempty" json:"cert_pem,omitempty"`
	KeyPem  *[]byte `cbor:"1,keyasint,omitempty" json:"key_pem,omitempty"`
	CaPem   *[]byte `cbor:"2,keyasint,omitempty" json:"ca_pem,omitempty"`
	Error   *string `cbor:"3,keyasint,omitempty" json:"error,omitempty"`
}

type RunnerRegistrationRefreshCertificateResults struct {
	call rpc.Call
	data runnerRegistrationRefreshCertificateResultsData
}

func (v *RunnerRegistrationRefreshCertificateResults) SetCertPem(cert_pem []byte) {
	x := slices.Clone(cert_pem)
	v.data.CertPem = &x
}

func (v *RunnerRegistrationRefreshCertificateResults) SetKeyPem(key_pem []byte) {
	x := slices.Clone(key_pem)
	v.data.KeyPem = &x
}

func (v *RunnerRegistrationRefreshCertificateResults) SetCaPem(ca_pem []byte) {
	x := slices.Clone(ca_pem)
	v.data.CaPem = &x
}

func (v *RunnerRegistrationRefreshCertificateResults) SetError(error string) {
	v.data.Error = &error
}

func (v *RunnerRegistrationRefreshCertificateResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationRefreshCertificateResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationRefreshCertificateResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationRefreshCertificateResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationCordonRunnerArgsData struct {
	Query  *string `cbor:"0,keyasint,omitempty" json:"query,omitempty"`
	Reason *string `cbor:"1,keyasint,omitempty" json:"reason,omitempty"`
}

type RunnerRegistrationCordonRunnerArgs struct {
	call rpc.Call
	data runnerRegistrationCordonRunnerArgsData
}

func (v *RunnerRegistrationCordonRunnerArgs) HasQuery() bool {
	return v.data.Query != nil
}

func (v *RunnerRegistrationCordonRunnerArgs) Query() string {
	if v.data.Query == nil {
		return ""
	}
	return *v.data.Query
}

func (v *RunnerRegistrationCordonRunnerArgs) HasReason() bool {
	return v.data.Reason != nil
}

func (v *RunnerRegistrationCordonRunnerArgs) Reason() string {
	if v.data.Reason == nil {
		return ""
	}
	return *v.data.Reason
}

func (v *RunnerRegistrationCordonRunnerArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationCordonRunnerArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationCordonRunnerArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationCordonRunnerArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationCordonRunnerResultsData struct {
	Name     *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	RunnerId *string `cbor:"1,keyasint,omitempty" json:"runner_id,omitempty"`
	Error    *string `cbor:"2,keyasint,omitempty" json:"error,omitempty"`
}

type RunnerRegistrationCordonRunnerResults struct {
	call rpc.Call
	data runnerRegistrationCordonRunnerResultsData
}

func (v *RunnerRegistrationCordonRunnerResults) SetName(name string) {
	v.data.Name = &name
}

func (v *RunnerRegistrationCordonRunnerResults) SetRunnerId(runner_id string) {
	v.data.RunnerId = &runner_id
}

func (v *RunnerRegistrationCordonRunnerResults) SetError(error string) {
	v.data.Error = &error
}

func (v *RunnerRegistrationCordonRunnerResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationCordonRunnerResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationCordonRunnerResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationCordonRunnerResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationUncordonRunnerArgsData struct {
	Query *string `cbor:"0,keyasint,omitempty" json:"query,omitempty"`
}

type RunnerRegistrationUncordonRunnerArgs struct {
	call rpc.Call
	data runnerRegistrationUncordonRunnerArgsData
}

func (v *RunnerRegistrationUncordonRunnerArgs) HasQuery() bool {
	return v.data.Query != nil
}

func (v *RunnerRegistrationUncordonRunnerArgs) Query() string {
	if v.data.Query == nil {
		return ""
	}
	return *v.data.Query
}

func (v *RunnerRegistrationUncordonRunnerArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationUncordonRunnerArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationUncordonRunnerArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationUncordonRunnerArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationUncordonRunnerResultsData struct {
	Name     *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	RunnerId *string `cbor:"1,keyasint,omitempty" json:"runner_id,omitempty"`
	Error    *string `cbor:"2,keyasint,omitempty" json:"error,omitempty"`
}

type RunnerRegistrationUncordonRunnerResults struct {
	call rpc.Call
	data runnerRegistrationUncordonRunnerResultsData
}

func (v *RunnerRegistrationUncordonRunnerResults) SetName(name string) {
	v.data.Name = &name
}

func (v *RunnerRegistrationUncordonRunnerResults) SetRunnerId(runner_id string) {
	v.data.RunnerId = &runner_id
}

func (v *RunnerRegistrationUncordonRunnerResults) SetError(error string) {
	v.data.Error = &error
}

func (v *RunnerRegistrationUncordonRunnerResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationUncordonRunnerResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationUncordonRunnerResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationUncordonRunnerResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationDrainRunnerArgsData struct {
	Query          *string `cbor:"0,keyasint,omitempty" json:"query,omitempty"`
	Reason         *string `cbor:"1,keyasint,omitempty" json:"reason,omitempty"`
	TimeoutSeconds *int64  `cbor:"2,keyasint,omitempty" json:"timeout_seconds,omitempty"`
}

type RunnerRegistrationDrainRunnerArgs struct {
	call rpc.Call
	data runnerRegistrationDrainRunnerArgsData
}

func (v *RunnerRegistrationDrainRunnerArgs) HasQuery() bool {
	return v.data.Query != nil
}

func (v *RunnerRegistrationDrainRunnerArgs) Query() string {
	if v.data.Query == nil {
		return ""
	}
	return *v.data.Query
}

func (v *RunnerRegistrationDrainRunnerArgs) HasReason() bool {
	return v.data.Reason != nil
}

func (v *RunnerRegistrationDrainRunnerArgs) Reason() string {
	if v.data.Reason == nil {
		return ""
	}
	return *v.data.Reason
}

func (v *RunnerRegistrationDrainRunnerArgs) HasTimeoutSeconds() bool {
	return v.data.TimeoutSeconds != nil
}

func (v *RunnerRegistrationDrainRunnerArgs) TimeoutSeconds() int64 {
	if v.data.TimeoutSeconds == nil {
		return 0
	}
	return *v.data.TimeoutSeconds
}

func (v *RunnerRegistrationDrainRunnerArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationDrainRunnerArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationDrainRunnerArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationDrainRunnerArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationDrainRunnerResultsData struct {
	Name         *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	RunnerId     *string `cbor:"1,keyasint,omitempty" json:"runner_id,omitempty"`
	EvictedCount *int32  `cbor:"2,keyasint,omitempty" json:"evicted_count,omitempty"`
	TimedOut     *bool   `cbor:"3,keyasint,omitempty" json:"timed_out,omitempty"`
	Error        *string `cbor:"4,keyasint,omitempty" json:"error,omitempty"`
}

type RunnerRegistrationDrainRunnerResults struct {
	call rpc.Call
	data runnerRegistrationDrainRunnerResultsData
}

func (v *RunnerRegistrationDrainRunnerResults) SetName(name string) {
	v.data.Name = &name
}

func (v *RunnerRegistrationDrainRunnerResults) SetRunnerId(runner_id string) {
	v.data.RunnerId = &runner_id
}

func (v *RunnerRegistrationDrainRunnerResults) SetEvictedCount(evicted_count int32) {
	v.data.EvictedCount = &evicted_count
}

func (v *RunnerRegistrationDrainRunnerResults) SetTimedOut(timed_out bool) {
	v.data.TimedOut = &timed_out
}

func (v *RunnerRegistrationDrainRunnerResults) SetError(error string) {
	v.data.Error = &error
}

func (v *RunnerRegistrationDrainRunnerResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationDrainRunnerResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationDrainRunnerResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationDrainRunnerResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationIssueSystemWorkloadTokenArgsData struct {
	SystemWorkload *string   `cbor:"0,keyasint,omitempty" json:"system_workload,omitempty"`
	Audience       *[]string `cbor:"1,keyasint,omitempty" json:"audience,omitempty"`
	TtlSeconds     *int64    `cbor:"2,keyasint,omitempty" json:"ttl_seconds,omitempty"`
}

type RunnerRegistrationIssueSystemWorkloadTokenArgs struct {
	call rpc.Call
	data runnerRegistrationIssueSystemWorkloadTokenArgsData
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) HasSystemWorkload() bool {
	return v.data.SystemWorkload != nil
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) SystemWorkload() string {
	if v.data.SystemWorkload == nil {
		return ""
	}
	return *v.data.SystemWorkload
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) HasAudience() bool {
	return v.data.Audience != nil
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) Audience() []string {
	if v.data.Audience == nil {
		return nil
	}
	return *v.data.Audience
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) HasTtlSeconds() bool {
	return v.data.TtlSeconds != nil
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) TtlSeconds() int64 {
	if v.data.TtlSeconds == nil {
		return 0
	}
	return *v.data.TtlSeconds
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runnerRegistrationIssueSystemWorkloadTokenResultsData struct {
	Token *string `cbor:"0,keyasint,omitempty" json:"token,omitempty"`
	Error *string `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
}

type RunnerRegistrationIssueSystemWorkloadTokenResults struct {
	call rpc.Call
	data runnerRegistrationIssueSystemWorkloadTokenResultsData
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenResults) SetToken(token string) {
	v.data.Token = &token
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenResults) SetError(error string) {
	v.data.Error = &error
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunnerRegistrationIssueSystemWorkloadTokenResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type RunnerRegistrationCreateInvite struct {
	rpc.Call
	args    RunnerRegistrationCreateInviteArgs
	results RunnerRegistrationCreateInviteResults
}

func (t *RunnerRegistrationCreateInvite) Args() *RunnerRegistrationCreateInviteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationCreateInvite) Results() *RunnerRegistrationCreateInviteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationJoin struct {
	rpc.Call
	args    RunnerRegistrationJoinArgs
	results RunnerRegistrationJoinResults
}

func (t *RunnerRegistrationJoin) Args() *RunnerRegistrationJoinArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationJoin) Results() *RunnerRegistrationJoinResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationListInvites struct {
	rpc.Call
	args    RunnerRegistrationListInvitesArgs
	results RunnerRegistrationListInvitesResults
}

func (t *RunnerRegistrationListInvites) Args() *RunnerRegistrationListInvitesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationListInvites) Results() *RunnerRegistrationListInvitesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationRevokeInvite struct {
	rpc.Call
	args    RunnerRegistrationRevokeInviteArgs
	results RunnerRegistrationRevokeInviteResults
}

func (t *RunnerRegistrationRevokeInvite) Args() *RunnerRegistrationRevokeInviteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationRevokeInvite) Results() *RunnerRegistrationRevokeInviteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationListRunners struct {
	rpc.Call
	args    RunnerRegistrationListRunnersArgs
	results RunnerRegistrationListRunnersResults
}

func (t *RunnerRegistrationListRunners) Args() *RunnerRegistrationListRunnersArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationListRunners) Results() *RunnerRegistrationListRunnersResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationRemoveRunner struct {
	rpc.Call
	args    RunnerRegistrationRemoveRunnerArgs
	results RunnerRegistrationRemoveRunnerResults
}

func (t *RunnerRegistrationRemoveRunner) Args() *RunnerRegistrationRemoveRunnerArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationRemoveRunner) Results() *RunnerRegistrationRemoveRunnerResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationWorkloadIssuerInfo struct {
	rpc.Call
	args    RunnerRegistrationWorkloadIssuerInfoArgs
	results RunnerRegistrationWorkloadIssuerInfoResults
}

func (t *RunnerRegistrationWorkloadIssuerInfo) Args() *RunnerRegistrationWorkloadIssuerInfoArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationWorkloadIssuerInfo) Results() *RunnerRegistrationWorkloadIssuerInfoResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationIssueWorkloadToken struct {
	rpc.Call
	args    RunnerRegistrationIssueWorkloadTokenArgs
	results RunnerRegistrationIssueWorkloadTokenResults
}

func (t *RunnerRegistrationIssueWorkloadToken) Args() *RunnerRegistrationIssueWorkloadTokenArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationIssueWorkloadToken) Results() *RunnerRegistrationIssueWorkloadTokenResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationRefreshCertificate struct {
	rpc.Call
	args    RunnerRegistrationRefreshCertificateArgs
	results RunnerRegistrationRefreshCertificateResults
}

func (t *RunnerRegistrationRefreshCertificate) Args() *RunnerRegistrationRefreshCertificateArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationRefreshCertificate) Results() *RunnerRegistrationRefreshCertificateResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationCordonRunner struct {
	rpc.Call
	args    RunnerRegistrationCordonRunnerArgs
	results RunnerRegistrationCordonRunnerResults
}

func (t *RunnerRegistrationCordonRunner) Args() *RunnerRegistrationCordonRunnerArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationCordonRunner) Results() *RunnerRegistrationCordonRunnerResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationUncordonRunner struct {
	rpc.Call
	args    RunnerRegistrationUncordonRunnerArgs
	results RunnerRegistrationUncordonRunnerResults
}

func (t *RunnerRegistrationUncordonRunner) Args() *RunnerRegistrationUncordonRunnerArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationUncordonRunner) Results() *RunnerRegistrationUncordonRunnerResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationDrainRunner struct {
	rpc.Call
	args    RunnerRegistrationDrainRunnerArgs
	results RunnerRegistrationDrainRunnerResults
}

func (t *RunnerRegistrationDrainRunner) Args() *RunnerRegistrationDrainRunnerArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationDrainRunner) Results() *RunnerRegistrationDrainRunnerResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistrationIssueSystemWorkloadToken struct {
	rpc.Call
	args    RunnerRegistrationIssueSystemWorkloadTokenArgs
	results RunnerRegistrationIssueSystemWorkloadTokenResults
}

func (t *RunnerRegistrationIssueSystemWorkloadToken) Args() *RunnerRegistrationIssueSystemWorkloadTokenArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunnerRegistrationIssueSystemWorkloadToken) Results() *RunnerRegistrationIssueSystemWorkloadTokenResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunnerRegistration interface {
	CreateInvite(ctx context.Context, state *RunnerRegistrationCreateInvite) error
	Join(ctx context.Context, state *RunnerRegistrationJoin) error
	ListInvites(ctx context.Context, state *RunnerRegistrationListInvites) error
	RevokeInvite(ctx context.Context, state *RunnerRegistrationRevokeInvite) error
	ListRunners(ctx context.Context, state *RunnerRegistrationListRunners) error
	RemoveRunner(ctx context.Context, state *RunnerRegistrationRemoveRunner) error
	WorkloadIssuerInfo(ctx context.Context, state *RunnerRegistrationWorkloadIssuerInfo) error
	IssueWorkloadToken(ctx context.Context, state *RunnerRegistrationIssueWorkloadToken) error
	RefreshCertificate(ctx context.Context, state *RunnerRegistrationRefreshCertificate) error
	CordonRunner(ctx context.Context, state *RunnerRegistrationCordonRunner) error
	UncordonRunner(ctx context.Context, state *RunnerRegistrationUncordonRunner) error
	DrainRunner(ctx context.Context, state *RunnerRegistrationDrainRunner) error
	IssueSystemWorkloadToken(ctx context.Context, state *RunnerRegistrationIssueSystemWorkloadToken) error
}

type reexportRunnerRegistration struct {
	client rpc.Client
}

func (reexportRunnerRegistration) CreateInvite(ctx context.Context, state *RunnerRegistrationCreateInvite) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) Join(ctx context.Context, state *RunnerRegistrationJoin) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) ListInvites(ctx context.Context, state *RunnerRegistrationListInvites) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) RevokeInvite(ctx context.Context, state *RunnerRegistrationRevokeInvite) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) ListRunners(ctx context.Context, state *RunnerRegistrationListRunners) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) RemoveRunner(ctx context.Context, state *RunnerRegistrationRemoveRunner) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) WorkloadIssuerInfo(ctx context.Context, state *RunnerRegistrationWorkloadIssuerInfo) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) IssueWorkloadToken(ctx context.Context, state *RunnerRegistrationIssueWorkloadToken) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) RefreshCertificate(ctx context.Context, state *RunnerRegistrationRefreshCertificate) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) CordonRunner(ctx context.Context, state *RunnerRegistrationCordonRunner) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) UncordonRunner(ctx context.Context, state *RunnerRegistrationUncordonRunner) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) DrainRunner(ctx context.Context, state *RunnerRegistrationDrainRunner) error {
	panic("not implemented")
}

func (reexportRunnerRegistration) IssueSystemWorkloadToken(ctx context.Context, state *RunnerRegistrationIssueSystemWorkloadToken) error {
	panic("not implemented")
}

func (t reexportRunnerRegistration) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptRunnerRegistration(t RunnerRegistration) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "CreateInvite",
			InterfaceName: "RunnerRegistration",
			Index:         0,
			Public:        false,
			Params:        []string{"labels", "expires_in_hours", "name", "reusable", "ttl_seconds", "coordinator_addr"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.CreateInvite(ctx, &RunnerRegistrationCreateInvite{Call: call})
			},
		},
		{
			Name:          "Join",
			InterfaceName: "RunnerRegistration",
			Index:         1,
			Public:        true,
			Params:        []string{"code", "runner_id", "listen_addr", "version", "labels", "name"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Join(ctx, &RunnerRegistrationJoin{Call: call})
			},
		},
		{
			Name:          "ListInvites",
			InterfaceName: "RunnerRegistration",
			Index:         2,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListInvites(ctx, &RunnerRegistrationListInvites{Call: call})
			},
		},
		{
			Name:          "RevokeInvite",
			InterfaceName: "RunnerRegistration",
			Index:         3,
			Public:        false,
			Params:        []string{"invite_id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.RevokeInvite(ctx, &RunnerRegistrationRevokeInvite{Call: call})
			},
		},
		{
			Name:          "ListRunners",
			InterfaceName: "RunnerRegistration",
			Index:         4,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListRunners(ctx, &RunnerRegistrationListRunners{Call: call})
			},
		},
		{
			Name:          "RemoveRunner",
			InterfaceName: "RunnerRegistration",
			Index:         5,
			Public:        false,
			Params:        []string{"query", "force"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.RemoveRunner(ctx, &RunnerRegistrationRemoveRunner{Call: call})
			},
		},
		{
			Name:          "WorkloadIssuerInfo",
			InterfaceName: "RunnerRegistration",
			Index:         6,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.WorkloadIssuerInfo(ctx, &RunnerRegistrationWorkloadIssuerInfo{Call: call})
			},
		},
		{
			Name:          "IssueWorkloadToken",
			InterfaceName: "RunnerRegistration",
			Index:         7,
			Public:        false,
			Params:        []string{"sandbox_id", "audience", "ttl_seconds"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.IssueWorkloadToken(ctx, &RunnerRegistrationIssueWorkloadToken{Call: call})
			},
		},
		{
			Name:          "RefreshCertificate",
			InterfaceName: "RunnerRegistration",
			Index:         8,
			Public:        true,
			Params:        []string{"listen_addr"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.RefreshCertificate(ctx, &RunnerRegistrationRefreshCertificate{Call: call})
			},
		},
		{
			Name:          "CordonRunner",
			InterfaceName: "RunnerRegistration",
			Index:         9,
			Public:        false,
			Params:        []string{"query", "reason"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.CordonRunner(ctx, &RunnerRegistrationCordonRunner{Call: call})
			},
		},
		{
			Name:          "UncordonRunner",
			InterfaceName: "RunnerRegistration",
			Index:         10,
			Public:        false,
			Params:        []string{"query"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.UncordonRunner(ctx, &RunnerRegistrationUncordonRunner{Call: call})
			},
		},
		{
			Name:          "DrainRunner",
			InterfaceName: "RunnerRegistration",
			Index:         11,
			Public:        false,
			Params:        []string{"query", "reason", "timeout_seconds"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.DrainRunner(ctx, &RunnerRegistrationDrainRunner{Call: call})
			},
		},
		{
			Name:          "IssueSystemWorkloadToken",
			InterfaceName: "RunnerRegistration",
			Index:         12,
			Public:        false,
			Params:        []string{"system_workload", "audience", "ttl_seconds"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.IssueSystemWorkloadToken(ctx, &RunnerRegistrationIssueSystemWorkloadToken{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type RunnerRegistrationClient struct {
	rpc.Client
}

func NewRunnerRegistrationClient(client rpc.Client) *RunnerRegistrationClient {
	return &RunnerRegistrationClient{Client: client}
}

func (c RunnerRegistrationClient) Export() RunnerRegistration {
	return reexportRunnerRegistration{client: c.Client}
}

type RunnerRegistrationClientCreateInviteResults struct {
	client rpc.Client
	data   runnerRegistrationCreateInviteResultsData
}

func (v *RunnerRegistrationClientCreateInviteResults) HasCode() bool {
	return v.data.Code != nil
}

func (v *RunnerRegistrationClientCreateInviteResults) Code() string {
	if v.data.Code == nil {
		return ""
	}
	return *v.data.Code
}

func (v *RunnerRegistrationClientCreateInviteResults) HasExpiresAt() bool {
	return v.data.ExpiresAt != nil
}

func (v *RunnerRegistrationClientCreateInviteResults) ExpiresAt() *standard.Timestamp {
	return v.data.ExpiresAt
}

func (v RunnerRegistrationClient) CreateInvite(ctx context.Context, labels []string, expires_in_hours int32, name string, reusable bool, ttl_seconds int64, coordinator_addr string) (*RunnerRegistrationClientCreateInviteResults, error) {
	args := RunnerRegistrationCreateInviteArgs{}
	x := slices.Clone(labels)
	args.data.Labels = &x
	args.data.ExpiresInHours = &expires_in_hours
	args.data.Name = &name
	args.data.Reusable = &reusable
	args.data.TtlSeconds = &ttl_seconds
	args.data.CoordinatorAddr = &coordinator_addr

	var ret runnerRegistrationCreateInviteResultsData

	err := v.Call(ctx, "CreateInvite", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientCreateInviteResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientJoinResults struct {
	client rpc.Client
	data   runnerRegistrationJoinResultsData
}

func (v *RunnerRegistrationClientJoinResults) HasCertPem() bool {
	return v.data.CertPem != nil
}

func (v *RunnerRegistrationClientJoinResults) CertPem() []byte {
	if v.data.CertPem == nil {
		return nil
	}
	return *v.data.CertPem
}

func (v *RunnerRegistrationClientJoinResults) HasKeyPem() bool {
	return v.data.KeyPem != nil
}

func (v *RunnerRegistrationClientJoinResults) KeyPem() []byte {
	if v.data.KeyPem == nil {
		return nil
	}
	return *v.data.KeyPem
}

func (v *RunnerRegistrationClientJoinResults) HasCaPem() bool {
	return v.data.CaPem != nil
}

func (v *RunnerRegistrationClientJoinResults) CaPem() []byte {
	if v.data.CaPem == nil {
		return nil
	}
	return *v.data.CaPem
}

func (v *RunnerRegistrationClientJoinResults) HasCoordinatorAddr() bool {
	return v.data.CoordinatorAddr != nil
}

func (v *RunnerRegistrationClientJoinResults) CoordinatorAddr() string {
	if v.data.CoordinatorAddr == nil {
		return ""
	}
	return *v.data.CoordinatorAddr
}

func (v *RunnerRegistrationClientJoinResults) HasRunnerId() bool {
	return v.data.RunnerId != nil
}

func (v *RunnerRegistrationClientJoinResults) RunnerId() string {
	if v.data.RunnerId == nil {
		return ""
	}
	return *v.data.RunnerId
}

func (v *RunnerRegistrationClientJoinResults) HasEtcdEndpoints() bool {
	return v.data.EtcdEndpoints != nil
}

func (v *RunnerRegistrationClientJoinResults) EtcdEndpoints() []string {
	if v.data.EtcdEndpoints == nil {
		return nil
	}
	return *v.data.EtcdEndpoints
}

func (v *RunnerRegistrationClientJoinResults) HasEtcdPrefix() bool {
	return v.data.EtcdPrefix != nil
}

func (v *RunnerRegistrationClientJoinResults) EtcdPrefix() string {
	if v.data.EtcdPrefix == nil {
		return ""
	}
	return *v.data.EtcdPrefix
}

func (v *RunnerRegistrationClientJoinResults) HasNetworkBackend() bool {
	return v.data.NetworkBackend != nil
}

func (v *RunnerRegistrationClientJoinResults) NetworkBackend() string {
	if v.data.NetworkBackend == nil {
		return ""
	}
	return *v.data.NetworkBackend
}

func (v *RunnerRegistrationClientJoinResults) HasVictoriametricsAddress() bool {
	return v.data.VictoriametricsAddress != nil
}

func (v *RunnerRegistrationClientJoinResults) VictoriametricsAddress() string {
	if v.data.VictoriametricsAddress == nil {
		return ""
	}
	return *v.data.VictoriametricsAddress
}

func (v *RunnerRegistrationClientJoinResults) HasVictorialogsAddress() bool {
	return v.data.VictorialogsAddress != nil
}

func (v *RunnerRegistrationClientJoinResults) VictorialogsAddress() string {
	if v.data.VictorialogsAddress == nil {
		return ""
	}
	return *v.data.VictorialogsAddress
}

func (v *RunnerRegistrationClientJoinResults) HasError() bool {
	return v.data.Error != nil
}

func (v *RunnerRegistrationClientJoinResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v RunnerRegistrationClient) Join(ctx context.Context, code string, runner_id string, listen_addr string, version string, labels []string, name string) (*RunnerRegistrationClientJoinResults, error) {
	args := RunnerRegistrationJoinArgs{}
	args.data.Code = &code
	args.data.RunnerId = &runner_id
	args.data.ListenAddr = &listen_addr
	args.data.Version = &version
	x := slices.Clone(labels)
	args.data.Labels = &x
	args.data.Name = &name

	var ret runnerRegistrationJoinResultsData

	err := v.Call(ctx, "Join", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientJoinResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientListInvitesResults struct {
	client rpc.Client
	data   runnerRegistrationListInvitesResultsData
}

func (v *RunnerRegistrationClientListInvitesResults) HasInvites() bool {
	return v.data.Invites != nil
}

func (v *RunnerRegistrationClientListInvitesResults) Invites() []*InviteInfo {
	if v.data.Invites == nil {
		return nil
	}
	return *v.data.Invites
}

func (v RunnerRegistrationClient) ListInvites(ctx context.Context) (*RunnerRegistrationClientListInvitesResults, error) {
	args := RunnerRegistrationListInvitesArgs{}

	var ret runnerRegistrationListInvitesResultsData

	err := v.Call(ctx, "ListInvites", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientListInvitesResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientRevokeInviteResults struct {
	client rpc.Client
	data   runnerRegistrationRevokeInviteResultsData
}

func (v *RunnerRegistrationClientRevokeInviteResults) HasSuccess() bool {
	return v.data.Success != nil
}

func (v *RunnerRegistrationClientRevokeInviteResults) Success() bool {
	if v.data.Success == nil {
		return false
	}
	return *v.data.Success
}

func (v *RunnerRegistrationClientRevokeInviteResults) HasError() bool {
	return v.data.Error != nil
}

func (v *RunnerRegistrationClientRevokeInviteResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v RunnerRegistrationClient) RevokeInvite(ctx context.Context, invite_id string) (*RunnerRegistrationClientRevokeInviteResults, error) {
	args := RunnerRegistrationRevokeInviteArgs{}
	args.data.InviteId = &invite_id

	var ret runnerRegistrationRevokeInviteResultsData

	err := v.Call(ctx, "RevokeInvite", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientRevokeInviteResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientListRunnersResults struct {
	client rpc.Client
	data   runnerRegistrationListRunnersResultsData
}

func (v *RunnerRegistrationClientListRunnersResults) HasRunners() bool {
	return v.data.Runners != nil
}

func (v *RunnerRegistrationClientListRunnersResults) Runners() []*RunnerInfo {
	if v.data.Runners == nil {
		return nil
	}
	return *v.data.Runners
}

func (v RunnerRegistrationClient) ListRunners(ctx context.Context) (*RunnerRegistrationClientListRunnersResults, error) {
	args := RunnerRegistrationListRunnersArgs{}

	var ret runnerRegistrationListRunnersResultsData

	err := v.Call(ctx, "ListRunners", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientListRunnersResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientRemoveRunnerResults struct {
	client rpc.Client
	data   runnerRegistrationRemoveRunnerResultsData
}

func (v *RunnerRegistrationClientRemoveRunnerResults) HasName() bool {
	return v.data.Name != nil
}

func (v *RunnerRegistrationClientRemoveRunnerResults) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *RunnerRegistrationClientRemoveRunnerResults) HasRunnerId() bool {
	return v.data.RunnerId != nil
}

func (v *RunnerRegistrationClientRemoveRunnerResults) RunnerId() string {
	if v.data.RunnerId == nil {
		return ""
	}
	return *v.data.RunnerId
}

func (v *RunnerRegistrationClientRemoveRunnerResults) HasRemovedResources() bool {
	return v.data.RemovedResources != nil
}

func (v *RunnerRegistrationClientRemoveRunnerResults) RemovedResources() int32 {
	if v.data.RemovedResources == nil {
		return 0
	}
	return *v.data.RemovedResources
}

func (v *RunnerRegistrationClientRemoveRunnerResults) HasError() bool {
	return v.data.Error != nil
}

func (v *RunnerRegistrationClientRemoveRunnerResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v RunnerRegistrationClient) RemoveRunner(ctx context.Context, query string, force bool) (*RunnerRegistrationClientRemoveRunnerResults, error) {
	args := RunnerRegistrationRemoveRunnerArgs{}
	args.data.Query = &query
	args.data.Force = &force

	var ret runnerRegistrationRemoveRunnerResultsData

	err := v.Call(ctx, "RemoveRunner", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientRemoveRunnerResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientWorkloadIssuerInfoResults struct {
	client rpc.Client
	data   runnerRegistrationWorkloadIssuerInfoResultsData
}

func (v *RunnerRegistrationClientWorkloadIssuerInfoResults) HasEnabled() bool {
	return v.data.Enabled != nil
}

func (v *RunnerRegistrationClientWorkloadIssuerInfoResults) Enabled() bool {
	if v.data.Enabled == nil {
		return false
	}
	return *v.data.Enabled
}

func (v *RunnerRegistrationClientWorkloadIssuerInfoResults) HasIssuerUrl() bool {
	return v.data.IssuerUrl != nil
}

func (v *RunnerRegistrationClientWorkloadIssuerInfoResults) IssuerUrl() string {
	if v.data.IssuerUrl == nil {
		return ""
	}
	return *v.data.IssuerUrl
}

func (v RunnerRegistrationClient) WorkloadIssuerInfo(ctx context.Context) (*RunnerRegistrationClientWorkloadIssuerInfoResults, error) {
	args := RunnerRegistrationWorkloadIssuerInfoArgs{}

	var ret runnerRegistrationWorkloadIssuerInfoResultsData

	err := v.Call(ctx, "WorkloadIssuerInfo", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientWorkloadIssuerInfoResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientIssueWorkloadTokenResults struct {
	client rpc.Client
	data   runnerRegistrationIssueWorkloadTokenResultsData
}

func (v *RunnerRegistrationClientIssueWorkloadTokenResults) HasToken() bool {
	return v.data.Token != nil
}

func (v *RunnerRegistrationClientIssueWorkloadTokenResults) Token() string {
	if v.data.Token == nil {
		return ""
	}
	return *v.data.Token
}

func (v *RunnerRegistrationClientIssueWorkloadTokenResults) HasError() bool {
	return v.data.Error != nil
}

func (v *RunnerRegistrationClientIssueWorkloadTokenResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v RunnerRegistrationClient) IssueWorkloadToken(ctx context.Context, sandbox_id string, audience []string, ttl_seconds int64) (*RunnerRegistrationClientIssueWorkloadTokenResults, error) {
	args := RunnerRegistrationIssueWorkloadTokenArgs{}
	args.data.SandboxId = &sandbox_id
	x := slices.Clone(audience)
	args.data.Audience = &x
	args.data.TtlSeconds = &ttl_seconds

	var ret runnerRegistrationIssueWorkloadTokenResultsData

	err := v.Call(ctx, "IssueWorkloadToken", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientIssueWorkloadTokenResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientRefreshCertificateResults struct {
	client rpc.Client
	data   runnerRegistrationRefreshCertificateResultsData
}

func (v *RunnerRegistrationClientRefreshCertificateResults) HasCertPem() bool {
	return v.data.CertPem != nil
}

func (v *RunnerRegistrationClientRefreshCertificateResults) CertPem() []byte {
	if v.data.CertPem == nil {
		return nil
	}
	return *v.data.CertPem
}

func (v *RunnerRegistrationClientRefreshCertificateResults) HasKeyPem() bool {
	return v.data.KeyPem != nil
}

func (v *RunnerRegistrationClientRefreshCertificateResults) KeyPem() []byte {
	if v.data.KeyPem == nil {
		return nil
	}
	return *v.data.KeyPem
}

func (v *RunnerRegistrationClientRefreshCertificateResults) HasCaPem() bool {
	return v.data.CaPem != nil
}

func (v *RunnerRegistrationClientRefreshCertificateResults) CaPem() []byte {
	if v.data.CaPem == nil {
		return nil
	}
	return *v.data.CaPem
}

func (v *RunnerRegistrationClientRefreshCertificateResults) HasError() bool {
	return v.data.Error != nil
}

func (v *RunnerRegistrationClientRefreshCertificateResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v RunnerRegistrationClient) RefreshCertificate(ctx context.Context, listen_addr string) (*RunnerRegistrationClientRefreshCertificateResults, error) {
	args := RunnerRegistrationRefreshCertificateArgs{}
	args.data.ListenAddr = &listen_addr

	var ret runnerRegistrationRefreshCertificateResultsData

	err := v.Call(ctx, "RefreshCertificate", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientRefreshCertificateResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientCordonRunnerResults struct {
	client rpc.Client
	data   runnerRegistrationCordonRunnerResultsData
}

func (v *RunnerRegistrationClientCordonRunnerResults) HasName() bool {
	return v.data.Name != nil
}

func (v *RunnerRegistrationClientCordonRunnerResults) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *RunnerRegistrationClientCordonRunnerResults) HasRunnerId() bool {
	return v.data.RunnerId != nil
}

func (v *RunnerRegistrationClientCordonRunnerResults) RunnerId() string {
	if v.data.RunnerId == nil {
		return ""
	}
	return *v.data.RunnerId
}

func (v *RunnerRegistrationClientCordonRunnerResults) HasError() bool {
	return v.data.Error != nil
}

func (v *RunnerRegistrationClientCordonRunnerResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v RunnerRegistrationClient) CordonRunner(ctx context.Context, query string, reason string) (*RunnerRegistrationClientCordonRunnerResults, error) {
	args := RunnerRegistrationCordonRunnerArgs{}
	args.data.Query = &query
	args.data.Reason = &reason

	var ret runnerRegistrationCordonRunnerResultsData

	err := v.Call(ctx, "CordonRunner", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientCordonRunnerResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientUncordonRunnerResults struct {
	client rpc.Client
	data   runnerRegistrationUncordonRunnerResultsData
}

func (v *RunnerRegistrationClientUncordonRunnerResults) HasName() bool {
	return v.data.Name != nil
}

func (v *RunnerRegistrationClientUncordonRunnerResults) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *RunnerRegistrationClientUncordonRunnerResults) HasRunnerId() bool {
	return v.data.RunnerId != nil
}

func (v *RunnerRegistrationClientUncordonRunnerResults) RunnerId() string {
	if v.data.RunnerId == nil {
		return ""
	}
	return *v.data.RunnerId
}

func (v *RunnerRegistrationClientUncordonRunnerResults) HasError() bool {
	return v.data.Error != nil
}

func (v *RunnerRegistrationClientUncordonRunnerResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v RunnerRegistrationClient) UncordonRunner(ctx context.Context, query string) (*RunnerRegistrationClientUncordonRunnerResults, error) {
	args := RunnerRegistrationUncordonRunnerArgs{}
	args.data.Query = &query

	var ret runnerRegistrationUncordonRunnerResultsData

	err := v.Call(ctx, "UncordonRunner", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientUncordonRunnerResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientDrainRunnerResults struct {
	client rpc.Client
	data   runnerRegistrationDrainRunnerResultsData
}

func (v *RunnerRegistrationClientDrainRunnerResults) HasName() bool {
	return v.data.Name != nil
}

func (v *RunnerRegistrationClientDrainRunnerResults) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *RunnerRegistrationClientDrainRunnerResults) HasRunnerId() bool {
	return v.data.RunnerId != nil
}

func (v *RunnerRegistrationClientDrainRunnerResults) RunnerId() string {
	if v.data.RunnerId == nil {
		return ""
	}
	return *v.data.RunnerId
}

func (v *RunnerRegistrationClientDrainRunnerResults) HasEvictedCount() bool {
	return v.data.EvictedCount != nil
}

func (v *RunnerRegistrationClientDrainRunnerResults) EvictedCount() int32 {
	if v.data.EvictedCount == nil {
		return 0
	}
	return *v.data.EvictedCount
}

func (v *RunnerRegistrationClientDrainRunnerResults) HasTimedOut() bool {
	return v.data.TimedOut != nil
}

func (v *RunnerRegistrationClientDrainRunnerResults) TimedOut() bool {
	if v.data.TimedOut == nil {
		return false
	}
	return *v.data.TimedOut
}

func (v *RunnerRegistrationClientDrainRunnerResults) HasError() bool {
	return v.data.Error != nil
}

func (v *RunnerRegistrationClientDrainRunnerResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v RunnerRegistrationClient) DrainRunner(ctx context.Context, query string, reason string, timeout_seconds int64) (*RunnerRegistrationClientDrainRunnerResults, error) {
	args := RunnerRegistrationDrainRunnerArgs{}
	args.data.Query = &query
	args.data.Reason = &reason
	args.data.TimeoutSeconds = &timeout_seconds

	var ret runnerRegistrationDrainRunnerResultsData

	err := v.Call(ctx, "DrainRunner", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientDrainRunnerResults{client: v.Client, data: ret}, nil
}

type RunnerRegistrationClientIssueSystemWorkloadTokenResults struct {
	client rpc.Client
	data   runnerRegistrationIssueSystemWorkloadTokenResultsData
}

func (v *RunnerRegistrationClientIssueSystemWorkloadTokenResults) HasToken() bool {
	return v.data.Token != nil
}

func (v *RunnerRegistrationClientIssueSystemWorkloadTokenResults) Token() string {
	if v.data.Token == nil {
		return ""
	}
	return *v.data.Token
}

func (v *RunnerRegistrationClientIssueSystemWorkloadTokenResults) HasError() bool {
	return v.data.Error != nil
}

func (v *RunnerRegistrationClientIssueSystemWorkloadTokenResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v RunnerRegistrationClient) IssueSystemWorkloadToken(ctx context.Context, system_workload string, audience []string, ttl_seconds int64) (*RunnerRegistrationClientIssueSystemWorkloadTokenResults, error) {
	args := RunnerRegistrationIssueSystemWorkloadTokenArgs{}
	args.data.SystemWorkload = &system_workload
	x := slices.Clone(audience)
	args.data.Audience = &x
	args.data.TtlSeconds = &ttl_seconds

	var ret runnerRegistrationIssueSystemWorkloadTokenResultsData

	err := v.Call(ctx, "IssueSystemWorkloadToken", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunnerRegistrationClientIssueSystemWorkloadTokenResults{client: v.Client, data: ret}, nil
}
