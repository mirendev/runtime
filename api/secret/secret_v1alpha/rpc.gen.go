package secret_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
)

type secretVersionInfoData struct {
	Version   *string `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	State     *string `cbor:"1,keyasint,omitempty" json:"state,omitempty"`
	CreatedAt *int64  `cbor:"2,keyasint,omitempty" json:"created_at,omitempty"`
	Current   *bool   `cbor:"3,keyasint,omitempty" json:"current,omitempty"`
}

type SecretVersionInfo struct {
	data secretVersionInfoData
}

func (v *SecretVersionInfo) HasVersion() bool {
	return v.data.Version != nil
}

func (v *SecretVersionInfo) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *SecretVersionInfo) SetVersion(version string) {
	v.data.Version = &version
}

func (v *SecretVersionInfo) HasState() bool {
	return v.data.State != nil
}

func (v *SecretVersionInfo) State() string {
	if v.data.State == nil {
		return ""
	}
	return *v.data.State
}

func (v *SecretVersionInfo) SetState(state string) {
	v.data.State = &state
}

func (v *SecretVersionInfo) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *SecretVersionInfo) CreatedAt() int64 {
	if v.data.CreatedAt == nil {
		return 0
	}
	return *v.data.CreatedAt
}

func (v *SecretVersionInfo) SetCreatedAt(created_at int64) {
	v.data.CreatedAt = &created_at
}

func (v *SecretVersionInfo) HasCurrent() bool {
	return v.data.Current != nil
}

func (v *SecretVersionInfo) Current() bool {
	if v.data.Current == nil {
		return false
	}
	return *v.data.Current
}

func (v *SecretVersionInfo) SetCurrent(current bool) {
	v.data.Current = &current
}

func (v *SecretVersionInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretVersionInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretVersionInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretVersionInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type keyInfoData struct {
	Id        *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Current   *bool   `cbor:"1,keyasint,omitempty" json:"current,omitempty"`
	CreatedAt *int64  `cbor:"2,keyasint,omitempty" json:"created_at,omitempty"`
	Versions  *int64  `cbor:"3,keyasint,omitempty" json:"versions,omitempty"`
}

type KeyInfo struct {
	data keyInfoData
}

func (v *KeyInfo) HasId() bool {
	return v.data.Id != nil
}

func (v *KeyInfo) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *KeyInfo) SetId(id string) {
	v.data.Id = &id
}

func (v *KeyInfo) HasCurrent() bool {
	return v.data.Current != nil
}

func (v *KeyInfo) Current() bool {
	if v.data.Current == nil {
		return false
	}
	return *v.data.Current
}

func (v *KeyInfo) SetCurrent(current bool) {
	v.data.Current = &current
}

func (v *KeyInfo) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *KeyInfo) CreatedAt() int64 {
	if v.data.CreatedAt == nil {
		return 0
	}
	return *v.data.CreatedAt
}

func (v *KeyInfo) SetCreatedAt(created_at int64) {
	v.data.CreatedAt = &created_at
}

func (v *KeyInfo) HasVersions() bool {
	return v.data.Versions != nil
}

func (v *KeyInfo) Versions() int64 {
	if v.data.Versions == nil {
		return 0
	}
	return *v.data.Versions
}

func (v *KeyInfo) SetVersions(versions int64) {
	v.data.Versions = &versions
}

func (v *KeyInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *KeyInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *KeyInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *KeyInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretInfoData struct {
	Path           *string               `cbor:"0,keyasint,omitempty" json:"path,omitempty"`
	Backend        *string               `cbor:"1,keyasint,omitempty" json:"backend,omitempty"`
	CurrentVersion *string               `cbor:"2,keyasint,omitempty" json:"current_version,omitempty"`
	Versions       *[]*SecretVersionInfo `cbor:"3,keyasint,omitempty" json:"versions,omitempty"`
}

type SecretInfo struct {
	data secretInfoData
}

func (v *SecretInfo) HasPath() bool {
	return v.data.Path != nil
}

func (v *SecretInfo) Path() string {
	if v.data.Path == nil {
		return ""
	}
	return *v.data.Path
}

func (v *SecretInfo) SetPath(path string) {
	v.data.Path = &path
}

func (v *SecretInfo) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretInfo) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretInfo) SetBackend(backend string) {
	v.data.Backend = &backend
}

func (v *SecretInfo) HasCurrentVersion() bool {
	return v.data.CurrentVersion != nil
}

func (v *SecretInfo) CurrentVersion() string {
	if v.data.CurrentVersion == nil {
		return ""
	}
	return *v.data.CurrentVersion
}

func (v *SecretInfo) SetCurrentVersion(current_version string) {
	v.data.CurrentVersion = &current_version
}

func (v *SecretInfo) HasVersions() bool {
	return v.data.Versions != nil
}

func (v *SecretInfo) Versions() []*SecretVersionInfo {
	if v.data.Versions == nil {
		return nil
	}
	return *v.data.Versions
}

func (v *SecretInfo) SetVersions(versions []*SecretVersionInfo) {
	x := slices.Clone(versions)
	v.data.Versions = &x
}

func (v *SecretInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsSetArgsData struct {
	Backend *string `cbor:"0,keyasint,omitempty" json:"backend,omitempty"`
	Path    *string `cbor:"1,keyasint,omitempty" json:"path,omitempty"`
	Value   *[]byte `cbor:"2,keyasint,omitempty" json:"value,omitempty"`
}

type SecretsSetArgs struct {
	call rpc.Call
	data secretsSetArgsData
}

func (v *SecretsSetArgs) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretsSetArgs) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretsSetArgs) HasPath() bool {
	return v.data.Path != nil
}

func (v *SecretsSetArgs) Path() string {
	if v.data.Path == nil {
		return ""
	}
	return *v.data.Path
}

func (v *SecretsSetArgs) HasValue() bool {
	return v.data.Value != nil
}

func (v *SecretsSetArgs) Value() []byte {
	if v.data.Value == nil {
		return nil
	}
	return *v.data.Value
}

func (v *SecretsSetArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsSetArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsSetArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsSetArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsSetResultsData struct {
	Version   *string `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	Unchanged *bool   `cbor:"1,keyasint,omitempty" json:"unchanged,omitempty"`
}

