package deployment_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

type deploymentInfoData struct {
	Id                  *string             `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	AppName             *string             `cbor:"1,keyasint,omitempty" json:"app_name,omitempty"`
	AppVersionId        *string             `cbor:"2,keyasint,omitempty" json:"app_version_id,omitempty"`
	ClusterId           *string             `cbor:"3,keyasint,omitempty" json:"cluster_id,omitempty"`
	Status              *string             `cbor:"4,keyasint,omitempty" json:"status,omitempty"`
	Phase               *string             `cbor:"5,keyasint,omitempty" json:"phase,omitempty"`
	DeployedByUserId    *string             `cbor:"6,keyasint,omitempty" json:"deployed_by_user_id,omitempty"`
	DeployedByUserEmail *string             `cbor:"7,keyasint,omitempty" json:"deployed_by_user_email,omitempty"`
	DeployedAt          *standard.Timestamp `cbor:"8,keyasint,omitempty" json:"deployed_at,omitempty"`
	CompletedAt         *standard.Timestamp `cbor:"9,keyasint,omitempty" json:"completed_at,omitempty"`
	ErrorMessage        *string             `cbor:"10,keyasint,omitempty" json:"error_message,omitempty"`
	BuildLogs           *string             `cbor:"11,keyasint,omitempty" json:"build_logs,omitempty"`
	GitInfo             *GitInfo            `cbor:"12,keyasint,omitempty" json:"git_info,omitempty"`
	DeployedByUserName  *string             `cbor:"21,keyasint,omitempty" json:"deployed_by_user_name,omitempty"`
	SourceDeploymentId  *string             `cbor:"22,keyasint,omitempty" json:"source_deployment_id,omitempty"`
	ShortId             *string             `cbor:"23,keyasint,omitempty" json:"short_id,omitempty"`
	AppVersionShortId   *string             `cbor:"24,keyasint,omitempty" json:"app_version_short_id,omitempty"`
}

type DeploymentInfo struct {
	data deploymentInfoData
}

func (v *DeploymentInfo) HasId() bool {
	return v.data.Id != nil
}

func (v *DeploymentInfo) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DeploymentInfo) SetId(id string) {
	v.data.Id = &id
}

func (v *DeploymentInfo) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *DeploymentInfo) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *DeploymentInfo) SetAppName(app_name string) {
	v.data.AppName = &app_name
}

func (v *DeploymentInfo) HasAppVersionId() bool {
	return v.data.AppVersionId != nil
}

func (v *DeploymentInfo) AppVersionId() string {
	if v.data.AppVersionId == nil {
		return ""
	}
	return *v.data.AppVersionId
}

func (v *DeploymentInfo) SetAppVersionId(app_version_id string) {
	v.data.AppVersionId = &app_version_id
}

func (v *DeploymentInfo) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeploymentInfo) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeploymentInfo) SetClusterId(cluster_id string) {
	v.data.ClusterId = &cluster_id
}

func (v *DeploymentInfo) HasStatus() bool {
	return v.data.Status != nil
}

func (v *DeploymentInfo) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *DeploymentInfo) SetStatus(status string) {
	v.data.Status = &status
}

func (v *DeploymentInfo) HasPhase() bool {
	return v.data.Phase != nil
}

func (v *DeploymentInfo) Phase() string {
	if v.data.Phase == nil {
		return ""
	}
	return *v.data.Phase
}

func (v *DeploymentInfo) SetPhase(phase string) {
	v.data.Phase = &phase
}

func (v *DeploymentInfo) HasDeployedByUserId() bool {
	return v.data.DeployedByUserId != nil
}

func (v *DeploymentInfo) DeployedByUserId() string {
	if v.data.DeployedByUserId == nil {
		return ""
	}
	return *v.data.DeployedByUserId
}

func (v *DeploymentInfo) SetDeployedByUserId(deployed_by_user_id string) {
	v.data.DeployedByUserId = &deployed_by_user_id
}

func (v *DeploymentInfo) HasDeployedByUserEmail() bool {
	return v.data.DeployedByUserEmail != nil
}

func (v *DeploymentInfo) DeployedByUserEmail() string {
	if v.data.DeployedByUserEmail == nil {
		return ""
	}
	return *v.data.DeployedByUserEmail
}

func (v *DeploymentInfo) SetDeployedByUserEmail(deployed_by_user_email string) {
	v.data.DeployedByUserEmail = &deployed_by_user_email
}

func (v *DeploymentInfo) HasDeployedAt() bool {
	return v.data.DeployedAt != nil
}

func (v *DeploymentInfo) DeployedAt() *standard.Timestamp {
	return v.data.DeployedAt
}

func (v *DeploymentInfo) SetDeployedAt(deployed_at *standard.Timestamp) {
	v.data.DeployedAt = deployed_at
}

func (v *DeploymentInfo) HasCompletedAt() bool {
	return v.data.CompletedAt != nil
}

func (v *DeploymentInfo) CompletedAt() *standard.Timestamp {
	return v.data.CompletedAt
}

func (v *DeploymentInfo) SetCompletedAt(completed_at *standard.Timestamp) {
	v.data.CompletedAt = completed_at
}

func (v *DeploymentInfo) HasErrorMessage() bool {
	return v.data.ErrorMessage != nil
}

func (v *DeploymentInfo) ErrorMessage() string {
	if v.data.ErrorMessage == nil {
		return ""
	}
	return *v.data.ErrorMessage
}

func (v *DeploymentInfo) SetErrorMessage(error_message string) {
	v.data.ErrorMessage = &error_message
}

func (v *DeploymentInfo) HasBuildLogs() bool {
	return v.data.BuildLogs != nil
}

func (v *DeploymentInfo) BuildLogs() string {
	if v.data.BuildLogs == nil {
		return ""
	}
	return *v.data.BuildLogs
}

func (v *DeploymentInfo) SetBuildLogs(build_logs string) {
	v.data.BuildLogs = &build_logs
}

func (v *DeploymentInfo) HasGitInfo() bool {
	return v.data.GitInfo != nil
}

func (v *DeploymentInfo) GitInfo() *GitInfo {
	return v.data.GitInfo
}

func (v *DeploymentInfo) SetGitInfo(git_info *GitInfo) {
	v.data.GitInfo = git_info
}

func (v *DeploymentInfo) HasDeployedByUserName() bool {
	return v.data.DeployedByUserName != nil
}

func (v *DeploymentInfo) DeployedByUserName() string {
	if v.data.DeployedByUserName == nil {
		return ""
	}
	return *v.data.DeployedByUserName
}

func (v *DeploymentInfo) SetDeployedByUserName(deployed_by_user_name string) {
	v.data.DeployedByUserName = &deployed_by_user_name
}

func (v *DeploymentInfo) HasSourceDeploymentId() bool {
	return v.data.SourceDeploymentId != nil
}

func (v *DeploymentInfo) SourceDeploymentId() string {
	if v.data.SourceDeploymentId == nil {
		return ""
	}
	return *v.data.SourceDeploymentId
}

func (v *DeploymentInfo) SetSourceDeploymentId(source_deployment_id string) {
	v.data.SourceDeploymentId = &source_deployment_id
}

func (v *DeploymentInfo) HasShortId() bool {
	return v.data.ShortId != nil
}

func (v *DeploymentInfo) ShortId() string {
	if v.data.ShortId == nil {
		return ""
	}
	return *v.data.ShortId
}

func (v *DeploymentInfo) SetShortId(short_id string) {
	v.data.ShortId = &short_id
}

func (v *DeploymentInfo) HasAppVersionShortId() bool {
	return v.data.AppVersionShortId != nil
}

func (v *DeploymentInfo) AppVersionShortId() string {
	if v.data.AppVersionShortId == nil {
		return ""
	}
	return *v.data.AppVersionShortId
}

func (v *DeploymentInfo) SetAppVersionShortId(app_version_short_id string) {
	v.data.AppVersionShortId = &app_version_short_id
}

func (v *DeploymentInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type gitInfoData struct {
	Sha               *string             `cbor:"0,keyasint,omitempty" json:"sha,omitempty"`
	Ref               *string             `cbor:"1,keyasint,omitempty" json:"ref,omitempty"`
	Branch            *string             `cbor:"2,keyasint,omitempty" json:"branch,omitempty"`
	Repository        *string             `cbor:"3,keyasint,omitempty" json:"repository,omitempty"`
	IsDirty           *bool               `cbor:"4,keyasint,omitempty" json:"is_dirty,omitempty"`
	WorkingTreeHash   *string             `cbor:"5,keyasint,omitempty" json:"working_tree_hash,omitempty"`
	CommitMessage     *string             `cbor:"6,keyasint,omitempty" json:"commit_message,omitempty"`
	CommitAuthorName  *string             `cbor:"7,keyasint,omitempty" json:"commit_author_name,omitempty"`
	CommitAuthorEmail *string             `cbor:"8,keyasint,omitempty" json:"commit_author_email,omitempty"`
	CommitTimestamp   *standard.Timestamp `cbor:"9,keyasint,omitempty" json:"commit_timestamp,omitempty"`
}

type GitInfo struct {
	data gitInfoData
}

func (v *GitInfo) HasSha() bool {
	return v.data.Sha != nil
}

func (v *GitInfo) Sha() string {
	if v.data.Sha == nil {
		return ""
	}
	return *v.data.Sha
}

func (v *GitInfo) SetSha(sha string) {
	v.data.Sha = &sha
}

func (v *GitInfo) HasRef() bool {
	return v.data.Ref != nil
}

func (v *GitInfo) Ref() string {
	if v.data.Ref == nil {
		return ""
	}
	return *v.data.Ref
}

func (v *GitInfo) SetRef(ref string) {
	v.data.Ref = &ref
}

func (v *GitInfo) HasBranch() bool {
	return v.data.Branch != nil
}

func (v *GitInfo) Branch() string {
	if v.data.Branch == nil {
		return ""
	}
	return *v.data.Branch
}

func (v *GitInfo) SetBranch(branch string) {
	v.data.Branch = &branch
}

func (v *GitInfo) HasRepository() bool {
	return v.data.Repository != nil
}

func (v *GitInfo) Repository() string {
	if v.data.Repository == nil {
		return ""
	}
	return *v.data.Repository
}

func (v *GitInfo) SetRepository(repository string) {
	v.data.Repository = &repository
}

func (v *GitInfo) HasIsDirty() bool {
	return v.data.IsDirty != nil
}

func (v *GitInfo) IsDirty() bool {
	if v.data.IsDirty == nil {
		return false
	}
	return *v.data.IsDirty
}

func (v *GitInfo) SetIsDirty(is_dirty bool) {
	v.data.IsDirty = &is_dirty
}

func (v *GitInfo) HasWorkingTreeHash() bool {
	return v.data.WorkingTreeHash != nil
}

func (v *GitInfo) WorkingTreeHash() string {
	if v.data.WorkingTreeHash == nil {
		return ""
	}
	return *v.data.WorkingTreeHash
}

func (v *GitInfo) SetWorkingTreeHash(working_tree_hash string) {
	v.data.WorkingTreeHash = &working_tree_hash
}

func (v *GitInfo) HasCommitMessage() bool {
	return v.data.CommitMessage != nil
}

func (v *GitInfo) CommitMessage() string {
	if v.data.CommitMessage == nil {
		return ""
	}
	return *v.data.CommitMessage
}

func (v *GitInfo) SetCommitMessage(commit_message string) {
	v.data.CommitMessage = &commit_message
}

func (v *GitInfo) HasCommitAuthorName() bool {
	return v.data.CommitAuthorName != nil
}

func (v *GitInfo) CommitAuthorName() string {
	if v.data.CommitAuthorName == nil {
		return ""
	}
	return *v.data.CommitAuthorName
}

func (v *GitInfo) SetCommitAuthorName(commit_author_name string) {
	v.data.CommitAuthorName = &commit_author_name
}

func (v *GitInfo) HasCommitAuthorEmail() bool {
	return v.data.CommitAuthorEmail != nil
}

func (v *GitInfo) CommitAuthorEmail() string {
	if v.data.CommitAuthorEmail == nil {
		return ""
	}
	return *v.data.CommitAuthorEmail
}

func (v *GitInfo) SetCommitAuthorEmail(commit_author_email string) {
	v.data.CommitAuthorEmail = &commit_author_email
}

func (v *GitInfo) HasCommitTimestamp() bool {
	return v.data.CommitTimestamp != nil
}

func (v *GitInfo) CommitTimestamp() *standard.Timestamp {
	return v.data.CommitTimestamp
}

func (v *GitInfo) SetCommitTimestamp(commit_timestamp *standard.Timestamp) {
	v.data.CommitTimestamp = commit_timestamp
}

func (v *GitInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *GitInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *GitInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *GitInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentLockInfoData struct {
	AppName                   *string             `cbor:"0,keyasint,omitempty" json:"app_name,omitempty"`
	ClusterId                 *string             `cbor:"1,keyasint,omitempty" json:"cluster_id,omitempty"`
	BlockingDeploymentId      *string             `cbor:"2,keyasint,omitempty" json:"blocking_deployment_id,omitempty"`
	StartedBy                 *string             `cbor:"3,keyasint,omitempty" json:"started_by,omitempty"`
	StartedAt                 *standard.Timestamp `cbor:"4,keyasint,omitempty" json:"started_at,omitempty"`
	CurrentPhase              *string             `cbor:"5,keyasint,omitempty" json:"current_phase,omitempty"`
	LockExpiresAt             *standard.Timestamp `cbor:"6,keyasint,omitempty" json:"lock_expires_at,omitempty"`
	BlockingDeploymentShortId *string             `cbor:"7,keyasint,omitempty" json:"blocking_deployment_short_id,omitempty"`
}

type DeploymentLockInfo struct {
	data deploymentLockInfoData
}

func (v *DeploymentLockInfo) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *DeploymentLockInfo) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *DeploymentLockInfo) SetAppName(app_name string) {
	v.data.AppName = &app_name
}

func (v *DeploymentLockInfo) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeploymentLockInfo) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeploymentLockInfo) SetClusterId(cluster_id string) {
	v.data.ClusterId = &cluster_id
}

func (v *DeploymentLockInfo) HasBlockingDeploymentId() bool {
	return v.data.BlockingDeploymentId != nil
}

func (v *DeploymentLockInfo) BlockingDeploymentId() string {
	if v.data.BlockingDeploymentId == nil {
		return ""
	}
	return *v.data.BlockingDeploymentId
}

func (v *DeploymentLockInfo) SetBlockingDeploymentId(blocking_deployment_id string) {
	v.data.BlockingDeploymentId = &blocking_deployment_id
}

func (v *DeploymentLockInfo) HasStartedBy() bool {
	return v.data.StartedBy != nil
}

func (v *DeploymentLockInfo) StartedBy() string {
	if v.data.StartedBy == nil {
		return ""
	}
	return *v.data.StartedBy
}

func (v *DeploymentLockInfo) SetStartedBy(started_by string) {
	v.data.StartedBy = &started_by
}

func (v *DeploymentLockInfo) HasStartedAt() bool {
	return v.data.StartedAt != nil
}

func (v *DeploymentLockInfo) StartedAt() *standard.Timestamp {
	return v.data.StartedAt
}

func (v *DeploymentLockInfo) SetStartedAt(started_at *standard.Timestamp) {
	v.data.StartedAt = started_at
}

func (v *DeploymentLockInfo) HasCurrentPhase() bool {
	return v.data.CurrentPhase != nil
}

func (v *DeploymentLockInfo) CurrentPhase() string {
	if v.data.CurrentPhase == nil {
		return ""
	}
	return *v.data.CurrentPhase
}

func (v *DeploymentLockInfo) SetCurrentPhase(current_phase string) {
	v.data.CurrentPhase = &current_phase
}

func (v *DeploymentLockInfo) HasLockExpiresAt() bool {
	return v.data.LockExpiresAt != nil
}

func (v *DeploymentLockInfo) LockExpiresAt() *standard.Timestamp {
	return v.data.LockExpiresAt
}

func (v *DeploymentLockInfo) SetLockExpiresAt(lock_expires_at *standard.Timestamp) {
	v.data.LockExpiresAt = lock_expires_at
}

func (v *DeploymentLockInfo) HasBlockingDeploymentShortId() bool {
	return v.data.BlockingDeploymentShortId != nil
}

func (v *DeploymentLockInfo) BlockingDeploymentShortId() string {
	if v.data.BlockingDeploymentShortId == nil {
		return ""
	}
	return *v.data.BlockingDeploymentShortId
}

func (v *DeploymentLockInfo) SetBlockingDeploymentShortId(blocking_deployment_short_id string) {
	v.data.BlockingDeploymentShortId = &blocking_deployment_short_id
}

func (v *DeploymentLockInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentLockInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentLockInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentLockInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type environmentVariableData struct {
	Key       *string `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Value     *string `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
	Sensitive *bool   `cbor:"2,keyasint,omitempty" json:"sensitive,omitempty"`
	Backend   *string `cbor:"3,keyasint,omitempty" json:"backend,omitempty"`
}

type EnvironmentVariable struct {
	data environmentVariableData
}

func (v *EnvironmentVariable) HasKey() bool {
	return v.data.Key != nil
}

func (v *EnvironmentVariable) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *EnvironmentVariable) SetKey(key string) {
	v.data.Key = &key
}

func (v *EnvironmentVariable) HasValue() bool {
	return v.data.Value != nil
}

func (v *EnvironmentVariable) Value() string {
	if v.data.Value == nil {
		return ""
	}
	return *v.data.Value
}

func (v *EnvironmentVariable) SetValue(value string) {
	v.data.Value = &value
}

func (v *EnvironmentVariable) HasSensitive() bool {
	return v.data.Sensitive != nil
}

func (v *EnvironmentVariable) Sensitive() bool {
	if v.data.Sensitive == nil {
		return false
	}
	return *v.data.Sensitive
}

func (v *EnvironmentVariable) SetSensitive(sensitive bool) {
	v.data.Sensitive = &sensitive
}

func (v *EnvironmentVariable) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *EnvironmentVariable) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *EnvironmentVariable) SetBackend(backend string) {
	v.data.Backend = &backend
}

func (v *EnvironmentVariable) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *EnvironmentVariable) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *EnvironmentVariable) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *EnvironmentVariable) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type accessInfoData struct {
	Hostnames       *[]string `cbor:"0,keyasint,omitempty" json:"hostnames,omitempty"`
	DefaultRoute    *bool     `cbor:"1,keyasint,omitempty" json:"default_route,omitempty"`
	ClusterHostname *string   `cbor:"2,keyasint,omitempty" json:"cluster_hostname,omitempty"`
}

type AccessInfo struct {
	data accessInfoData
}

func (v *AccessInfo) HasHostnames() bool {
	return v.data.Hostnames != nil
}

func (v *AccessInfo) Hostnames() *[]string {
	return v.data.Hostnames
}

func (v *AccessInfo) SetHostnames(hostnames *[]string) {
	v.data.Hostnames = hostnames
}

func (v *AccessInfo) HasDefaultRoute() bool {
	return v.data.DefaultRoute != nil
}

func (v *AccessInfo) DefaultRoute() bool {
	if v.data.DefaultRoute == nil {
		return false
	}
	return *v.data.DefaultRoute
}

func (v *AccessInfo) SetDefaultRoute(default_route bool) {
	v.data.DefaultRoute = &default_route
}

func (v *AccessInfo) HasClusterHostname() bool {
	return v.data.ClusterHostname != nil
}

func (v *AccessInfo) ClusterHostname() string {
	if v.data.ClusterHostname == nil {
		return ""
	}
	return *v.data.ClusterHostname
}

func (v *AccessInfo) SetClusterHostname(cluster_hostname string) {
	v.data.ClusterHostname = &cluster_hostname
}

func (v *AccessInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AccessInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AccessInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AccessInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentCreateDeploymentArgsData struct {
	AppName      *string  `cbor:"0,keyasint,omitempty" json:"app_name,omitempty"`
	ClusterId    *string  `cbor:"1,keyasint,omitempty" json:"cluster_id,omitempty"`
	AppVersionId *string  `cbor:"2,keyasint,omitempty" json:"app_version_id,omitempty"`
	GitInfo      *GitInfo `cbor:"3,keyasint,omitempty" json:"git_info,omitempty"`
}

type DeploymentCreateDeploymentArgs struct {
	call rpc.Call
	data deploymentCreateDeploymentArgsData
}

func (v *DeploymentCreateDeploymentArgs) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *DeploymentCreateDeploymentArgs) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *DeploymentCreateDeploymentArgs) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeploymentCreateDeploymentArgs) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeploymentCreateDeploymentArgs) HasAppVersionId() bool {
	return v.data.AppVersionId != nil
}

func (v *DeploymentCreateDeploymentArgs) AppVersionId() string {
	if v.data.AppVersionId == nil {
		return ""
	}
	return *v.data.AppVersionId
}

func (v *DeploymentCreateDeploymentArgs) HasGitInfo() bool {
	return v.data.GitInfo != nil
}

func (v *DeploymentCreateDeploymentArgs) GitInfo() *GitInfo {
	return v.data.GitInfo
}

func (v *DeploymentCreateDeploymentArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentCreateDeploymentArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentCreateDeploymentArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentCreateDeploymentArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentCreateDeploymentResultsData struct {
	Deployment *DeploymentInfo     `cbor:"0,keyasint,omitempty" json:"deployment,omitempty"`
	Error      *string             `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
	LockInfo   *DeploymentLockInfo `cbor:"2,keyasint,omitempty" json:"lock_info,omitempty"`
}

