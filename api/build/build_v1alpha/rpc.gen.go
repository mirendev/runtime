package build_v1alpha

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

type StatusUpdate interface {
	Which() string
	Message() string
	SetMessage(string)
	Buildkit() []byte
	SetBuildkit([]byte)
	Error() string
	SetError(string)
	Log() *LogEntry
	SetLog(*LogEntry)
	Deployment() *DeploymentProgress
	SetDeployment(*DeploymentProgress)
	Image() string
	SetImage(string)
}

type statusUpdate struct {
	U_Message    *string              `cbor:"1,keyasint,omitempty" json:"message,omitempty"`
	U_Buildkit   *[]byte              `cbor:"2,keyasint,omitempty" json:"buildkit,omitempty"`
	U_Error      *string              `cbor:"3,keyasint,omitempty" json:"error,omitempty"`
	U_Log        **LogEntry           `cbor:"4,keyasint,omitempty" json:"log,omitempty"`
	U_Deployment **DeploymentProgress `cbor:"5,keyasint,omitempty" json:"deployment,omitempty"`
	U_Image      *string              `cbor:"6,keyasint,omitempty" json:"image,omitempty"`
}

func (v *statusUpdate) Which() string {
	if v.U_Message != nil {
		return "message"
	}
	if v.U_Buildkit != nil {
		return "buildkit"
	}
	if v.U_Error != nil {
		return "error"
	}
	if v.U_Log != nil {
		return "log"
	}
	if v.U_Deployment != nil {
		return "deployment"
	}
	if v.U_Image != nil {
		return "image"
	}
	return ""
}

func (v *statusUpdate) Message() string {
	if v.U_Message == nil {
		return ""
	}
	return *v.U_Message
}

func (v *statusUpdate) SetMessage(val string) {
	v.U_Buildkit = nil
	v.U_Error = nil
	v.U_Log = nil
	v.U_Deployment = nil
	v.U_Image = nil
	v.U_Message = &val
}

func (v *statusUpdate) Buildkit() []byte {
	if v.U_Buildkit == nil {
		return nil
	}
	return *v.U_Buildkit
}

func (v *statusUpdate) SetBuildkit(val []byte) {
	v.U_Message = nil
	v.U_Error = nil
	v.U_Log = nil
	v.U_Deployment = nil
	v.U_Image = nil
	v.U_Buildkit = &val
}

func (v *statusUpdate) Error() string {
	if v.U_Error == nil {
		return ""
	}
	return *v.U_Error
}

func (v *statusUpdate) SetError(val string) {
	v.U_Message = nil
	v.U_Buildkit = nil
	v.U_Log = nil
	v.U_Deployment = nil
	v.U_Image = nil
	v.U_Error = &val
}

func (v *statusUpdate) Log() *LogEntry {
	if v.U_Log == nil {
		return nil
	}
	return *v.U_Log
}

func (v *statusUpdate) SetLog(val *LogEntry) {
	v.U_Message = nil
	v.U_Buildkit = nil
	v.U_Error = nil
	v.U_Deployment = nil
	v.U_Image = nil
	v.U_Log = &val
}

func (v *statusUpdate) Deployment() *DeploymentProgress {
	if v.U_Deployment == nil {
		return nil
	}
	return *v.U_Deployment
}

func (v *statusUpdate) SetDeployment(val *DeploymentProgress) {
	v.U_Message = nil
	v.U_Buildkit = nil
	v.U_Error = nil
	v.U_Log = nil
	v.U_Image = nil
	v.U_Deployment = &val
}

func (v *statusUpdate) Image() string {
	if v.U_Image == nil {
		return ""
	}
	return *v.U_Image
}

func (v *statusUpdate) SetImage(val string) {
	v.U_Message = nil
	v.U_Buildkit = nil
	v.U_Error = nil
	v.U_Log = nil
	v.U_Deployment = nil
	v.U_Image = &val
}

type statusData struct {
	Kind *string `cbor:"0,keyasint,omitempty" json:"kind,omitempty"`
	statusUpdate
}

type Status struct {
	data statusData
}

func (v *Status) HasKind() bool {
	return v.data.Kind != nil
}

func (v *Status) Kind() string {
	if v.data.Kind == nil {
		return ""
	}
	return *v.data.Kind
}

func (v *Status) SetKind(kind string) {
	v.data.Kind = &kind
}

func (v *Status) Update() StatusUpdate {
	return &v.data.statusUpdate
}

func (v *Status) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Status) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Status) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deploymentProgressData struct {
	DeploymentId *string `cbor:"0,keyasint,omitempty" json:"deployment_id,omitempty"`
	Phase        *string `cbor:"1,keyasint,omitempty" json:"phase,omitempty"`
}

type DeploymentProgress struct {
	data deploymentProgressData
}

func (v *DeploymentProgress) HasDeploymentId() bool {
	return v.data.DeploymentId != nil
}

func (v *DeploymentProgress) DeploymentId() string {
	if v.data.DeploymentId == nil {
		return ""
	}
	return *v.data.DeploymentId
}

func (v *DeploymentProgress) SetDeploymentId(deployment_id string) {
	v.data.DeploymentId = &deployment_id
}

func (v *DeploymentProgress) HasPhase() bool {
	return v.data.Phase != nil
}

func (v *DeploymentProgress) Phase() string {
	if v.data.Phase == nil {
		return ""
	}
	return *v.data.Phase
}

func (v *DeploymentProgress) SetPhase(phase string) {
	v.data.Phase = &phase
}

func (v *DeploymentProgress) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeploymentProgress) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeploymentProgress) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeploymentProgress) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type deployRequestData struct {
	ClusterId *string  `cbor:"0,keyasint,omitempty" json:"cluster_id,omitempty"`
	GitInfo   *GitInfo `cbor:"1,keyasint,omitempty" json:"git_info,omitempty"`
}