type SecretsSetResults struct {
	call rpc.Call
	data secretsSetResultsData
}

func (v *SecretsSetResults) SetVersion(version string) {
	v.data.Version = &version
}

func (v *SecretsSetResults) SetUnchanged(unchanged bool) {
	v.data.Unchanged = &unchanged
}

func (v *SecretsSetResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsSetResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsSetResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsSetResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsListArgsData struct {
	Backend *string `cbor:"0,keyasint,omitempty" json:"backend,omitempty"`
}

type SecretsListArgs struct {
	call rpc.Call
	data secretsListArgsData
}

func (v *SecretsListArgs) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretsListArgs) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretsListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsListResultsData struct {
	Secrets *[]*SecretInfo `cbor:"0,keyasint,omitempty" json:"secrets,omitempty"`
}

type SecretsListResults struct {
	call rpc.Call
	data secretsListResultsData
}

func (v *SecretsListResults) SetSecrets(secrets []*SecretInfo) {
	x := slices.Clone(secrets)
	v.data.Secrets = &x
}

func (v *SecretsListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsListVersionsArgsData struct {
	Backend *string `cbor:"0,keyasint,omitempty" json:"backend,omitempty"`
	Path    *string `cbor:"1,keyasint,omitempty" json:"path,omitempty"`
}

type SecretsListVersionsArgs struct {
	call rpc.Call
	data secretsListVersionsArgsData
}

func (v *SecretsListVersionsArgs) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretsListVersionsArgs) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretsListVersionsArgs) HasPath() bool {
	return v.data.Path != nil
}

func (v *SecretsListVersionsArgs) Path() string {
	if v.data.Path == nil {
		return ""
	}
	return *v.data.Path
}