type DeploymentCreateDeploymentResults struct {
	call rpc.Call
	data deploymentCreateDeploymentResultsData
}

func (v *DeploymentCreateDeploymentResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentCreateDeploymentResults) SetError(error string) {
	v.data.Error = &error
}

func (v *DeploymentCreateDeploymentResults) SetLockInfo(lock_info *DeploymentLockInfo) {
	v.data.LockInfo = lock_info
}

func (v *DeploymentCreateDeploymentResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentCreateDeploymentResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentCreateDeploymentResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentCreateDeploymentResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentUpdateDeploymentStatusArgsData struct {
	DeploymentId *string `cbor:"0,keyasint,omitempty" json:"deployment_id,omitempty"`
	Status       *string `cbor:"1,keyasint,omitempty" json:"status,omitempty"`
	ErrorMessage *string `cbor:"2,keyasint,omitempty" json:"error_message,omitempty"`
}

type DeploymentUpdateDeploymentStatusArgs struct {
	call rpc.Call
	data deploymentUpdateDeploymentStatusArgsData
}

func (v *DeploymentUpdateDeploymentStatusArgs) HasDeploymentId() bool {
	return v.data.DeploymentId != nil
}

func (v *DeploymentUpdateDeploymentStatusArgs) DeploymentId() string {
	if v.data.DeploymentId == nil {
		return ""
	}
	return *v.data.DeploymentId
}

func (v *DeploymentUpdateDeploymentStatusArgs) HasStatus() bool {
	return v.data.Status != nil
}

func (v *DeploymentUpdateDeploymentStatusArgs) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *DeploymentUpdateDeploymentStatusArgs) HasErrorMessage() bool {
	return v.data.ErrorMessage != nil
}