type DeployRequest struct {
	data deployRequestData
}

func (v *DeployRequest) HasClusterId() bool {
	return v.data.ClusterId != nil
}

func (v *DeployRequest) ClusterId() string {
	if v.data.ClusterId == nil {
		return ""
	}
	return *v.data.ClusterId
}

func (v *DeployRequest) SetClusterId(cluster_id string) {
	v.data.ClusterId = &cluster_id
}

func (v *DeployRequest) HasGitInfo() bool {
	return v.data.GitInfo != nil
}

func (v *DeployRequest) GitInfo() *GitInfo {
	return v.data.GitInfo
}

func (v *DeployRequest) SetGitInfo(git_info *GitInfo) {
	v.data.GitInfo = git_info
}

func (v *DeployRequest) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DeployRequest) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DeployRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DeployRequest) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type gitInfoData struct {
	Sha               *string             `cbor:"0,keyasint,omitempty" json:"sha,omitempty"`
	Branch            *string             `cbor:"1,keyasint,omitempty" json:"branch,omitempty"`
	Repository        *string             `cbor:"2,keyasint,omitempty" json:"repository,omitempty"`
	IsDirty           *bool               `cbor:"3,keyasint,omitempty" json:"is_dirty,omitempty"`
	WorkingTreeHash   *string             `cbor:"4,keyasint,omitempty" json:"working_tree_hash,omitempty"`
	CommitMessage     *string             `cbor:"5,keyasint,omitempty" json:"commit_message,omitempty"`
	CommitAuthorName  *string             `cbor:"6,keyasint,omitempty" json:"commit_author_name,omitempty"`
	CommitAuthorEmail *string             `cbor:"7,keyasint,omitempty" json:"commit_author_email,omitempty"`
	CommitTimestamp   *standard.Timestamp `cbor:"8,keyasint,omitempty" json:"commit_timestamp,omitempty"`
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

type logEntryData struct {
	Level  *string      `cbor:"0,keyasint,omitempty" json:"level,omitempty"`
	Text   *string      `cbor:"1,keyasint,omitempty" json:"text,omitempty"`
	Fields *[]*LogField `cbor:"2,keyasint,omitempty" json:"fields,omitempty"`
}

type LogEntry struct {
	data logEntryData
}

func (v *LogEntry) HasLevel() bool {
	return v.data.Level != nil
}

func (v *LogEntry) Level() string {
	if v.data.Level == nil {
		return ""
	}
	return *v.data.Level
}

func (v *LogEntry) SetLevel(level string) {
	v.data.Level = &level
}

func (v *LogEntry) HasText() bool {
	return v.data.Text != nil
}

func (v *LogEntry) Text() string {
	if v.data.Text == nil {
		return ""
	}
	return *v.data.Text
}

func (v *LogEntry) SetText(text string) {
	v.data.Text = &text
}

func (v *LogEntry) HasFields() bool {
	return v.data.Fields != nil
}

func (v *LogEntry) Fields() []*LogField {
	if v.data.Fields == nil {
		return nil
	}
	return *v.data.Fields
}

func (v *LogEntry) SetFields(fields []*LogField) {
	x := slices.Clone(fields)
	v.data.Fields = &x
}

func (v *LogEntry) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogEntry) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogEntry) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logFieldData struct {
	Key   *string `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Value *string `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
}

type LogField struct {
	data logFieldData
}

func (v *LogField) HasKey() bool {
	return v.data.Key != nil
}

func (v *LogField) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *LogField) SetKey(key string) {
	v.data.Key = &key
}

func (v *LogField) HasValue() bool {
	return v.data.Value != nil
}

func (v *LogField) Value() string {
	if v.data.Value == nil {
		return ""
	}
	return *v.data.Value
}

func (v *LogField) SetValue(value string) {
	v.data.Value = &value
}

func (v *LogField) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogField) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogField) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogField) UnmarshalJSON(data []byte) error {
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

type serviceInfoData struct {
	Name    *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Command *string `cbor:"1,keyasint,omitempty" json:"command,omitempty"`
	Source  *string `cbor:"2,keyasint,omitempty" json:"source,omitempty"`
}

type ServiceInfo struct {
	data serviceInfoData
}

func (v *ServiceInfo) HasName() bool {
	return v.data.Name != nil
}

func (v *ServiceInfo) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *ServiceInfo) SetName(name string) {
	v.data.Name = &name
}

func (v *ServiceInfo) HasCommand() bool {
	return v.data.Command != nil
}

func (v *ServiceInfo) Command() string {
	if v.data.Command == nil {
		return ""
	}
	return *v.data.Command
}

func (v *ServiceInfo) SetCommand(command string) {
	v.data.Command = &command
}

func (v *ServiceInfo) HasSource() bool {
	return v.data.Source != nil
}

func (v *ServiceInfo) Source() string {
	if v.data.Source == nil {
		return ""
	}
	return *v.data.Source
}

func (v *ServiceInfo) SetSource(source string) {
	v.data.Source = &source
}

func (v *ServiceInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServiceInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServiceInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServiceInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type taskInfoData struct {
	Name     *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Command  *string `cbor:"1,keyasint,omitempty" json:"command,omitempty"`
	Trigger  *string `cbor:"2,keyasint,omitempty" json:"trigger,omitempty"`
	Schedule *string `cbor:"3,keyasint,omitempty" json:"schedule,omitempty"`
}

type TaskInfo struct {
	data taskInfoData
}

func (v *TaskInfo) HasName() bool {
	return v.data.Name != nil
}

func (v *TaskInfo) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *TaskInfo) SetName(name string) {
	v.data.Name = &name
}