func (v *SecretsListVersionsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsListVersionsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsListVersionsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsListVersionsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsListVersionsResultsData struct {
	Secret *SecretInfo `cbor:"0,keyasint,omitempty" json:"secret,omitempty"`
}

type SecretsListVersionsResults struct {
	call rpc.Call
	data secretsListVersionsResultsData
}

func (v *SecretsListVersionsResults) SetSecret(secret *SecretInfo) {
	v.data.Secret = secret
}

func (v *SecretsListVersionsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsListVersionsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsListVersionsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsListVersionsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsKeyringArgsData struct{}

type SecretsKeyringArgs struct {
	call rpc.Call
	data secretsKeyringArgsData
}

func (v *SecretsKeyringArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsKeyringArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsKeyringArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsKeyringArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsKeyringResultsData struct {
	Keys         *[]*KeyInfo `cbor:"0,keyasint,omitempty" json:"keys,omitempty"`
	Rotating     *bool       `cbor:"1,keyasint,omitempty" json:"rotating,omitempty"`
	RotatingFrom *string     `cbor:"2,keyasint,omitempty" json:"rotating_from,omitempty"`
	Rewrapped    *int64      `cbor:"3,keyasint,omitempty" json:"rewrapped,omitempty"`
}

type SecretsKeyringResults struct {
	call rpc.Call
	data secretsKeyringResultsData
}

func (v *SecretsKeyringResults) SetKeys(keys []*KeyInfo) {
	x := slices.Clone(keys)
	v.data.Keys = &x
}

func (v *SecretsKeyringResults) SetRotating(rotating bool) {
	v.data.Rotating = &rotating
}

func (v *SecretsKeyringResults) SetRotatingFrom(rotating_from string) {
	v.data.RotatingFrom = &rotating_from
}

func (v *SecretsKeyringResults) SetRewrapped(rewrapped int64) {
	v.data.Rewrapped = &rewrapped
}

func (v *SecretsKeyringResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsKeyringResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsKeyringResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsKeyringResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsRotateKeyArgsData struct{}

type SecretsRotateKeyArgs struct {
	call rpc.Call
	data secretsRotateKeyArgsData
}

func (v *SecretsRotateKeyArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsRotateKeyArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsRotateKeyArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsRotateKeyArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsRotateKeyResultsData struct {
	FromKey *string `cbor:"0,keyasint,omitempty" json:"from_key,omitempty"`
	ToKey   *string `cbor:"1,keyasint,omitempty" json:"to_key,omitempty"`
}

type SecretsRotateKeyResults struct {
	call rpc.Call
	data secretsRotateKeyResultsData
}

func (v *SecretsRotateKeyResults) SetFromKey(from_key string) {
	v.data.FromKey = &from_key
}

func (v *SecretsRotateKeyResults) SetToKey(to_key string) {
	v.data.ToKey = &to_key
}

func (v *SecretsRotateKeyResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsRotateKeyResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsRotateKeyResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsRotateKeyResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsResolveArgsData struct {
	Backend *string `cbor:"0,keyasint,omitempty" json:"backend,omitempty"`
	Ref     *string `cbor:"1,keyasint,omitempty" json:"ref,omitempty"`
}

type SecretsResolveArgs struct {
	call rpc.Call
	data secretsResolveArgsData
}

func (v *SecretsResolveArgs) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretsResolveArgs) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretsResolveArgs) HasRef() bool {
	return v.data.Ref != nil
}

func (v *SecretsResolveArgs) Ref() string {
	if v.data.Ref == nil {
		return ""
	}
	return *v.data.Ref
}

func (v *SecretsResolveArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsResolveArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsResolveArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsResolveArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsResolveResultsData struct {
	Ref   *string `cbor:"0,keyasint,omitempty" json:"ref,omitempty"`
	Value *[]byte `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
}

type SecretsResolveResults struct {
	call rpc.Call
	data secretsResolveResultsData
}

func (v *SecretsResolveResults) SetRef(ref string) {
	v.data.Ref = &ref
}

func (v *SecretsResolveResults) SetValue(value []byte) {
	x := slices.Clone(value)
	v.data.Value = &x
}

func (v *SecretsResolveResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsResolveResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsResolveResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsResolveResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsSetStateArgsData struct {
	Backend *string `cbor:"0,keyasint,omitempty" json:"backend,omitempty"`
	Ref     *string `cbor:"1,keyasint,omitempty" json:"ref,omitempty"`
	State   *string `cbor:"2,keyasint,omitempty" json:"state,omitempty"`
}

type SecretsSetStateArgs struct {
	call rpc.Call
	data secretsSetStateArgsData
}

func (v *SecretsSetStateArgs) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *SecretsSetStateArgs) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *SecretsSetStateArgs) HasRef() bool {
	return v.data.Ref != nil
}

func (v *SecretsSetStateArgs) Ref() string {
	if v.data.Ref == nil {
		return ""
	}
	return *v.data.Ref
}

func (v *SecretsSetStateArgs) HasState() bool {
	return v.data.State != nil
}

func (v *SecretsSetStateArgs) State() string {
	if v.data.State == nil {
		return ""
	}
	return *v.data.State
}

func (v *SecretsSetStateArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsSetStateArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsSetStateArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsSetStateArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type secretsSetStateResultsData struct{}

type SecretsSetStateResults struct {
	call rpc.Call
	data secretsSetStateResultsData
}

func (v *SecretsSetStateResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *SecretsSetStateResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *SecretsSetStateResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *SecretsSetStateResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type SecretsSet struct {
	rpc.Call
	args    SecretsSetArgs
	results SecretsSetResults
}

func (t *SecretsSet) Args() *SecretsSetArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsSet) Results() *SecretsSetResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SecretsList struct {
	rpc.Call
	args    SecretsListArgs
	results SecretsListResults
}

func (t *SecretsList) Args() *SecretsListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsList) Results() *SecretsListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SecretsListVersions struct {
	rpc.Call
	args    SecretsListVersionsArgs
	results SecretsListVersionsResults
}

func (t *SecretsListVersions) Args() *SecretsListVersionsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsListVersions) Results() *SecretsListVersionsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SecretsKeyring struct {
	rpc.Call
	args    SecretsKeyringArgs
	results SecretsKeyringResults
}

func (t *SecretsKeyring) Args() *SecretsKeyringArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsKeyring) Results() *SecretsKeyringResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SecretsRotateKey struct {
	rpc.Call
	args    SecretsRotateKeyArgs
	results SecretsRotateKeyResults
}

func (t *SecretsRotateKey) Args() *SecretsRotateKeyArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsRotateKey) Results() *SecretsRotateKeyResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SecretsResolve struct {
	rpc.Call
	args    SecretsResolveArgs
	results SecretsResolveResults
}

func (t *SecretsResolve) Args() *SecretsResolveArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsResolve) Results() *SecretsResolveResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type SecretsSetState struct {
	rpc.Call
	args    SecretsSetStateArgs
	results SecretsSetStateResults
}

func (t *SecretsSetState) Args() *SecretsSetStateArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *SecretsSetState) Results() *SecretsSetStateResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Secrets interface {
	Set(ctx context.Context, state *SecretsSet) error
	List(ctx context.Context, state *SecretsList) error
	ListVersions(ctx context.Context, state *SecretsListVersions) error
	Keyring(ctx context.Context, state *SecretsKeyring) error
	RotateKey(ctx context.Context, state *SecretsRotateKey) error
	Resolve(ctx context.Context, state *SecretsResolve) error
	SetState(ctx context.Context, state *SecretsSetState) error
}

type reexportSecrets struct {
	client rpc.Client
}

func (reexportSecrets) Set(ctx context.Context, state *SecretsSet) error {
	panic("not implemented")
}

func (reexportSecrets) List(ctx context.Context, state *SecretsList) error {
	panic("not implemented")
}

func (reexportSecrets) ListVersions(ctx context.Context, state *SecretsListVersions) error {
	panic("not implemented")
}

func (reexportSecrets) Keyring(ctx context.Context, state *SecretsKeyring) error {
	panic("not implemented")
}

func (reexportSecrets) RotateKey(ctx context.Context, state *SecretsRotateKey) error {
	panic("not implemented")
}

func (reexportSecrets) Resolve(ctx context.Context, state *SecretsResolve) error {
	panic("not implemented")
}

func (reexportSecrets) SetState(ctx context.Context, state *SecretsSetState) error {
	panic("not implemented")
}

func (t reexportSecrets) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptSecrets(t Secrets) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "set",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{"backend", "path", "value"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Set(ctx, &SecretsSet{Call: call})
			},
		},
		{
			Name:          "list",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{"backend"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &SecretsList{Call: call})
			},
		},
		{
			Name:          "listVersions",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{"backend", "path"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListVersions(ctx, &SecretsListVersions{Call: call})
			},
		},
		{
			Name:          "keyring",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Keyring(ctx, &SecretsKeyring{Call: call})
			},
		},
		{
			Name:          "rotateKey",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.RotateKey(ctx, &SecretsRotateKey{Call: call})
			},
		},
		{
			Name:          "resolve",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{"backend", "ref"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Resolve(ctx, &SecretsResolve{Call: call})
			},
		},
		{
			Name:          "setState",
			InterfaceName: "Secrets",
			Index:         0,
			Public:        false,
			Params:        []string{"backend", "ref", "state"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetState(ctx, &SecretsSetState{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type SecretsClient struct {
	rpc.Client
}

func NewSecretsClient(client rpc.Client) *SecretsClient {
	return &SecretsClient{Client: client}
}

func (c SecretsClient) Export() Secrets {
	return reexportSecrets{client: c.Client}
}

type SecretsClientSetResults struct {
	client rpc.Client
	data   secretsSetResultsData
}

func (v *SecretsClientSetResults) HasVersion() bool {
	return v.data.Version != nil
}

func (v *SecretsClientSetResults) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *SecretsClientSetResults) HasUnchanged() bool {
	return v.data.Unchanged != nil
}

func (v *SecretsClientSetResults) Unchanged() bool {
	if v.data.Unchanged == nil {
		return false
	}
	return *v.data.Unchanged
}

func (v SecretsClient) Set(ctx context.Context, backend string, path string, value []byte) (*SecretsClientSetResults, error) {
	args := SecretsSetArgs{}
	args.data.Backend = &backend
	args.data.Path = &path
	args.data.Value = &value

	var ret secretsSetResultsData

	err := v.Call(ctx, "set", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientSetResults{client: v.Client, data: ret}, nil
}

type SecretsClientListResults struct {
	client rpc.Client
	data   secretsListResultsData
}

func (v *SecretsClientListResults) HasSecrets() bool {
	return v.data.Secrets != nil
}

func (v *SecretsClientListResults) Secrets() []*SecretInfo {
	if v.data.Secrets == nil {
		return nil
	}
	return *v.data.Secrets
}

func (v SecretsClient) List(ctx context.Context, backend string) (*SecretsClientListResults, error) {
	args := SecretsListArgs{}
	args.data.Backend = &backend

	var ret secretsListResultsData

	err := v.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientListResults{client: v.Client, data: ret}, nil
}

type SecretsClientListVersionsResults struct {
	client rpc.Client
	data   secretsListVersionsResultsData
}

func (v *SecretsClientListVersionsResults) HasSecret() bool {
	return v.data.Secret != nil
}

func (v *SecretsClientListVersionsResults) Secret() *SecretInfo {
	return v.data.Secret
}

func (v SecretsClient) ListVersions(ctx context.Context, backend string, path string) (*SecretsClientListVersionsResults, error) {
	args := SecretsListVersionsArgs{}
	args.data.Backend = &backend
	args.data.Path = &path

	var ret secretsListVersionsResultsData

	err := v.Call(ctx, "listVersions", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientListVersionsResults{client: v.Client, data: ret}, nil
}

type SecretsClientKeyringResults struct {
	client rpc.Client
	data   secretsKeyringResultsData
}

func (v *SecretsClientKeyringResults) HasKeys() bool {
	return v.data.Keys != nil
}

func (v *SecretsClientKeyringResults) Keys() []*KeyInfo {
	if v.data.Keys == nil {
		return nil
	}
	return *v.data.Keys
}

func (v *SecretsClientKeyringResults) HasRotating() bool {
	return v.data.Rotating != nil
}

func (v *SecretsClientKeyringResults) Rotating() bool {
	if v.data.Rotating == nil {
		return false
	}
	return *v.data.Rotating
}

func (v *SecretsClientKeyringResults) HasRotatingFrom() bool {
	return v.data.RotatingFrom != nil
}

func (v *SecretsClientKeyringResults) RotatingFrom() string {
	if v.data.RotatingFrom == nil {
		return ""
	}
	return *v.data.RotatingFrom
}

func (v *SecretsClientKeyringResults) HasRewrapped() bool {
	return v.data.Rewrapped != nil
}

func (v *SecretsClientKeyringResults) Rewrapped() int64 {
	if v.data.Rewrapped == nil {
		return 0
	}
	return *v.data.Rewrapped
}

func (v SecretsClient) Keyring(ctx context.Context) (*SecretsClientKeyringResults, error) {
	args := SecretsKeyringArgs{}

	var ret secretsKeyringResultsData

	err := v.Call(ctx, "keyring", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientKeyringResults{client: v.Client, data: ret}, nil
}

type SecretsClientRotateKeyResults struct {
	client rpc.Client
	data   secretsRotateKeyResultsData
}

func (v *SecretsClientRotateKeyResults) HasFromKey() bool {
	return v.data.FromKey != nil
}

func (v *SecretsClientRotateKeyResults) FromKey() string {
	if v.data.FromKey == nil {
		return ""
	}
	return *v.data.FromKey
}

func (v *SecretsClientRotateKeyResults) HasToKey() bool {
	return v.data.ToKey != nil
}

func (v *SecretsClientRotateKeyResults) ToKey() string {
	if v.data.ToKey == nil {
		return ""
	}
	return *v.data.ToKey
}

func (v SecretsClient) RotateKey(ctx context.Context) (*SecretsClientRotateKeyResults, error) {
	args := SecretsRotateKeyArgs{}

	var ret secretsRotateKeyResultsData

	err := v.Call(ctx, "rotateKey", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientRotateKeyResults{client: v.Client, data: ret}, nil
}

type SecretsClientResolveResults struct {
	client rpc.Client
	data   secretsResolveResultsData
}

func (v *SecretsClientResolveResults) HasRef() bool {
	return v.data.Ref != nil
}

func (v *SecretsClientResolveResults) Ref() string {
	if v.data.Ref == nil {
		return ""
	}
	return *v.data.Ref
}

func (v *SecretsClientResolveResults) HasValue() bool {
	return v.data.Value != nil
}

func (v *SecretsClientResolveResults) Value() []byte {
	if v.data.Value == nil {
		return nil
	}
	return *v.data.Value
}

func (v SecretsClient) Resolve(ctx context.Context, backend string, ref string) (*SecretsClientResolveResults, error) {
	args := SecretsResolveArgs{}
	args.data.Backend = &backend
	args.data.Ref = &ref

	var ret secretsResolveResultsData

	err := v.Call(ctx, "resolve", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientResolveResults{client: v.Client, data: ret}, nil
}

type SecretsClientSetStateResults struct {
	client rpc.Client
	data   secretsSetStateResultsData
}

func (v SecretsClient) SetState(ctx context.Context, backend string, ref string, state string) (*SecretsClientSetStateResults, error) {
	args := SecretsSetStateArgs{}
	args.data.Backend = &backend
	args.data.Ref = &ref
	args.data.State = &state

	var ret secretsSetStateResultsData

	err := v.Call(ctx, "setState", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &SecretsClientSetStateResults{client: v.Client, data: ret}, nil
}