func (v *DeploymentUpdateDeploymentStatusArgs) ErrorMessage() string {
	if v.data.ErrorMessage == nil {
		return ""
	}
	return *v.data.ErrorMessage
}

func (v *DeploymentUpdateDeploymentStatusArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentStatusArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentUpdateDeploymentStatusArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentStatusArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentUpdateDeploymentStatusResultsData struct {
	Deployment *DeploymentInfo `cbor:"0,keyasint,omitempty" json:"deployment,omitempty"`
}

type DeploymentUpdateDeploymentStatusResults struct {
	call rpc.Call
	data deploymentUpdateDeploymentStatusResultsData
}

func (v *DeploymentUpdateDeploymentStatusResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentUpdateDeploymentStatusResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentStatusResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentUpdateDeploymentStatusResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentStatusResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentUpdateDeploymentPhaseArgsData struct {
	DeploymentId *string `cbor:"0,keyasint,omitempty" json:"deployment_id,omitempty"`
	Phase        *string `cbor:"1,keyasint,omitempty" json:"phase,omitempty"`
}

type DeploymentUpdateDeploymentPhaseArgs struct {
	call rpc.Call
	data deploymentUpdateDeploymentPhaseArgsData
}

func (v *DeploymentUpdateDeploymentPhaseArgs) HasDeploymentId() bool {
	return v.data.DeploymentId != nil
}

func (v *DeploymentUpdateDeploymentPhaseArgs) DeploymentId() string {
	if v.data.DeploymentId == nil {
		return ""
	}
	return *v.data.DeploymentId
}

func (v *DeploymentUpdateDeploymentPhaseArgs) HasPhase() bool {
	return v.data.Phase != nil
}

func (v *DeploymentUpdateDeploymentPhaseArgs) Phase() string {
	if v.data.Phase == nil {
		return ""
	}
	return *v.data.Phase
}

func (v *DeploymentUpdateDeploymentPhaseArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentPhaseArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentUpdateDeploymentPhaseArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentPhaseArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentUpdateDeploymentPhaseResultsData struct {
	Deployment *DeploymentInfo `cbor:"0,keyasint,omitempty" json:"deployment,omitempty"`
}

type DeploymentUpdateDeploymentPhaseResults struct {
	call rpc.Call
	data deploymentUpdateDeploymentPhaseResultsData
}

func (v *DeploymentUpdateDeploymentPhaseResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentUpdateDeploymentPhaseResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentPhaseResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentUpdateDeploymentPhaseResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentPhaseResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentUpdateFailedDeploymentArgsData struct {
	DeploymentId *string `cbor:"0,keyasint,omitempty" json:"deployment_id,omitempty"`
	ErrorMessage *string `cbor:"1,keyasint,omitempty" json:"error_message,omitempty"`
	BuildLogs    *string `cbor:"2,keyasint,omitempty" json:"build_logs,omitempty"`
}

type DeploymentUpdateFailedDeploymentArgs struct {
	call rpc.Call
	data deploymentUpdateFailedDeploymentArgsData
}

func (v *DeploymentUpdateFailedDeploymentArgs) HasDeploymentId() bool {
	return v.data.DeploymentId != nil
}

func (v *DeploymentUpdateFailedDeploymentArgs) DeploymentId() string {
	if v.data.DeploymentId == nil {
		return ""
	}
	return *v.data.DeploymentId
}

func (v *DeploymentUpdateFailedDeploymentArgs) HasErrorMessage() bool {
	return v.data.ErrorMessage != nil
}

func (v *DeploymentUpdateFailedDeploymentArgs) ErrorMessage() string {
	if v.data.ErrorMessage == nil {
		return ""
	}
	return *v.data.ErrorMessage
}

func (v *DeploymentUpdateFailedDeploymentArgs) HasBuildLogs() bool {
	return v.data.BuildLogs != nil
}

func (v *DeploymentUpdateFailedDeploymentArgs) BuildLogs() string {
	if v.data.BuildLogs == nil {
		return ""
	}
	return *v.data.BuildLogs
}

func (v *DeploymentUpdateFailedDeploymentArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentUpdateFailedDeploymentArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentUpdateFailedDeploymentArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentUpdateFailedDeploymentArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentUpdateFailedDeploymentResultsData struct {
	Deployment *DeploymentInfo `cbor:"0,keyasint,omitempty" json:"deployment,omitempty"`
}

type DeploymentUpdateFailedDeploymentResults struct {
	call rpc.Call
	data deploymentUpdateFailedDeploymentResultsData
}

func (v *DeploymentUpdateFailedDeploymentResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentUpdateFailedDeploymentResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentUpdateFailedDeploymentResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentUpdateFailedDeploymentResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentUpdateFailedDeploymentResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentUpdateDeploymentAppVersionArgsData struct {
	DeploymentId *string `cbor:"0,keyasint,omitempty" json:"deployment_id,omitempty"`
	AppVersionId *string `cbor:"1,keyasint,omitempty" json:"app_version_id,omitempty"`
}

type DeploymentUpdateDeploymentAppVersionArgs struct {
	call rpc.Call
	data deploymentUpdateDeploymentAppVersionArgsData
}

func (v *DeploymentUpdateDeploymentAppVersionArgs) HasDeploymentId() bool {
	return v.data.DeploymentId != nil
}

func (v *DeploymentUpdateDeploymentAppVersionArgs) DeploymentId() string {
	if v.data.DeploymentId == nil {
		return ""
	}
	return *v.data.DeploymentId
}

func (v *DeploymentUpdateDeploymentAppVersionArgs) HasAppVersionId() bool {
	return v.data.AppVersionId != nil
}

func (v *DeploymentUpdateDeploymentAppVersionArgs) AppVersionId() string {
	if v.data.AppVersionId == nil {
		return ""
	}
	return *v.data.AppVersionId
}

func (v *DeploymentUpdateDeploymentAppVersionArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentAppVersionArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentUpdateDeploymentAppVersionArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentAppVersionArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentUpdateDeploymentAppVersionResultsData struct {
	Deployment *DeploymentInfo `cbor:"0,keyasint,omitempty" json:"deployment,omitempty"`
}

type DeploymentUpdateDeploymentAppVersionResults struct {
	call rpc.Call
	data deploymentUpdateDeploymentAppVersionResultsData
}

func (v *DeploymentUpdateDeploymentAppVersionResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentUpdateDeploymentAppVersionResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentAppVersionResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentUpdateDeploymentAppVersionResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentUpdateDeploymentAppVersionResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentListDeploymentsArgsData struct {
	AppName   *string `cbor:"0,keyasint,omitempty" json:"app_name,omitempty"`
	ClusterId *string `cbor:"1,keyasint,omitempty" json:"cluster_id,omitempty"`
	Status    *string `cbor:"2,keyasint,omitempty" json:"status,omitempty"`
	Limit     *int32  `cbor:"3,keyasint,omitempty" json:"limit,omitempty"`
}

type DeploymentListDeploymentsArgs struct {
	call rpc.Call
	data deploymentListDeploymentsArgsData
}

func (v *DeploymentListDeploymentsArgs) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *DeploymentListDeploymentsArgs) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *DeploymentListDeploymentsArgs) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeploymentListDeploymentsArgs) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeploymentListDeploymentsArgs) HasStatus() bool {
	return v.data.Status != nil
}

func (v *DeploymentListDeploymentsArgs) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *DeploymentListDeploymentsArgs) HasLimit() bool {
	return v.data.Limit != nil
}

func (v *DeploymentListDeploymentsArgs) Limit() int32 {
	if v.data.Limit == nil {
		return 0
	}
	return *v.data.Limit
}

func (v *DeploymentListDeploymentsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentListDeploymentsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentListDeploymentsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentListDeploymentsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentListDeploymentsResultsData struct {
	Deployments *[]*DeploymentInfo `cbor:"0,keyasint,omitempty" json:"deployments,omitempty"`
}

type DeploymentListDeploymentsResults struct {
	call rpc.Call
	data deploymentListDeploymentsResultsData
}

func (v *DeploymentListDeploymentsResults) SetDeployments(deployments []*DeploymentInfo) {
	x := slices.Clone(deployments)
	v.data.Deployments = &x
}

func (v *DeploymentListDeploymentsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentListDeploymentsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentListDeploymentsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentListDeploymentsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentGetDeploymentByIdArgsData struct {
	DeploymentId *string `cbor:"0,keyasint,omitempty" json:"deployment_id,omitempty"`
}

type DeploymentGetDeploymentByIdArgs struct {
	call rpc.Call
	data deploymentGetDeploymentByIdArgsData
}

func (v *DeploymentGetDeploymentByIdArgs) HasDeploymentId() bool {
	return v.data.DeploymentId != nil
}

func (v *DeploymentGetDeploymentByIdArgs) DeploymentId() string {
	if v.data.DeploymentId == nil {
		return ""
	}
	return *v.data.DeploymentId
}

func (v *DeploymentGetDeploymentByIdArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentGetDeploymentByIdArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentGetDeploymentByIdArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentGetDeploymentByIdArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentGetDeploymentByIdResultsData struct {
	Deployment *DeploymentInfo `cbor:"0,keyasint,omitempty" json:"deployment,omitempty"`
}

type DeploymentGetDeploymentByIdResults struct {
	call rpc.Call
	data deploymentGetDeploymentByIdResultsData
}

func (v *DeploymentGetDeploymentByIdResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentGetDeploymentByIdResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentGetDeploymentByIdResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentGetDeploymentByIdResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentGetDeploymentByIdResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentGetActiveDeploymentArgsData struct {
	AppName   *string `cbor:"0,keyasint,omitempty" json:"app_name,omitempty"`
	ClusterId *string `cbor:"1,keyasint,omitempty" json:"cluster_id,omitempty"`
}

type DeploymentGetActiveDeploymentArgs struct {
	call rpc.Call
	data deploymentGetActiveDeploymentArgsData
}

func (v *DeploymentGetActiveDeploymentArgs) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *DeploymentGetActiveDeploymentArgs) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *DeploymentGetActiveDeploymentArgs) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeploymentGetActiveDeploymentArgs) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeploymentGetActiveDeploymentArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentGetActiveDeploymentArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentGetActiveDeploymentArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentGetActiveDeploymentArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentGetActiveDeploymentResultsData struct {
	Deployment *DeploymentInfo `cbor:"0,keyasint,omitempty" json:"deployment,omitempty"`
}

type DeploymentGetActiveDeploymentResults struct {
	call rpc.Call
	data deploymentGetActiveDeploymentResultsData
}

func (v *DeploymentGetActiveDeploymentResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentGetActiveDeploymentResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentGetActiveDeploymentResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentGetActiveDeploymentResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentGetActiveDeploymentResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentCancelDeploymentArgsData struct {
	DeploymentId *string `cbor:"0,keyasint,omitempty" json:"deployment_id,omitempty"`
	CallerUserId *string `cbor:"1,keyasint,omitempty" json:"caller_user_id,omitempty"`
}

type DeploymentCancelDeploymentArgs struct {
	call rpc.Call
	data deploymentCancelDeploymentArgsData
}

func (v *DeploymentCancelDeploymentArgs) HasDeploymentId() bool {
	return v.data.DeploymentId != nil
}

func (v *DeploymentCancelDeploymentArgs) DeploymentId() string {
	if v.data.DeploymentId == nil {
		return ""
	}
	return *v.data.DeploymentId
}

func (v *DeploymentCancelDeploymentArgs) HasCallerUserId() bool {
	return v.data.CallerUserId != nil
}

func (v *DeploymentCancelDeploymentArgs) CallerUserId() string {
	if v.data.CallerUserId == nil {
		return ""
	}
	return *v.data.CallerUserId
}

func (v *DeploymentCancelDeploymentArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentCancelDeploymentArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentCancelDeploymentArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentCancelDeploymentArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentCancelDeploymentResultsData struct {
	Success *bool   `cbor:"0,keyasint,omitempty" json:"success,omitempty"`
	Error   *string `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
}

type DeploymentCancelDeploymentResults struct {
	call rpc.Call
	data deploymentCancelDeploymentResultsData
}

func (v *DeploymentCancelDeploymentResults) SetSuccess(success bool) {
	v.data.Success = &success
}

func (v *DeploymentCancelDeploymentResults) SetError(error string) {
	v.data.Error = &error
}

func (v *DeploymentCancelDeploymentResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentCancelDeploymentResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentCancelDeploymentResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentCancelDeploymentResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentDeployVersionArgsData struct {
	AppName        *string                 `cbor:"0,keyasint,omitempty" json:"app_name,omitempty"`
	ClusterId      *string                 `cbor:"1,keyasint,omitempty" json:"cluster_id,omitempty"`
	AppVersionId   *string                 `cbor:"2,keyasint,omitempty" json:"app_version_id,omitempty"`
	IsRollback     *bool                   `cbor:"3,keyasint,omitempty" json:"is_rollback,omitempty"`
	EnvVars        *[]*EnvironmentVariable `cbor:"4,keyasint,omitempty" json:"env_vars,omitempty"`
	EphemeralLabel *string                 `cbor:"5,keyasint,omitempty" json:"ephemeral_label,omitempty"`
	EphemeralTtl   *string                 `cbor:"6,keyasint,omitempty" json:"ephemeral_ttl,omitempty"`
}

type DeploymentDeployVersionArgs struct {
	call rpc.Call
	data deploymentDeployVersionArgsData
}

func (v *DeploymentDeployVersionArgs) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *DeploymentDeployVersionArgs) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *DeploymentDeployVersionArgs) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeploymentDeployVersionArgs) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeploymentDeployVersionArgs) HasAppVersionId() bool {
	return v.data.AppVersionId != nil
}

func (v *DeploymentDeployVersionArgs) AppVersionId() string {
	if v.data.AppVersionId == nil {
		return ""
	}
	return *v.data.AppVersionId
}

func (v *DeploymentDeployVersionArgs) HasIsRollback() bool {
	return v.data.IsRollback != nil
}

func (v *DeploymentDeployVersionArgs) IsRollback() bool {
	if v.data.IsRollback == nil {
		return false
	}
	return *v.data.IsRollback
}

func (v *DeploymentDeployVersionArgs) HasEnvVars() bool {
	return v.data.EnvVars != nil
}

func (v *DeploymentDeployVersionArgs) EnvVars() []*EnvironmentVariable {
	if v.data.EnvVars == nil {
		return nil
	}
	return *v.data.EnvVars
}

func (v *DeploymentDeployVersionArgs) HasEphemeralLabel() bool {
	return v.data.EphemeralLabel != nil
}

func (v *DeploymentDeployVersionArgs) EphemeralLabel() string {
	if v.data.EphemeralLabel == nil {
		return ""
	}
	return *v.data.EphemeralLabel
}

func (v *DeploymentDeployVersionArgs) HasEphemeralTtl() bool {
	return v.data.EphemeralTtl != nil
}

func (v *DeploymentDeployVersionArgs) EphemeralTtl() string {
	if v.data.EphemeralTtl == nil {
		return ""
	}
	return *v.data.EphemeralTtl
}

func (v *DeploymentDeployVersionArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentDeployVersionArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentDeployVersionArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentDeployVersionArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentDeployVersionResultsData struct {
	Deployment *DeploymentInfo     `cbor:"0,keyasint,omitempty" json:"deployment,omitempty"`
	Error      *string             `cbor:"1,keyasint,omitempty" json:"error,omitempty"`
	LockInfo   *DeploymentLockInfo `cbor:"2,keyasint,omitempty" json:"lock_info,omitempty"`
	AccessInfo **AccessInfo        `cbor:"3,keyasint,omitempty" json:"access_info,omitempty"`
}

type DeploymentDeployVersionResults struct {
	call rpc.Call
	data deploymentDeployVersionResultsData
}

func (v *DeploymentDeployVersionResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentDeployVersionResults) SetError(error string) {
	v.data.Error = &error
}

func (v *DeploymentDeployVersionResults) SetLockInfo(lock_info *DeploymentLockInfo) {
	v.data.LockInfo = lock_info
}

func (v *DeploymentDeployVersionResults) SetAccessInfo(access_info **AccessInfo) {
	v.data.AccessInfo = access_info
}

func (v *DeploymentDeployVersionResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentDeployVersionResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentDeployVersionResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentDeployVersionResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentSetEnvVarsArgsData struct {
	AppName   *string                 `cbor:"0,keyasint,omitempty" json:"app_name,omitempty"`
	ClusterId *string                 `cbor:"1,keyasint,omitempty" json:"cluster_id,omitempty"`
	Vars      *[]*EnvironmentVariable `cbor:"2,keyasint,omitempty" json:"vars,omitempty"`
	Service   *string                 `cbor:"3,keyasint,omitempty" json:"service,omitempty"`
}

type DeploymentSetEnvVarsArgs struct {
	call rpc.Call
	data deploymentSetEnvVarsArgsData
}

func (v *DeploymentSetEnvVarsArgs) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *DeploymentSetEnvVarsArgs) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *DeploymentSetEnvVarsArgs) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeploymentSetEnvVarsArgs) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeploymentSetEnvVarsArgs) HasVars() bool {
	return v.data.Vars != nil
}

func (v *DeploymentSetEnvVarsArgs) Vars() []*EnvironmentVariable {
	if v.data.Vars == nil {
		return nil
	}
	return *v.data.Vars
}

func (v *DeploymentSetEnvVarsArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *DeploymentSetEnvVarsArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *DeploymentSetEnvVarsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentSetEnvVarsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentSetEnvVarsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentSetEnvVarsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentSetEnvVarsResultsData struct {
	VersionId  *string             `cbor:"0,keyasint,omitempty" json:"version_id,omitempty"`
	Deployment *DeploymentInfo     `cbor:"1,keyasint,omitempty" json:"deployment,omitempty"`
	Error      *string             `cbor:"2,keyasint,omitempty" json:"error,omitempty"`
	LockInfo   *DeploymentLockInfo `cbor:"3,keyasint,omitempty" json:"lock_info,omitempty"`
	AccessInfo **AccessInfo        `cbor:"4,keyasint,omitempty" json:"access_info,omitempty"`
}

type DeploymentSetEnvVarsResults struct {
	call rpc.Call
	data deploymentSetEnvVarsResultsData
}

func (v *DeploymentSetEnvVarsResults) SetVersionId(version_id string) {
	v.data.VersionId = &version_id
}

func (v *DeploymentSetEnvVarsResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentSetEnvVarsResults) SetError(error string) {
	v.data.Error = &error
}

func (v *DeploymentSetEnvVarsResults) SetLockInfo(lock_info *DeploymentLockInfo) {
	v.data.LockInfo = lock_info
}

func (v *DeploymentSetEnvVarsResults) SetAccessInfo(access_info **AccessInfo) {
	v.data.AccessInfo = access_info
}

func (v *DeploymentSetEnvVarsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentSetEnvVarsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentSetEnvVarsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentSetEnvVarsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentGetDeployLockArgsData struct {
	AppName   *string `cbor:"0,keyasint,omitempty" json:"app_name,omitempty"`
	ClusterId *string `cbor:"1,keyasint,omitempty" json:"cluster_id,omitempty"`
}

type DeploymentGetDeployLockArgs struct {
	call rpc.Call
	data deploymentGetDeployLockArgsData
}

func (v *DeploymentGetDeployLockArgs) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *DeploymentGetDeployLockArgs) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *DeploymentGetDeployLockArgs) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeploymentGetDeployLockArgs) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeploymentGetDeployLockArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentGetDeployLockArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentGetDeployLockArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentGetDeployLockArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentGetDeployLockResultsData struct {
	Held     *bool               `cbor:"0,keyasint,omitempty" json:"held,omitempty"`
	LockInfo *DeploymentLockInfo `cbor:"1,keyasint,omitempty" json:"lock_info,omitempty"`
}

type DeploymentGetDeployLockResults struct {
	call rpc.Call
	data deploymentGetDeployLockResultsData
}

func (v *DeploymentGetDeployLockResults) SetHeld(held bool) {
	v.data.Held = &held
}

func (v *DeploymentGetDeployLockResults) SetLockInfo(lock_info *DeploymentLockInfo) {
	v.data.LockInfo = lock_info
}

func (v *DeploymentGetDeployLockResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentGetDeployLockResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentGetDeployLockResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentGetDeployLockResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentDeleteEnvVarsArgsData struct {
	AppName   *string   `cbor:"0,keyasint,omitempty" json:"app_name,omitempty"`
	ClusterId *string   `cbor:"1,keyasint,omitempty" json:"cluster_id,omitempty"`
	Keys      *[]string `cbor:"2,keyasint,omitempty" json:"keys,omitempty"`
	Service   *string   `cbor:"3,keyasint,omitempty" json:"service,omitempty"`
}

type DeploymentDeleteEnvVarsArgs struct {
	call rpc.Call
	data deploymentDeleteEnvVarsArgsData
}

func (v *DeploymentDeleteEnvVarsArgs) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *DeploymentDeleteEnvVarsArgs) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *DeploymentDeleteEnvVarsArgs) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeploymentDeleteEnvVarsArgs) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeploymentDeleteEnvVarsArgs) HasKeys() bool {
	return v.data.Keys != nil
}

func (v *DeploymentDeleteEnvVarsArgs) Keys() []string {
	if v.data.Keys == nil {
		return nil
	}
	return *v.data.Keys
}

func (v *DeploymentDeleteEnvVarsArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *DeploymentDeleteEnvVarsArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *DeploymentDeleteEnvVarsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentDeleteEnvVarsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentDeleteEnvVarsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentDeleteEnvVarsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentDeleteEnvVarsResultsData struct {
	VersionId      *string             `cbor:"0,keyasint,omitempty" json:"version_id,omitempty"`
	Deployment     *DeploymentInfo     `cbor:"1,keyasint,omitempty" json:"deployment,omitempty"`
	Error          *string             `cbor:"2,keyasint,omitempty" json:"error,omitempty"`
	LockInfo       *DeploymentLockInfo `cbor:"3,keyasint,omitempty" json:"lock_info,omitempty"`
	AccessInfo     **AccessInfo        `cbor:"4,keyasint,omitempty" json:"access_info,omitempty"`
	DeletedSources *[]string           `cbor:"5,keyasint,omitempty" json:"deleted_sources,omitempty"`
}

type DeploymentDeleteEnvVarsResults struct {
	call rpc.Call
	data deploymentDeleteEnvVarsResultsData
}

func (v *DeploymentDeleteEnvVarsResults) SetVersionId(version_id string) {
	v.data.VersionId = &version_id
}

func (v *DeploymentDeleteEnvVarsResults) SetDeployment(deployment *DeploymentInfo) {
	v.data.Deployment = deployment
}

func (v *DeploymentDeleteEnvVarsResults) SetError(error string) {
	v.data.Error = &error
}

func (v *DeploymentDeleteEnvVarsResults) SetLockInfo(lock_info *DeploymentLockInfo) {
	v.data.LockInfo = lock_info
}

func (v *DeploymentDeleteEnvVarsResults) SetAccessInfo(access_info **AccessInfo) {
	v.data.AccessInfo = access_info
}

func (v *DeploymentDeleteEnvVarsResults) SetDeletedSources(deleted_sources *[]string) {
	v.data.DeletedSources = deleted_sources
}

func (v *DeploymentDeleteEnvVarsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentDeleteEnvVarsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentDeleteEnvVarsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentDeleteEnvVarsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type DeploymentCreateDeployment struct {
	rpc.Call
	args    DeploymentCreateDeploymentArgs
	results DeploymentCreateDeploymentResults
}

func (t *DeploymentCreateDeployment) Args() *DeploymentCreateDeploymentArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentCreateDeployment) Results() *DeploymentCreateDeploymentResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentUpdateDeploymentStatus struct {
	rpc.Call
	args    DeploymentUpdateDeploymentStatusArgs
	results DeploymentUpdateDeploymentStatusResults
}

func (t *DeploymentUpdateDeploymentStatus) Args() *DeploymentUpdateDeploymentStatusArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentUpdateDeploymentStatus) Results() *DeploymentUpdateDeploymentStatusResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentUpdateDeploymentPhase struct {
	rpc.Call
	args    DeploymentUpdateDeploymentPhaseArgs
	results DeploymentUpdateDeploymentPhaseResults
}

func (t *DeploymentUpdateDeploymentPhase) Args() *DeploymentUpdateDeploymentPhaseArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentUpdateDeploymentPhase) Results() *DeploymentUpdateDeploymentPhaseResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentUpdateFailedDeployment struct {
	rpc.Call
	args    DeploymentUpdateFailedDeploymentArgs
	results DeploymentUpdateFailedDeploymentResults
}

func (t *DeploymentUpdateFailedDeployment) Args() *DeploymentUpdateFailedDeploymentArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentUpdateFailedDeployment) Results() *DeploymentUpdateFailedDeploymentResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentUpdateDeploymentAppVersion struct {
	rpc.Call
	args    DeploymentUpdateDeploymentAppVersionArgs
	results DeploymentUpdateDeploymentAppVersionResults
}

func (t *DeploymentUpdateDeploymentAppVersion) Args() *DeploymentUpdateDeploymentAppVersionArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentUpdateDeploymentAppVersion) Results() *DeploymentUpdateDeploymentAppVersionResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentListDeployments struct {
	rpc.Call
	args    DeploymentListDeploymentsArgs
	results DeploymentListDeploymentsResults
}

func (t *DeploymentListDeployments) Args() *DeploymentListDeploymentsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentListDeployments) Results() *DeploymentListDeploymentsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentGetDeploymentById struct {
	rpc.Call
	args    DeploymentGetDeploymentByIdArgs
	results DeploymentGetDeploymentByIdResults
}

func (t *DeploymentGetDeploymentById) Args() *DeploymentGetDeploymentByIdArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentGetDeploymentById) Results() *DeploymentGetDeploymentByIdResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentGetActiveDeployment struct {
	rpc.Call
	args    DeploymentGetActiveDeploymentArgs
	results DeploymentGetActiveDeploymentResults
}

func (t *DeploymentGetActiveDeployment) Args() *DeploymentGetActiveDeploymentArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentGetActiveDeployment) Results() *DeploymentGetActiveDeploymentResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentCancelDeployment struct {
	rpc.Call
	args    DeploymentCancelDeploymentArgs
	results DeploymentCancelDeploymentResults
}

func (t *DeploymentCancelDeployment) Args() *DeploymentCancelDeploymentArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentCancelDeployment) Results() *DeploymentCancelDeploymentResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentDeployVersion struct {
	rpc.Call
	args    DeploymentDeployVersionArgs
	results DeploymentDeployVersionResults
}

func (t *DeploymentDeployVersion) Args() *DeploymentDeployVersionArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentDeployVersion) Results() *DeploymentDeployVersionResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentSetEnvVars struct {
	rpc.Call
	args    DeploymentSetEnvVarsArgs
	results DeploymentSetEnvVarsResults
}

func (t *DeploymentSetEnvVars) Args() *DeploymentSetEnvVarsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentSetEnvVars) Results() *DeploymentSetEnvVarsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentGetDeployLock struct {
	rpc.Call
	args    DeploymentGetDeployLockArgs
	results DeploymentGetDeployLockResults
}

func (t *DeploymentGetDeployLock) Args() *DeploymentGetDeployLockArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentGetDeployLock) Results() *DeploymentGetDeployLockResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DeploymentDeleteEnvVars struct {
	rpc.Call
	args    DeploymentDeleteEnvVarsArgs
	results DeploymentDeleteEnvVarsResults
}

func (t *DeploymentDeleteEnvVars) Args() *DeploymentDeleteEnvVarsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DeploymentDeleteEnvVars) Results() *DeploymentDeleteEnvVarsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Deployment interface {
	CreateDeployment(ctx context.Context, state *DeploymentCreateDeployment) error
	UpdateDeploymentStatus(ctx context.Context, state *DeploymentUpdateDeploymentStatus) error
	UpdateDeploymentPhase(ctx context.Context, state *DeploymentUpdateDeploymentPhase) error
	UpdateFailedDeployment(ctx context.Context, state *DeploymentUpdateFailedDeployment) error
	UpdateDeploymentAppVersion(ctx context.Context, state *DeploymentUpdateDeploymentAppVersion) error
	ListDeployments(ctx context.Context, state *DeploymentListDeployments) error
	GetDeploymentById(ctx context.Context, state *DeploymentGetDeploymentById) error
	GetActiveDeployment(ctx context.Context, state *DeploymentGetActiveDeployment) error
	CancelDeployment(ctx context.Context, state *DeploymentCancelDeployment) error
	DeployVersion(ctx context.Context, state *DeploymentDeployVersion) error
	SetEnvVars(ctx context.Context, state *DeploymentSetEnvVars) error
	GetDeployLock(ctx context.Context, state *DeploymentGetDeployLock) error
	DeleteEnvVars(ctx context.Context, state *DeploymentDeleteEnvVars) error
}

type reexportDeployment struct {
	client rpc.Client
}

func (reexportDeployment) CreateDeployment(ctx context.Context, state *DeploymentCreateDeployment) error {
	panic("not implemented")
}

func (reexportDeployment) UpdateDeploymentStatus(ctx context.Context, state *DeploymentUpdateDeploymentStatus) error {
	panic("not implemented")
}

func (reexportDeployment) UpdateDeploymentPhase(ctx context.Context, state *DeploymentUpdateDeploymentPhase) error {
	panic("not implemented")
}

func (reexportDeployment) UpdateFailedDeployment(ctx context.Context, state *DeploymentUpdateFailedDeployment) error {
	panic("not implemented")
}

func (reexportDeployment) UpdateDeploymentAppVersion(ctx context.Context, state *DeploymentUpdateDeploymentAppVersion) error {
	panic("not implemented")
}

func (reexportDeployment) ListDeployments(ctx context.Context, state *DeploymentListDeployments) error {
	panic("not implemented")
}

func (reexportDeployment) GetDeploymentById(ctx context.Context, state *DeploymentGetDeploymentById) error {
	panic("not implemented")
}

func (reexportDeployment) GetActiveDeployment(ctx context.Context, state *DeploymentGetActiveDeployment) error {
	panic("not implemented")
}

func (reexportDeployment) CancelDeployment(ctx context.Context, state *DeploymentCancelDeployment) error {
	panic("not implemented")
}

func (reexportDeployment) DeployVersion(ctx context.Context, state *DeploymentDeployVersion) error {
	panic("not implemented")
}

func (reexportDeployment) SetEnvVars(ctx context.Context, state *DeploymentSetEnvVars) error {
	panic("not implemented")
}

func (reexportDeployment) GetDeployLock(ctx context.Context, state *DeploymentGetDeployLock) error {
	panic("not implemented")
}

func (reexportDeployment) DeleteEnvVars(ctx context.Context, state *DeploymentDeleteEnvVars) error {
	panic("not implemented")
}

func (t reexportDeployment) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptDeployment(t Deployment) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "CreateDeployment",
			InterfaceName: "Deployment",
			Index:         0,
			Public:        false,
			Params:        []string{"app_name", "cluster_id", "app_version_id", "git_info"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.CreateDeployment(ctx, &DeploymentCreateDeployment{Call: call})
			},
		},
		{
			Name:          "UpdateDeploymentStatus",
			InterfaceName: "Deployment",
			Index:         1,
			Public:        false,
			Params:        []string{"deployment_id", "status", "error_message"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.UpdateDeploymentStatus(ctx, &DeploymentUpdateDeploymentStatus{Call: call})
			},
		},
		{
			Name:          "UpdateDeploymentPhase",
			InterfaceName: "Deployment",
			Index:         2,
			Public:        false,
			Params:        []string{"deployment_id", "phase"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.UpdateDeploymentPhase(ctx, &DeploymentUpdateDeploymentPhase{Call: call})
			},
		},
		{
			Name:          "UpdateFailedDeployment",
			InterfaceName: "Deployment",
			Index:         3,
			Public:        false,
			Params:        []string{"deployment_id", "error_message", "build_logs"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.UpdateFailedDeployment(ctx, &DeploymentUpdateFailedDeployment{Call: call})
			},
		},
		{
			Name:          "UpdateDeploymentAppVersion",
			InterfaceName: "Deployment",
			Index:         4,
			Public:        false,
			Params:        []string{"deployment_id", "app_version_id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.UpdateDeploymentAppVersion(ctx, &DeploymentUpdateDeploymentAppVersion{Call: call})
			},
		},
		{
			Name:          "ListDeployments",
			InterfaceName: "Deployment",
			Index:         5,
			Public:        false,
			Params:        []string{"app_name", "cluster_id", "status", "limit"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListDeployments(ctx, &DeploymentListDeployments{Call: call})
			},
		},
		{
			Name:          "GetDeploymentById",
			InterfaceName: "Deployment",
			Index:         6,
			Public:        false,
			Params:        []string{"deployment_id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetDeploymentById(ctx, &DeploymentGetDeploymentById{Call: call})
			},
		},
		{
			Name:          "GetActiveDeployment",
			InterfaceName: "Deployment",
			Index:         7,
			Public:        false,
			Params:        []string{"app_name", "cluster_id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetActiveDeployment(ctx, &DeploymentGetActiveDeployment{Call: call})
			},
		},
		{
			Name:          "CancelDeployment",
			InterfaceName: "Deployment",
			Index:         8,
			Public:        false,
			Params:        []string{"deployment_id", "caller_user_id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.CancelDeployment(ctx, &DeploymentCancelDeployment{Call: call})
			},
		},
		{
			Name:          "DeployVersion",
			InterfaceName: "Deployment",
			Index:         9,
			Public:        false,
			Params:        []string{"app_name", "cluster_id", "app_version_id", "is_rollback", "env_vars", "ephemeral_label", "ephemeral_ttl"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.DeployVersion(ctx, &DeploymentDeployVersion{Call: call})
			},
		},
		{
			Name:          "SetEnvVars",
			InterfaceName: "Deployment",
			Index:         10,
			Public:        false,
			Params:        []string{"app_name", "cluster_id", "vars", "service"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetEnvVars(ctx, &DeploymentSetEnvVars{Call: call})
			},
		},
		{
			Name:          "GetDeployLock",
			InterfaceName: "Deployment",
			Index:         12,
			Public:        false,
			Params:        []string{"app_name", "cluster_id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetDeployLock(ctx, &DeploymentGetDeployLock{Call: call})
			},
		},
		{
			Name:          "DeleteEnvVars",
			InterfaceName: "Deployment",
			Index:         11,
			Public:        false,
			Params:        []string{"app_name", "cluster_id", "keys", "service"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.DeleteEnvVars(ctx, &DeploymentDeleteEnvVars{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type DeploymentClient struct {
	rpc.Client
}

func NewDeploymentClient(client rpc.Client) *DeploymentClient {
	return &DeploymentClient{Client: client}
}

func (c DeploymentClient) Export() Deployment {
	return reexportDeployment{client: c.Client}
}

type DeploymentClientCreateDeploymentResults struct {
	client rpc.Client
	data   deploymentCreateDeploymentResultsData
}

func (v *DeploymentClientCreateDeploymentResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientCreateDeploymentResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v *DeploymentClientCreateDeploymentResults) HasError() bool {
	return v.data.Error != nil
}

func (v *DeploymentClientCreateDeploymentResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v *DeploymentClientCreateDeploymentResults) HasLockInfo() bool {
	return v.data.LockInfo != nil
}

func (v *DeploymentClientCreateDeploymentResults) LockInfo() *DeploymentLockInfo {
	return v.data.LockInfo
}

func (v DeploymentClient) CreateDeployment(ctx context.Context, app_name string, cluster_id string, app_version_id string, git_info *GitInfo) (*DeploymentClientCreateDeploymentResults, error) {
	args := DeploymentCreateDeploymentArgs{}
	args.data.AppName = &app_name
	args.data.ClusterId = &cluster_id
	args.data.AppVersionId = &app_version_id
	args.data.GitInfo = git_info

	var ret deploymentCreateDeploymentResultsData

	err := v.Call(ctx, "CreateDeployment", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientCreateDeploymentResults{client: v.Client, data: ret}, nil
}

type DeploymentClientUpdateDeploymentStatusResults struct {
	client rpc.Client
	data   deploymentUpdateDeploymentStatusResultsData
}

func (v *DeploymentClientUpdateDeploymentStatusResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientUpdateDeploymentStatusResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v DeploymentClient) UpdateDeploymentStatus(ctx context.Context, deployment_id string, status string, error_message string) (*DeploymentClientUpdateDeploymentStatusResults, error) {
	args := DeploymentUpdateDeploymentStatusArgs{}
	args.data.DeploymentId = &deployment_id
	args.data.Status = &status
	args.data.ErrorMessage = &error_message

	var ret deploymentUpdateDeploymentStatusResultsData

	err := v.Call(ctx, "UpdateDeploymentStatus", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientUpdateDeploymentStatusResults{client: v.Client, data: ret}, nil
}

type DeploymentClientUpdateDeploymentPhaseResults struct {
	client rpc.Client
	data   deploymentUpdateDeploymentPhaseResultsData
}

func (v *DeploymentClientUpdateDeploymentPhaseResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientUpdateDeploymentPhaseResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v DeploymentClient) UpdateDeploymentPhase(ctx context.Context, deployment_id string, phase string) (*DeploymentClientUpdateDeploymentPhaseResults, error) {
	args := DeploymentUpdateDeploymentPhaseArgs{}
	args.data.DeploymentId = &deployment_id
	args.data.Phase = &phase

	var ret deploymentUpdateDeploymentPhaseResultsData

	err := v.Call(ctx, "UpdateDeploymentPhase", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientUpdateDeploymentPhaseResults{client: v.Client, data: ret}, nil
}

type DeploymentClientUpdateFailedDeploymentResults struct {
	client rpc.Client
	data   deploymentUpdateFailedDeploymentResultsData
}

func (v *DeploymentClientUpdateFailedDeploymentResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientUpdateFailedDeploymentResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v DeploymentClient) UpdateFailedDeployment(ctx context.Context, deployment_id string, error_message string, build_logs string) (*DeploymentClientUpdateFailedDeploymentResults, error) {
	args := DeploymentUpdateFailedDeploymentArgs{}
	args.data.DeploymentId = &deployment_id
	args.data.ErrorMessage = &error_message
	args.data.BuildLogs = &build_logs

	var ret deploymentUpdateFailedDeploymentResultsData

	err := v.Call(ctx, "UpdateFailedDeployment", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientUpdateFailedDeploymentResults{client: v.Client, data: ret}, nil
}

type DeploymentClientUpdateDeploymentAppVersionResults struct {
	client rpc.Client
	data   deploymentUpdateDeploymentAppVersionResultsData
}

func (v *DeploymentClientUpdateDeploymentAppVersionResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientUpdateDeploymentAppVersionResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v DeploymentClient) UpdateDeploymentAppVersion(ctx context.Context, deployment_id string, app_version_id string) (*DeploymentClientUpdateDeploymentAppVersionResults, error) {
	args := DeploymentUpdateDeploymentAppVersionArgs{}
	args.data.DeploymentId = &deployment_id
	args.data.AppVersionId = &app_version_id

	var ret deploymentUpdateDeploymentAppVersionResultsData

	err := v.Call(ctx, "UpdateDeploymentAppVersion", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientUpdateDeploymentAppVersionResults{client: v.Client, data: ret}, nil
}

type DeploymentClientListDeploymentsResults struct {
	client rpc.Client
	data   deploymentListDeploymentsResultsData
}

func (v *DeploymentClientListDeploymentsResults) HasDeployments() bool {
	return v.data.Deployments != nil
}

func (v *DeploymentClientListDeploymentsResults) Deployments() []*DeploymentInfo {
	if v.data.Deployments == nil {
		return nil
	}
	return *v.data.Deployments
}

func (v DeploymentClient) ListDeployments(ctx context.Context, app_name string, cluster_id string, status string, limit int32) (*DeploymentClientListDeploymentsResults, error) {
	args := DeploymentListDeploymentsArgs{}
	args.data.AppName = &app_name
	args.data.ClusterId = &cluster_id
	args.data.Status = &status
	args.data.Limit = &limit

	var ret deploymentListDeploymentsResultsData

	err := v.Call(ctx, "ListDeployments", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientListDeploymentsResults{client: v.Client, data: ret}, nil
}

type DeploymentClientGetDeploymentByIdResults struct {
	client rpc.Client
	data   deploymentGetDeploymentByIdResultsData
}

func (v *DeploymentClientGetDeploymentByIdResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientGetDeploymentByIdResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v DeploymentClient) GetDeploymentById(ctx context.Context, deployment_id string) (*DeploymentClientGetDeploymentByIdResults, error) {
	args := DeploymentGetDeploymentByIdArgs{}
	args.data.DeploymentId = &deployment_id

	var ret deploymentGetDeploymentByIdResultsData

	err := v.Call(ctx, "GetDeploymentById", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientGetDeploymentByIdResults{client: v.Client, data: ret}, nil
}

type DeploymentClientGetActiveDeploymentResults struct {
	client rpc.Client
	data   deploymentGetActiveDeploymentResultsData
}

func (v *DeploymentClientGetActiveDeploymentResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientGetActiveDeploymentResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v DeploymentClient) GetActiveDeployment(ctx context.Context, app_name string, cluster_id string) (*DeploymentClientGetActiveDeploymentResults, error) {
	args := DeploymentGetActiveDeploymentArgs{}
	args.data.AppName = &app_name
	args.data.ClusterId = &cluster_id

	var ret deploymentGetActiveDeploymentResultsData

	err := v.Call(ctx, "GetActiveDeployment", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientGetActiveDeploymentResults{client: v.Client, data: ret}, nil
}

type DeploymentClientCancelDeploymentResults struct {
	client rpc.Client
	data   deploymentCancelDeploymentResultsData
}

func (v *DeploymentClientCancelDeploymentResults) HasSuccess() bool {
	return v.data.Success != nil
}

func (v *DeploymentClientCancelDeploymentResults) Success() bool {
	if v.data.Success == nil {
		return false
	}
	return *v.data.Success
}

func (v *DeploymentClientCancelDeploymentResults) HasError() bool {
	return v.data.Error != nil
}

func (v *DeploymentClientCancelDeploymentResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v DeploymentClient) CancelDeployment(ctx context.Context, deployment_id string, caller_user_id string) (*DeploymentClientCancelDeploymentResults, error) {
	args := DeploymentCancelDeploymentArgs{}
	args.data.DeploymentId = &deployment_id
	args.data.CallerUserId = &caller_user_id

	var ret deploymentCancelDeploymentResultsData

	err := v.Call(ctx, "CancelDeployment", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientCancelDeploymentResults{client: v.Client, data: ret}, nil
}

type DeploymentClientDeployVersionResults struct {
	client rpc.Client
	data   deploymentDeployVersionResultsData
}

func (v *DeploymentClientDeployVersionResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientDeployVersionResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v *DeploymentClientDeployVersionResults) HasError() bool {
	return v.data.Error != nil
}

func (v *DeploymentClientDeployVersionResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v *DeploymentClientDeployVersionResults) HasLockInfo() bool {
	return v.data.LockInfo != nil
}

func (v *DeploymentClientDeployVersionResults) LockInfo() *DeploymentLockInfo {
	return v.data.LockInfo
}

func (v *DeploymentClientDeployVersionResults) HasAccessInfo() bool {
	return v.data.AccessInfo != nil
}

func (v *DeploymentClientDeployVersionResults) AccessInfo() *AccessInfo {
	if v.data.AccessInfo == nil {
		return nil
	}
	return *v.data.AccessInfo
}

func (v DeploymentClient) DeployVersion(ctx context.Context, app_name string, cluster_id string, app_version_id string, is_rollback bool, env_vars []*EnvironmentVariable, ephemeral_label string, ephemeral_ttl string) (*DeploymentClientDeployVersionResults, error) {
	args := DeploymentDeployVersionArgs{}
	args.data.AppName = &app_name
	args.data.ClusterId = &cluster_id
	args.data.AppVersionId = &app_version_id
	args.data.IsRollback = &is_rollback
	args.data.EnvVars = &env_vars
	args.data.EphemeralLabel = &ephemeral_label
	args.data.EphemeralTtl = &ephemeral_ttl

	var ret deploymentDeployVersionResultsData

	err := v.Call(ctx, "DeployVersion", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientDeployVersionResults{client: v.Client, data: ret}, nil
}

type DeploymentClientSetEnvVarsResults struct {
	client rpc.Client
	data   deploymentSetEnvVarsResultsData
}

func (v *DeploymentClientSetEnvVarsResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *DeploymentClientSetEnvVarsResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v *DeploymentClientSetEnvVarsResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientSetEnvVarsResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v *DeploymentClientSetEnvVarsResults) HasError() bool {
	return v.data.Error != nil
}

func (v *DeploymentClientSetEnvVarsResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v *DeploymentClientSetEnvVarsResults) HasLockInfo() bool {
	return v.data.LockInfo != nil
}

func (v *DeploymentClientSetEnvVarsResults) LockInfo() *DeploymentLockInfo {
	return v.data.LockInfo
}

func (v *DeploymentClientSetEnvVarsResults) HasAccessInfo() bool {
	return v.data.AccessInfo != nil
}

func (v *DeploymentClientSetEnvVarsResults) AccessInfo() *AccessInfo {
	if v.data.AccessInfo == nil {
		return nil
	}
	return *v.data.AccessInfo
}

func (v DeploymentClient) SetEnvVars(ctx context.Context, app_name string, cluster_id string, vars []*EnvironmentVariable, service string) (*DeploymentClientSetEnvVarsResults, error) {
	args := DeploymentSetEnvVarsArgs{}
	args.data.AppName = &app_name
	args.data.ClusterId = &cluster_id
	args.data.Vars = &vars
	args.data.Service = &service

	var ret deploymentSetEnvVarsResultsData

	err := v.Call(ctx, "SetEnvVars", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientSetEnvVarsResults{client: v.Client, data: ret}, nil
}

type DeploymentClientGetDeployLockResults struct {
	client rpc.Client
	data   deploymentGetDeployLockResultsData
}

func (v *DeploymentClientGetDeployLockResults) HasHeld() bool {
	return v.data.Held != nil
}

func (v *DeploymentClientGetDeployLockResults) Held() bool {
	if v.data.Held == nil {
		return false
	}
	return *v.data.Held
}

func (v *DeploymentClientGetDeployLockResults) HasLockInfo() bool {
	return v.data.LockInfo != nil
}

func (v *DeploymentClientGetDeployLockResults) LockInfo() *DeploymentLockInfo {
	return v.data.LockInfo
}

func (v DeploymentClient) GetDeployLock(ctx context.Context, app_name string, cluster_id string) (*DeploymentClientGetDeployLockResults, error) {
	args := DeploymentGetDeployLockArgs{}
	args.data.AppName = &app_name
	args.data.ClusterId = &cluster_id

	var ret deploymentGetDeployLockResultsData

	err := v.Call(ctx, "GetDeployLock", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientGetDeployLockResults{client: v.Client, data: ret}, nil
}

type DeploymentClientDeleteEnvVarsResults struct {
	client rpc.Client
	data   deploymentDeleteEnvVarsResultsData
}

func (v *DeploymentClientDeleteEnvVarsResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *DeploymentClientDeleteEnvVarsResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v *DeploymentClientDeleteEnvVarsResults) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *DeploymentClientDeleteEnvVarsResults) Deployment() *DeploymentInfo {
	return v.data.Deployment
}

func (v *DeploymentClientDeleteEnvVarsResults) HasError() bool {
	return v.data.Error != nil
}

func (v *DeploymentClientDeleteEnvVarsResults) Error() string {
	if v.data.Error == nil {
		return ""
	}
	return *v.data.Error
}

func (v *DeploymentClientDeleteEnvVarsResults) HasLockInfo() bool {
	return v.data.LockInfo != nil
}

func (v *DeploymentClientDeleteEnvVarsResults) LockInfo() *DeploymentLockInfo {
	return v.data.LockInfo
}

func (v *DeploymentClientDeleteEnvVarsResults) HasAccessInfo() bool {
	return v.data.AccessInfo != nil
}

func (v *DeploymentClientDeleteEnvVarsResults) AccessInfo() *AccessInfo {
	if v.data.AccessInfo == nil {
		return nil
	}
	return *v.data.AccessInfo
}

func (v *DeploymentClientDeleteEnvVarsResults) HasDeletedSources() bool {
	return v.data.DeletedSources != nil
}

func (v *DeploymentClientDeleteEnvVarsResults) DeletedSources() []string {
	if v.data.DeletedSources == nil {
		return nil
	}
	return *v.data.DeletedSources
}

func (v DeploymentClient) DeleteEnvVars(ctx context.Context, app_name string, cluster_id string, keys []string, service string) (*DeploymentClientDeleteEnvVarsResults, error) {
	args := DeploymentDeleteEnvVarsArgs{}
	args.data.AppName = &app_name
	args.data.ClusterId = &cluster_id
	args.data.Keys = &keys
	args.data.Service = &service

	var ret deploymentDeleteEnvVarsResultsData

	err := v.Call(ctx, "DeleteEnvVars", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DeploymentClientDeleteEnvVarsResults{client: v.Client, data: ret}, nil
}