func (v *TaskInfo) HasCommand() bool {
	return v.data.Command != nil
}

func (v *TaskInfo) Command() string {
	if v.data.Command == nil {
		return ""
	}
	return *v.data.Command
}

func (v *TaskInfo) SetCommand(command string) {
	v.data.Command = &command
}

func (v *TaskInfo) HasTrigger() bool {
	return v.data.Trigger != nil
}

func (v *TaskInfo) Trigger() string {
	if v.data.Trigger == nil {
		return ""
	}
	return *v.data.Trigger
}

func (v *TaskInfo) SetTrigger(trigger string) {
	v.data.Trigger = &trigger
}

func (v *TaskInfo) HasSchedule() bool {
	return v.data.Schedule != nil
}

func (v *TaskInfo) Schedule() string {
	if v.data.Schedule == nil {
		return ""
	}
	return *v.data.Schedule
}

func (v *TaskInfo) SetSchedule(schedule string) {
	v.data.Schedule = &schedule
}

func (v *TaskInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *TaskInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *TaskInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *TaskInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type detectionEventData struct {
	Kind    *string `cbor:"0,keyasint,omitempty" json:"kind,omitempty"`
	Name    *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	Message *string `cbor:"2,keyasint,omitempty" json:"message,omitempty"`
}

type DetectionEvent struct {
	data detectionEventData
}

func (v *DetectionEvent) HasKind() bool {
	return v.data.Kind != nil
}

func (v *DetectionEvent) Kind() string {
	if v.data.Kind == nil {
		return ""
	}
	return *v.data.Kind
}

func (v *DetectionEvent) SetKind(kind string) {
	v.data.Kind = &kind
}

func (v *DetectionEvent) HasName() bool {
	return v.data.Name != nil
}

func (v *DetectionEvent) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *DetectionEvent) SetName(name string) {
	v.data.Name = &name
}

func (v *DetectionEvent) HasMessage() bool {
	return v.data.Message != nil
}

func (v *DetectionEvent) Message() string {
	if v.data.Message == nil {
		return ""
	}
	return *v.data.Message
}

func (v *DetectionEvent) SetMessage(message string) {
	v.data.Message = &message
}

func (v *DetectionEvent) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DetectionEvent) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DetectionEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DetectionEvent) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type analysisResultData struct {
	Stack           *string           `cbor:"0,keyasint,omitempty" json:"stack,omitempty"`
	Services        *[]ServiceInfo    `cbor:"1,keyasint,omitempty" json:"services,omitempty"`
	WorkingDir      *string           `cbor:"2,keyasint,omitempty" json:"working_dir,omitempty"`
	Entrypoint      *string           `cbor:"3,keyasint,omitempty" json:"entrypoint,omitempty"`
	AppName         *string           `cbor:"4,keyasint,omitempty" json:"app_name,omitempty"`
	BuildDockerfile *string           `cbor:"5,keyasint,omitempty" json:"build_dockerfile,omitempty"`
	EnvVars         *[]string         `cbor:"6,keyasint,omitempty" json:"env_vars,omitempty"`
	Events          *[]DetectionEvent `cbor:"7,keyasint,omitempty" json:"events,omitempty"`
	Tasks           *[]TaskInfo       `cbor:"8,keyasint,omitempty" json:"tasks,omitempty"`
	SourceKind      *string           `cbor:"9,keyasint,omitempty" json:"source_kind,omitempty"`
	SourceValue     *string           `cbor:"10,keyasint,omitempty" json:"source_value,omitempty"`
}

type AnalysisResult struct {
	data analysisResultData
}

func (v *AnalysisResult) HasStack() bool {
	return v.data.Stack != nil
}

func (v *AnalysisResult) Stack() string {
	if v.data.Stack == nil {
		return ""
	}
	return *v.data.Stack
}

func (v *AnalysisResult) SetStack(stack string) {
	v.data.Stack = &stack
}

func (v *AnalysisResult) HasServices() bool {
	return v.data.Services != nil
}

func (v *AnalysisResult) Services() *[]ServiceInfo {
	return v.data.Services
}

func (v *AnalysisResult) SetServices(services *[]ServiceInfo) {
	v.data.Services = services
}

func (v *AnalysisResult) HasWorkingDir() bool {
	return v.data.WorkingDir != nil
}

func (v *AnalysisResult) WorkingDir() string {
	if v.data.WorkingDir == nil {
		return ""
	}
	return *v.data.WorkingDir
}

func (v *AnalysisResult) SetWorkingDir(working_dir string) {
	v.data.WorkingDir = &working_dir
}

func (v *AnalysisResult) HasEntrypoint() bool {
	return v.data.Entrypoint != nil
}

func (v *AnalysisResult) Entrypoint() string {
	if v.data.Entrypoint == nil {
		return ""
	}
	return *v.data.Entrypoint
}

func (v *AnalysisResult) SetEntrypoint(entrypoint string) {
	v.data.Entrypoint = &entrypoint
}

func (v *AnalysisResult) HasAppName() bool {
	return v.data.AppName != nil
}

func (v *AnalysisResult) AppName() string {
	if v.data.AppName == nil {
		return ""
	}
	return *v.data.AppName
}

func (v *AnalysisResult) SetAppName(app_name string) {
	v.data.AppName = &app_name
}

func (v *AnalysisResult) HasBuildDockerfile() bool {
	return v.data.BuildDockerfile != nil
}

func (v *AnalysisResult) BuildDockerfile() string {
	if v.data.BuildDockerfile == nil {
		return ""
	}
	return *v.data.BuildDockerfile
}

func (v *AnalysisResult) SetBuildDockerfile(build_dockerfile string) {
	v.data.BuildDockerfile = &build_dockerfile
}

func (v *AnalysisResult) HasEnvVars() bool {
	return v.data.EnvVars != nil
}

func (v *AnalysisResult) EnvVars() *[]string {
	return v.data.EnvVars
}

func (v *AnalysisResult) SetEnvVars(env_vars *[]string) {
	v.data.EnvVars = env_vars
}

func (v *AnalysisResult) HasEvents() bool {
	return v.data.Events != nil
}

func (v *AnalysisResult) Events() *[]DetectionEvent {
	return v.data.Events
}

func (v *AnalysisResult) SetEvents(events *[]DetectionEvent) {
	v.data.Events = events
}

func (v *AnalysisResult) HasTasks() bool {
	return v.data.Tasks != nil
}

func (v *AnalysisResult) Tasks() *[]TaskInfo {
	return v.data.Tasks
}

func (v *AnalysisResult) SetTasks(tasks *[]TaskInfo) {
	v.data.Tasks = tasks
}

func (v *AnalysisResult) HasSourceKind() bool {
	return v.data.SourceKind != nil
}

func (v *AnalysisResult) SourceKind() string {
	if v.data.SourceKind == nil {
		return ""
	}
	return *v.data.SourceKind
}

func (v *AnalysisResult) SetSourceKind(source_kind string) {
	v.data.SourceKind = &source_kind
}

func (v *AnalysisResult) HasSourceValue() bool {
	return v.data.SourceValue != nil
}

func (v *AnalysisResult) SourceValue() string {
	if v.data.SourceValue == nil {
		return ""
	}
	return *v.data.SourceValue
}

func (v *AnalysisResult) SetSourceValue(source_value string) {
	v.data.SourceValue = &source_value
}

func (v *AnalysisResult) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AnalysisResult) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AnalysisResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AnalysisResult) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type environmentVariableData struct {
	Key       *string `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Value     *string `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
	Sensitive *bool   `cbor:"2,keyasint,omitempty" json:"sensitive,omitempty"`
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

type fileManifestEntryData struct {
	Path *string `cbor:"0,keyasint,omitempty" json:"path,omitempty"`
	Hash *string `cbor:"1,keyasint,omitempty" json:"hash,omitempty"`
	Size *int64  `cbor:"2,keyasint,omitempty" json:"size,omitempty"`
	Mode *int32  `cbor:"3,keyasint,omitempty" json:"mode,omitempty"`
}

type FileManifestEntry struct {
	data fileManifestEntryData
}

func (v *FileManifestEntry) HasPath() bool {
	return v.data.Path != nil
}

func (v *FileManifestEntry) Path() string {
	if v.data.Path == nil {
		return ""
	}
	return *v.data.Path
}

func (v *FileManifestEntry) SetPath(path string) {
	v.data.Path = &path
}

func (v *FileManifestEntry) HasHash() bool {
	return v.data.Hash != nil
}

func (v *FileManifestEntry) Hash() string {
	if v.data.Hash == nil {
		return ""
	}
	return *v.data.Hash
}

func (v *FileManifestEntry) SetHash(hash string) {
	v.data.Hash = &hash
}

func (v *FileManifestEntry) HasSize() bool {
	return v.data.Size != nil
}

func (v *FileManifestEntry) Size() int64 {
	if v.data.Size == nil {
		return 0
	}
	return *v.data.Size
}

func (v *FileManifestEntry) SetSize(size int64) {
	v.data.Size = &size
}

func (v *FileManifestEntry) HasMode() bool {
	return v.data.Mode != nil
}

func (v *FileManifestEntry) Mode() int32 {
	if v.data.Mode == nil {
		return 0
	}
	return *v.data.Mode
}

func (v *FileManifestEntry) SetMode(mode int32) {
	v.data.Mode = &mode
}

func (v *FileManifestEntry) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *FileManifestEntry) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *FileManifestEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *FileManifestEntry) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type prepareUploadResultData struct {
	SessionId   *string   `cbor:"0,keyasint,omitempty" json:"session_id,omitempty"`
	NeededPaths *[]string `cbor:"1,keyasint,omitempty" json:"needed_paths,omitempty"`
	CachedCount *int32    `cbor:"2,keyasint,omitempty" json:"cached_count,omitempty"`
}

type PrepareUploadResult struct {
	data prepareUploadResultData
}

func (v *PrepareUploadResult) HasSessionId() bool {
	return v.data.SessionId != nil
}

func (v *PrepareUploadResult) SessionId() string {
	if v.data.SessionId == nil {
		return ""
	}
	return *v.data.SessionId
}

func (v *PrepareUploadResult) SetSessionId(session_id string) {
	v.data.SessionId = &session_id
}

func (v *PrepareUploadResult) HasNeededPaths() bool {
	return v.data.NeededPaths != nil
}

func (v *PrepareUploadResult) NeededPaths() *[]string {
	return v.data.NeededPaths
}

func (v *PrepareUploadResult) SetNeededPaths(needed_paths *[]string) {
	v.data.NeededPaths = needed_paths
}

func (v *PrepareUploadResult) HasCachedCount() bool {
	return v.data.CachedCount != nil
}

func (v *PrepareUploadResult) CachedCount() int32 {
	if v.data.CachedCount == nil {
		return 0
	}
	return *v.data.CachedCount
}

func (v *PrepareUploadResult) SetCachedCount(cached_count int32) {
	v.data.CachedCount = &cached_count
}

func (v *PrepareUploadResult) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *PrepareUploadResult) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *PrepareUploadResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *PrepareUploadResult) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type streamRecvArgsData struct {
	Count *int32 `cbor:"0,keyasint,omitempty" json:"count,omitempty"`
}

type StreamRecvArgs struct {
	call rpc.Call
	data streamRecvArgsData
}

func (v *StreamRecvArgs) HasCount() bool {
	return v.data.Count != nil
}

func (v *StreamRecvArgs) Count() int32 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *StreamRecvArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *StreamRecvArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *StreamRecvArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *StreamRecvArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type streamRecvResultsData struct {
	Data *[]byte `cbor:"0,keyasint,omitempty" json:"data,omitempty"`
}

type StreamRecvResults struct {
	call rpc.Call
	data streamRecvResultsData
}

func (v *StreamRecvResults) SetData(data []byte) {
	x := slices.Clone(data)
	v.data.Data = &x
}

func (v *StreamRecvResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *StreamRecvResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *StreamRecvResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *StreamRecvResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type StreamRecv struct {
	rpc.Call
	args    StreamRecvArgs
	results StreamRecvResults
}

func (t *StreamRecv) Args() *StreamRecvArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *StreamRecv) Results() *StreamRecvResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Stream interface {
	Recv(ctx context.Context, state *StreamRecv) error
}

type reexportStream struct {
	client rpc.Client
}

func (reexportStream) Recv(ctx context.Context, state *StreamRecv) error {
	panic("not implemented")
}

func (t reexportStream) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptStream(t Stream) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "recv",
			InterfaceName: "Stream",
			Index:         0,
			Public:        false,
			Params:        []string{"count"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Recv(ctx, &StreamRecv{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type StreamClient struct {
	rpc.Client
}

func NewStreamClient(client rpc.Client) *StreamClient {
	return &StreamClient{Client: client}
}

func (c StreamClient) Export() Stream {
	return reexportStream{client: c.Client}
}

type StreamClientRecvResults struct {
	client rpc.Client
	data   streamRecvResultsData
}

func (v *StreamClientRecvResults) HasData() bool {
	return v.data.Data != nil
}

func (v *StreamClientRecvResults) Data() []byte {
	if v.data.Data == nil {
		return nil
	}
	return *v.data.Data
}

func (v StreamClient) Recv(ctx context.Context, count int32) (*StreamClientRecvResults, error) {
	args := StreamRecvArgs{}
	args.data.Count = &count

	var ret streamRecvResultsData

	err := v.Call(ctx, "recv", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &StreamClientRecvResults{client: v.Client, data: ret}, nil
}

type builderBuildFromTarArgsData struct {
	Application    *string                 `cbor:"0,keyasint,omitempty" json:"application,omitempty"`
	Tardata        *rpc.Capability         `cbor:"1,keyasint,omitempty" json:"tardata,omitempty"`
	Status         *rpc.Capability         `cbor:"2,keyasint,omitempty" json:"status,omitempty"`
	EnvVars        *[]*EnvironmentVariable `cbor:"3,keyasint,omitempty" json:"envVars,omitempty"`
	EphemeralLabel *string                 `cbor:"4,keyasint,omitempty" json:"ephemeral_label,omitempty"`
	EphemeralTtl   *string                 `cbor:"5,keyasint,omitempty" json:"ephemeral_ttl,omitempty"`
	Deployment     **DeployRequest         `cbor:"6,keyasint,omitempty" json:"deployment,omitempty"`
}

type BuilderBuildFromTarArgs struct {
	call rpc.Call
	data builderBuildFromTarArgsData
}

func (v *BuilderBuildFromTarArgs) HasApplication() bool {
	return v.data.Application != nil
}

func (v *BuilderBuildFromTarArgs) Application() string {
	if v.data.Application == nil {
		return ""
	}
	return *v.data.Application
}

func (v *BuilderBuildFromTarArgs) HasTardata() bool {
	return v.data.Tardata != nil
}

func (v *BuilderBuildFromTarArgs) Tardata() *stream.RecvStreamClient[[]byte] {
	if v.data.Tardata == nil {
		return nil
	}
	return &stream.RecvStreamClient[[]byte]{Client: v.call.NewClient(v.data.Tardata)}
}

func (v *BuilderBuildFromTarArgs) HasStatus() bool {
	return v.data.Status != nil
}

func (v *BuilderBuildFromTarArgs) Status() *stream.SendStreamClient[*Status] {
	if v.data.Status == nil {
		return nil
	}
	return &stream.SendStreamClient[*Status]{Client: v.call.NewClient(v.data.Status)}
}

func (v *BuilderBuildFromTarArgs) HasEnvVars() bool {
	return v.data.EnvVars != nil
}

func (v *BuilderBuildFromTarArgs) EnvVars() []*EnvironmentVariable {
	if v.data.EnvVars == nil {
		return nil
	}
	return *v.data.EnvVars
}

func (v *BuilderBuildFromTarArgs) HasEphemeralLabel() bool {
	return v.data.EphemeralLabel != nil
}

func (v *BuilderBuildFromTarArgs) EphemeralLabel() string {
	if v.data.EphemeralLabel == nil {
		return ""
	}
	return *v.data.EphemeralLabel
}

func (v *BuilderBuildFromTarArgs) HasEphemeralTtl() bool {
	return v.data.EphemeralTtl != nil
}

func (v *BuilderBuildFromTarArgs) EphemeralTtl() string {
	if v.data.EphemeralTtl == nil {
		return ""
	}
	return *v.data.EphemeralTtl
}

func (v *BuilderBuildFromTarArgs) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *BuilderBuildFromTarArgs) Deployment() *DeployRequest {
	if v.data.Deployment == nil {
		return nil
	}
	return *v.data.Deployment
}

func (v *BuilderBuildFromTarArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderBuildFromTarArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderBuildFromTarArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderBuildFromTarArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type builderBuildFromTarResultsData struct {
	Version        *string      `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	AccessInfo     **AccessInfo `cbor:"1,keyasint,omitempty" json:"access_info,omitempty"`
	VersionShortId *string      `cbor:"2,keyasint,omitempty" json:"version_short_id,omitempty"`
}

type BuilderBuildFromTarResults struct {
	call rpc.Call
	data builderBuildFromTarResultsData
}

func (v *BuilderBuildFromTarResults) SetVersion(version string) {
	v.data.Version = &version
}

func (v *BuilderBuildFromTarResults) SetAccessInfo(access_info **AccessInfo) {
	v.data.AccessInfo = access_info
}

func (v *BuilderBuildFromTarResults) SetVersionShortId(version_short_id string) {
	v.data.VersionShortId = &version_short_id
}

func (v *BuilderBuildFromTarResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderBuildFromTarResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderBuildFromTarResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderBuildFromTarResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type builderAnalyzeAppArgsData struct {
	Tardata *rpc.Capability `cbor:"0,keyasint,omitempty" json:"tardata,omitempty"`
}

type BuilderAnalyzeAppArgs struct {
	call rpc.Call
	data builderAnalyzeAppArgsData
}

func (v *BuilderAnalyzeAppArgs) HasTardata() bool {
	return v.data.Tardata != nil
}

func (v *BuilderAnalyzeAppArgs) Tardata() *stream.RecvStreamClient[[]byte] {
	if v.data.Tardata == nil {
		return nil
	}
	return &stream.RecvStreamClient[[]byte]{Client: v.call.NewClient(v.data.Tardata)}
}

func (v *BuilderAnalyzeAppArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderAnalyzeAppArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderAnalyzeAppArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderAnalyzeAppArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type builderAnalyzeAppResultsData struct {
	Result **AnalysisResult `cbor:"0,keyasint,omitempty" json:"result,omitempty"`
}

type BuilderAnalyzeAppResults struct {
	call rpc.Call
	data builderAnalyzeAppResultsData
}

func (v *BuilderAnalyzeAppResults) SetResult(result **AnalysisResult) {
	v.data.Result = result
}

func (v *BuilderAnalyzeAppResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderAnalyzeAppResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderAnalyzeAppResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderAnalyzeAppResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type builderPrepareUploadArgsData struct {
	Application *string               `cbor:"0,keyasint,omitempty" json:"application,omitempty"`
	Manifest    *[]*FileManifestEntry `cbor:"1,keyasint,omitempty" json:"manifest,omitempty"`
}

type BuilderPrepareUploadArgs struct {
	call rpc.Call
	data builderPrepareUploadArgsData
}

func (v *BuilderPrepareUploadArgs) HasApplication() bool {
	return v.data.Application != nil
}

func (v *BuilderPrepareUploadArgs) Application() string {
	if v.data.Application == nil {
		return ""
	}
	return *v.data.Application
}

func (v *BuilderPrepareUploadArgs) HasManifest() bool {
	return v.data.Manifest != nil
}

func (v *BuilderPrepareUploadArgs) Manifest() []*FileManifestEntry {
	if v.data.Manifest == nil {
		return nil
	}
	return *v.data.Manifest
}

func (v *BuilderPrepareUploadArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderPrepareUploadArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderPrepareUploadArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderPrepareUploadArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type builderPrepareUploadResultsData struct {
	Result **PrepareUploadResult `cbor:"0,keyasint,omitempty" json:"result,omitempty"`
}

type BuilderPrepareUploadResults struct {
	call rpc.Call
	data builderPrepareUploadResultsData
}

func (v *BuilderPrepareUploadResults) SetResult(result **PrepareUploadResult) {
	v.data.Result = result
}

func (v *BuilderPrepareUploadResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderPrepareUploadResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderPrepareUploadResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderPrepareUploadResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type builderBuildFromPreparedArgsData struct {
	SessionId      *string                 `cbor:"0,keyasint,omitempty" json:"session_id,omitempty"`
	Tardata        *rpc.Capability         `cbor:"1,keyasint,omitempty" json:"tardata,omitempty"`
	Status         *rpc.Capability         `cbor:"2,keyasint,omitempty" json:"status,omitempty"`
	EnvVars        *[]*EnvironmentVariable `cbor:"3,keyasint,omitempty" json:"envVars,omitempty"`
	EphemeralLabel *string                 `cbor:"4,keyasint,omitempty" json:"ephemeral_label,omitempty"`
	EphemeralTtl   *string                 `cbor:"5,keyasint,omitempty" json:"ephemeral_ttl,omitempty"`
	Deployment     **DeployRequest         `cbor:"6,keyasint,omitempty" json:"deployment,omitempty"`
}

type BuilderBuildFromPreparedArgs struct {
	call rpc.Call
	data builderBuildFromPreparedArgsData
}

func (v *BuilderBuildFromPreparedArgs) HasSessionId() bool {
	return v.data.SessionId != nil
}

func (v *BuilderBuildFromPreparedArgs) SessionId() string {
	if v.data.SessionId == nil {
		return ""
	}
	return *v.data.SessionId
}

func (v *BuilderBuildFromPreparedArgs) HasTardata() bool {
	return v.data.Tardata != nil
}

func (v *BuilderBuildFromPreparedArgs) Tardata() *stream.RecvStreamClient[[]byte] {
	if v.data.Tardata == nil {
		return nil
	}
	return &stream.RecvStreamClient[[]byte]{Client: v.call.NewClient(v.data.Tardata)}
}

func (v *BuilderBuildFromPreparedArgs) HasStatus() bool {
	return v.data.Status != nil
}

func (v *BuilderBuildFromPreparedArgs) Status() *stream.SendStreamClient[*Status] {
	if v.data.Status == nil {
		return nil
	}
	return &stream.SendStreamClient[*Status]{Client: v.call.NewClient(v.data.Status)}
}

func (v *BuilderBuildFromPreparedArgs) HasEnvVars() bool {
	return v.data.EnvVars != nil
}

func (v *BuilderBuildFromPreparedArgs) EnvVars() []*EnvironmentVariable {
	if v.data.EnvVars == nil {
		return nil
	}
	return *v.data.EnvVars
}

func (v *BuilderBuildFromPreparedArgs) HasEphemeralLabel() bool {
	return v.data.EphemeralLabel != nil
}

func (v *BuilderBuildFromPreparedArgs) EphemeralLabel() string {
	if v.data.EphemeralLabel == nil {
		return ""
	}
	return *v.data.EphemeralLabel
}

func (v *BuilderBuildFromPreparedArgs) HasEphemeralTtl() bool {
	return v.data.EphemeralTtl != nil
}

func (v *BuilderBuildFromPreparedArgs) EphemeralTtl() string {
	if v.data.EphemeralTtl == nil {
		return ""
	}
	return *v.data.EphemeralTtl
}

func (v *BuilderBuildFromPreparedArgs) HasDeployment() bool {
	return v.data.Deployment != nil
}

func (v *BuilderBuildFromPreparedArgs) Deployment() *DeployRequest {
	if v.data.Deployment == nil {
		return nil
	}
	return *v.data.Deployment
}

func (v *BuilderBuildFromPreparedArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderBuildFromPreparedArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderBuildFromPreparedArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderBuildFromPreparedArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type builderBuildFromPreparedResultsData struct {
	Version        *string      `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	AccessInfo     **AccessInfo `cbor:"1,keyasint,omitempty" json:"access_info,omitempty"`
	VersionShortId *string      `cbor:"2,keyasint,omitempty" json:"version_short_id,omitempty"`
}

type BuilderBuildFromPreparedResults struct {
	call rpc.Call
	data builderBuildFromPreparedResultsData
}

func (v *BuilderBuildFromPreparedResults) SetVersion(version string) {
	v.data.Version = &version
}

func (v *BuilderBuildFromPreparedResults) SetAccessInfo(access_info **AccessInfo) {
	v.data.AccessInfo = access_info
}

func (v *BuilderBuildFromPreparedResults) SetVersionShortId(version_short_id string) {
	v.data.VersionShortId = &version_short_id
}

func (v *BuilderBuildFromPreparedResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BuilderBuildFromPreparedResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BuilderBuildFromPreparedResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BuilderBuildFromPreparedResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type BuilderBuildFromTar struct {
	rpc.Call
	args    BuilderBuildFromTarArgs
	results BuilderBuildFromTarResults
}

func (t *BuilderBuildFromTar) Args() *BuilderBuildFromTarArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *BuilderBuildFromTar) Results() *BuilderBuildFromTarResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type BuilderAnalyzeApp struct {
	rpc.Call
	args    BuilderAnalyzeAppArgs
	results BuilderAnalyzeAppResults
}

func (t *BuilderAnalyzeApp) Args() *BuilderAnalyzeAppArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *BuilderAnalyzeApp) Results() *BuilderAnalyzeAppResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type BuilderPrepareUpload struct {
	rpc.Call
	args    BuilderPrepareUploadArgs
	results BuilderPrepareUploadResults
}

func (t *BuilderPrepareUpload) Args() *BuilderPrepareUploadArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *BuilderPrepareUpload) Results() *BuilderPrepareUploadResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type BuilderBuildFromPrepared struct {
	rpc.Call
	args    BuilderBuildFromPreparedArgs
	results BuilderBuildFromPreparedResults
}

func (t *BuilderBuildFromPrepared) Args() *BuilderBuildFromPreparedArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *BuilderBuildFromPrepared) Results() *BuilderBuildFromPreparedResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Builder interface {
	BuildFromTar(ctx context.Context, state *BuilderBuildFromTar) error
	AnalyzeApp(ctx context.Context, state *BuilderAnalyzeApp) error
	PrepareUpload(ctx context.Context, state *BuilderPrepareUpload) error
	BuildFromPrepared(ctx context.Context, state *BuilderBuildFromPrepared) error
}

type reexportBuilder struct {
	client rpc.Client
}

func (reexportBuilder) BuildFromTar(ctx context.Context, state *BuilderBuildFromTar) error {
	panic("not implemented")
}

func (reexportBuilder) AnalyzeApp(ctx context.Context, state *BuilderAnalyzeApp) error {
	panic("not implemented")
}

func (reexportBuilder) PrepareUpload(ctx context.Context, state *BuilderPrepareUpload) error {
	panic("not implemented")
}

func (reexportBuilder) BuildFromPrepared(ctx context.Context, state *BuilderBuildFromPrepared) error {
	panic("not implemented")
}

func (t reexportBuilder) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptBuilder(t Builder) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "buildFromTar",
			InterfaceName: "Builder",
			Index:         0,
			Public:        false,
			Params:        []string{"application", "tardata", "status", "envVars", "ephemeral_label", "ephemeral_ttl", "deployment"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.BuildFromTar(ctx, &BuilderBuildFromTar{Call: call})
			},
		},
		{
			Name:          "analyzeApp",
			InterfaceName: "Builder",
			Index:         0,
			Public:        false,
			Params:        []string{"tardata"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.AnalyzeApp(ctx, &BuilderAnalyzeApp{Call: call})
			},
		},
		{
			Name:          "prepareUpload",
			InterfaceName: "Builder",
			Index:         0,
			Public:        false,
			Params:        []string{"application", "manifest"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.PrepareUpload(ctx, &BuilderPrepareUpload{Call: call})
			},
		},
		{
			Name:          "buildFromPrepared",
			InterfaceName: "Builder",
			Index:         0,
			Public:        false,
			Params:        []string{"session_id", "tardata", "status", "envVars", "ephemeral_label", "ephemeral_ttl", "deployment"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.BuildFromPrepared(ctx, &BuilderBuildFromPrepared{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type BuilderClient struct {
	rpc.Client
}

func NewBuilderClient(client rpc.Client) *BuilderClient {
	return &BuilderClient{Client: client}
}

func (c BuilderClient) Export() Builder {
	return reexportBuilder{client: c.Client}
}

type BuilderClientBuildFromTarResults struct {
	client rpc.Client
	data   builderBuildFromTarResultsData
}

func (v *BuilderClientBuildFromTarResults) HasVersion() bool {
	return v.data.Version != nil
}

func (v *BuilderClientBuildFromTarResults) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *BuilderClientBuildFromTarResults) HasAccessInfo() bool {
	return v.data.AccessInfo != nil
}

func (v *BuilderClientBuildFromTarResults) AccessInfo() *AccessInfo {
	if v.data.AccessInfo == nil {
		return nil
	}
	return *v.data.AccessInfo
}

func (v *BuilderClientBuildFromTarResults) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *BuilderClientBuildFromTarResults) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v BuilderClient) BuildFromTar(ctx context.Context, application string, tardata stream.RecvStream[[]byte], status stream.SendStream[*Status], envVars []*EnvironmentVariable, ephemeral_label string, ephemeral_ttl string, deployment *DeployRequest) (*BuilderClientBuildFromTarResults, error) {
	args := BuilderBuildFromTarArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Application = &application
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptRecvStream[[]byte](tardata), tardata)
		args.data.Tardata = c
		caps[oid] = ic
	}
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[*Status](status), status)
		args.data.Status = c
		caps[oid] = ic
	}
	args.data.EnvVars = &envVars
	args.data.EphemeralLabel = &ephemeral_label
	args.data.EphemeralTtl = &ephemeral_ttl
	args.data.Deployment = &deployment

	var ret builderBuildFromTarResultsData

	err := v.CallWithCaps(ctx, "buildFromTar", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &BuilderClientBuildFromTarResults{client: v.Client, data: ret}, nil
}

type BuilderClientAnalyzeAppResults struct {
	client rpc.Client
	data   builderAnalyzeAppResultsData
}

func (v *BuilderClientAnalyzeAppResults) HasResult() bool {
	return v.data.Result != nil
}

func (v *BuilderClientAnalyzeAppResults) Result() *AnalysisResult {
	if v.data.Result == nil {
		return nil
	}
	return *v.data.Result
}

func (v BuilderClient) AnalyzeApp(ctx context.Context, tardata stream.RecvStream[[]byte]) (*BuilderClientAnalyzeAppResults, error) {
	args := BuilderAnalyzeAppArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptRecvStream[[]byte](tardata), tardata)
		args.data.Tardata = c
		caps[oid] = ic
	}

	var ret builderAnalyzeAppResultsData

	err := v.CallWithCaps(ctx, "analyzeApp", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &BuilderClientAnalyzeAppResults{client: v.Client, data: ret}, nil
}

type BuilderClientPrepareUploadResults struct {
	client rpc.Client
	data   builderPrepareUploadResultsData
}

func (v *BuilderClientPrepareUploadResults) HasResult() bool {
	return v.data.Result != nil
}

func (v *BuilderClientPrepareUploadResults) Result() *PrepareUploadResult {
	if v.data.Result == nil {
		return nil
	}
	return *v.data.Result
}

func (v BuilderClient) PrepareUpload(ctx context.Context, application string, manifest []*FileManifestEntry) (*BuilderClientPrepareUploadResults, error) {
	args := BuilderPrepareUploadArgs{}
	args.data.Application = &application
	args.data.Manifest = &manifest

	var ret builderPrepareUploadResultsData

	err := v.Call(ctx, "prepareUpload", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &BuilderClientPrepareUploadResults{client: v.Client, data: ret}, nil
}

type BuilderClientBuildFromPreparedResults struct {
	client rpc.Client
	data   builderBuildFromPreparedResultsData
}

func (v *BuilderClientBuildFromPreparedResults) HasVersion() bool {
	return v.data.Version != nil
}

func (v *BuilderClientBuildFromPreparedResults) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *BuilderClientBuildFromPreparedResults) HasAccessInfo() bool {
	return v.data.AccessInfo != nil
}

func (v *BuilderClientBuildFromPreparedResults) AccessInfo() *AccessInfo {
	if v.data.AccessInfo == nil {
		return nil
	}
	return *v.data.AccessInfo
}

func (v *BuilderClientBuildFromPreparedResults) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *BuilderClientBuildFromPreparedResults) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v BuilderClient) BuildFromPrepared(ctx context.Context, session_id string, tardata stream.RecvStream[[]byte], status stream.SendStream[*Status], envVars []*EnvironmentVariable, ephemeral_label string, ephemeral_ttl string, deployment *DeployRequest) (*BuilderClientBuildFromPreparedResults, error) {
	args := BuilderBuildFromPreparedArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.SessionId = &session_id
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptRecvStream[[]byte](tardata), tardata)
		args.data.Tardata = c
		caps[oid] = ic
	}
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[*Status](status), status)
		args.data.Status = c
		caps[oid] = ic
	}
	args.data.EnvVars = &envVars
	args.data.EphemeralLabel = &ephemeral_label
	args.data.EphemeralTtl = &ephemeral_ttl
	args.data.Deployment = &deployment

	var ret builderBuildFromPreparedResultsData

	err := v.CallWithCaps(ctx, "buildFromPrepared", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &BuilderClientBuildFromPreparedResults{client: v.Client, data: ret}, nil
}
