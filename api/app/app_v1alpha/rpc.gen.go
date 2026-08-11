package app_v1alpha

import (
	"context"
	"encoding/json"
	"maps"
	"slices"

	"github.com/fxamacker/cbor/v2"
	rpc "miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

type logTargetData struct {
	App     *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Sandbox *string `cbor:"1,keyasint,omitempty" json:"sandbox,omitempty"`
	System  *bool   `cbor:"2,keyasint,omitempty" json:"system,omitempty"`
}

type LogTarget struct {
	data logTargetData
}

func (v *LogTarget) HasApp() bool {
	return v.data.App != nil
}

func (v *LogTarget) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *LogTarget) SetApp(app string) {
	v.data.App = &app
}

func (v *LogTarget) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *LogTarget) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *LogTarget) SetSandbox(sandbox string) {
	v.data.Sandbox = &sandbox
}

func (v *LogTarget) HasSystem() bool {
	return v.data.System != nil
}

func (v *LogTarget) System() bool {
	if v.data.System == nil {
		return false
	}
	return *v.data.System
}

func (v *LogTarget) SetSystem(system bool) {
	v.data.System = &system
}

func (v *LogTarget) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogTarget) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogTarget) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogTarget) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type autoConcurrencyData struct {
	Factor *int32 `cbor:"1,keyasint,omitempty" json:"factor,omitempty"`
}

type AutoConcurrency struct {
	data autoConcurrencyData
}

func (v *AutoConcurrency) HasFactor() bool {
	return v.data.Factor != nil
}

func (v *AutoConcurrency) Factor() int32 {
	if v.data.Factor == nil {
		return 0
	}
	return *v.data.Factor
}

func (v *AutoConcurrency) SetFactor(factor int32) {
	v.data.Factor = &factor
}

func (v *AutoConcurrency) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AutoConcurrency) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AutoConcurrency) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AutoConcurrency) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serviceCommandData struct {
	Service *string `cbor:"0,keyasint,omitempty" json:"service,omitempty"`
	Command *string `cbor:"1,keyasint,omitempty" json:"command,omitempty"`
}

type ServiceCommand struct {
	data serviceCommandData
}

func (v *ServiceCommand) HasService() bool {
	return v.data.Service != nil
}

func (v *ServiceCommand) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *ServiceCommand) SetService(service string) {
	v.data.Service = &service
}

func (v *ServiceCommand) HasCommand() bool {
	return v.data.Command != nil
}

func (v *ServiceCommand) Command() string {
	if v.data.Command == nil {
		return ""
	}
	return *v.data.Command
}

func (v *ServiceCommand) SetCommand(command string) {
	v.data.Command = &command
}

func (v *ServiceCommand) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServiceCommand) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServiceCommand) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServiceCommand) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type serviceConfigData struct {
	Service         *string        `cbor:"0,keyasint,omitempty" json:"service,omitempty"`
	ServiceEnv      *[]*NamedValue `cbor:"1,keyasint,omitempty" json:"service_env,omitempty"`
	ConcurrencyMode *string        `cbor:"2,keyasint,omitempty" json:"concurrency_mode,omitempty"`
	NumInstances    *int32         `cbor:"3,keyasint,omitempty" json:"num_instances,omitempty"`
}

type ServiceConfig struct {
	data serviceConfigData
}

func (v *ServiceConfig) HasService() bool {
	return v.data.Service != nil
}

func (v *ServiceConfig) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *ServiceConfig) SetService(service string) {
	v.data.Service = &service
}

func (v *ServiceConfig) HasServiceEnv() bool {
	return v.data.ServiceEnv != nil
}

func (v *ServiceConfig) ServiceEnv() []*NamedValue {
	if v.data.ServiceEnv == nil {
		return nil
	}
	return *v.data.ServiceEnv
}

func (v *ServiceConfig) SetServiceEnv(service_env []*NamedValue) {
	x := slices.Clone(service_env)
	v.data.ServiceEnv = &x
}

func (v *ServiceConfig) HasConcurrencyMode() bool {
	return v.data.ConcurrencyMode != nil
}

func (v *ServiceConfig) ConcurrencyMode() string {
	if v.data.ConcurrencyMode == nil {
		return ""
	}
	return *v.data.ConcurrencyMode
}

func (v *ServiceConfig) SetConcurrencyMode(concurrency_mode string) {
	v.data.ConcurrencyMode = &concurrency_mode
}

func (v *ServiceConfig) HasNumInstances() bool {
	return v.data.NumInstances != nil
}

func (v *ServiceConfig) NumInstances() int32 {
	if v.data.NumInstances == nil {
		return 0
	}
	return *v.data.NumInstances
}

func (v *ServiceConfig) SetNumInstances(num_instances int32) {
	v.data.NumInstances = &num_instances
}

func (v *ServiceConfig) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ServiceConfig) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ServiceConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ServiceConfig) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type configurationData struct {
	EnvVars         *[]*NamedValue     `cbor:"0,keyasint,omitempty" json:"env_vars,omitempty"`
	Concurrency     *int32             `cbor:"1,keyasint,omitempty" json:"concurrency,omitempty"`
	AutoConcurrency *AutoConcurrency   `cbor:"2,keyasint,omitempty" json:"auto_concurrency,omitempty"`
	Commands        *[]*ServiceCommand `cbor:"3,keyasint,omitempty" json:"commands,omitempty"`
	Entrypoint      *string            `cbor:"4,keyasint,omitempty" json:"entrypoint,omitempty"`
	Services        *[]*ServiceConfig  `cbor:"5,keyasint,omitempty" json:"services,omitempty"`
}

type Configuration struct {
	data configurationData
}

func (v *Configuration) HasEnvVars() bool {
	return v.data.EnvVars != nil
}

func (v *Configuration) EnvVars() []*NamedValue {
	if v.data.EnvVars == nil {
		return nil
	}
	return *v.data.EnvVars
}

func (v *Configuration) SetEnvVars(env_vars []*NamedValue) {
	x := slices.Clone(env_vars)
	v.data.EnvVars = &x
}

func (v *Configuration) HasConcurrency() bool {
	return v.data.Concurrency != nil
}

func (v *Configuration) Concurrency() int32 {
	if v.data.Concurrency == nil {
		return 0
	}
	return *v.data.Concurrency
}

func (v *Configuration) SetConcurrency(concurrency int32) {
	v.data.Concurrency = &concurrency
}

func (v *Configuration) HasAutoConcurrency() bool {
	return v.data.AutoConcurrency != nil
}

func (v *Configuration) AutoConcurrency() *AutoConcurrency {
	return v.data.AutoConcurrency
}

func (v *Configuration) SetAutoConcurrency(auto_concurrency *AutoConcurrency) {
	v.data.AutoConcurrency = auto_concurrency
}

func (v *Configuration) HasCommands() bool {
	return v.data.Commands != nil
}

func (v *Configuration) Commands() []*ServiceCommand {
	if v.data.Commands == nil {
		return nil
	}
	return *v.data.Commands
}

func (v *Configuration) SetCommands(commands []*ServiceCommand) {
	x := slices.Clone(commands)
	v.data.Commands = &x
}

func (v *Configuration) HasEntrypoint() bool {
	return v.data.Entrypoint != nil
}

func (v *Configuration) Entrypoint() string {
	if v.data.Entrypoint == nil {
		return ""
	}
	return *v.data.Entrypoint
}

func (v *Configuration) SetEntrypoint(entrypoint string) {
	v.data.Entrypoint = &entrypoint
}

func (v *Configuration) HasServices() bool {
	return v.data.Services != nil
}

func (v *Configuration) Services() []*ServiceConfig {
	if v.data.Services == nil {
		return nil
	}
	return *v.data.Services
}

func (v *Configuration) SetServices(services []*ServiceConfig) {
	x := slices.Clone(services)
	v.data.Services = &x
}

func (v *Configuration) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *Configuration) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *Configuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *Configuration) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type namedValueData struct {
	Key         *string `cbor:"0,keyasint,omitempty" json:"key,omitempty"`
	Value       *string `cbor:"1,keyasint,omitempty" json:"value,omitempty"`
	Sensitive   *bool   `cbor:"2,keyasint,omitempty" json:"sensitive,omitempty"`
	Source      *string `cbor:"3,keyasint,omitempty" json:"source,omitempty"`
	Required    *bool   `cbor:"4,keyasint,omitempty" json:"required,omitempty"`
	Description *string `cbor:"5,keyasint,omitempty" json:"description,omitempty"`
	Backend     *string `cbor:"6,keyasint,omitempty" json:"backend,omitempty"`
}

type NamedValue struct {
	data namedValueData
}

func (v *NamedValue) HasKey() bool {
	return v.data.Key != nil
}

func (v *NamedValue) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *NamedValue) SetKey(key string) {
	v.data.Key = &key
}

func (v *NamedValue) HasValue() bool {
	return v.data.Value != nil
}

func (v *NamedValue) Value() string {
	if v.data.Value == nil {
		return ""
	}
	return *v.data.Value
}

func (v *NamedValue) SetValue(value string) {
	v.data.Value = &value
}

func (v *NamedValue) HasSensitive() bool {
	return v.data.Sensitive != nil
}

func (v *NamedValue) Sensitive() bool {
	if v.data.Sensitive == nil {
		return false
	}
	return *v.data.Sensitive
}

func (v *NamedValue) SetSensitive(sensitive bool) {
	v.data.Sensitive = &sensitive
}

func (v *NamedValue) HasSource() bool {
	return v.data.Source != nil
}

func (v *NamedValue) Source() string {
	if v.data.Source == nil {
		return ""
	}
	return *v.data.Source
}

func (v *NamedValue) SetSource(source string) {
	v.data.Source = &source
}

func (v *NamedValue) HasRequired() bool {
	return v.data.Required != nil
}

func (v *NamedValue) Required() bool {
	if v.data.Required == nil {
		return false
	}
	return *v.data.Required
}

func (v *NamedValue) SetRequired(required bool) {
	v.data.Required = &required
}

func (v *NamedValue) HasDescription() bool {
	return v.data.Description != nil
}

func (v *NamedValue) Description() string {
	if v.data.Description == nil {
		return ""
	}
	return *v.data.Description
}

func (v *NamedValue) SetDescription(description string) {
	v.data.Description = &description
}

func (v *NamedValue) HasBackend() bool {
	return v.data.Backend != nil
}

func (v *NamedValue) Backend() string {
	if v.data.Backend == nil {
		return ""
	}
	return *v.data.Backend
}

func (v *NamedValue) SetBackend(backend string) {
	v.data.Backend = &backend
}

func (v *NamedValue) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *NamedValue) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *NamedValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *NamedValue) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type versionInfoData struct {
	Version   *string             `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	CreatedAt *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"created_at,omitempty"`
	ShortId   *string             `cbor:"2,keyasint,omitempty" json:"short_id,omitempty"`
}

type VersionInfo struct {
	data versionInfoData
}

func (v *VersionInfo) HasVersion() bool {
	return v.data.Version != nil
}

func (v *VersionInfo) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *VersionInfo) SetVersion(version string) {
	v.data.Version = &version
}

func (v *VersionInfo) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *VersionInfo) CreatedAt() *standard.Timestamp {
	return v.data.CreatedAt
}

func (v *VersionInfo) SetCreatedAt(created_at *standard.Timestamp) {
	v.data.CreatedAt = created_at
}

func (v *VersionInfo) HasShortId() bool {
	return v.data.ShortId != nil
}

func (v *VersionInfo) ShortId() string {
	if v.data.ShortId == nil {
		return ""
	}
	return *v.data.ShortId
}

func (v *VersionInfo) SetShortId(short_id string) {
	v.data.ShortId = &short_id
}

func (v *VersionInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *VersionInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *VersionInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *VersionInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type appInfoData struct {
	Name             *string             `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	CreatedAt        *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"created_at,omitempty"`
	CurrentVersion   *VersionInfo        `cbor:"2,keyasint,omitempty" json:"current_version,omitempty"`
	Health           *string             `cbor:"3,keyasint,omitempty" json:"health,omitempty"`
	ReadyInstances   *int32              `cbor:"4,keyasint,omitempty" json:"ready_instances,omitempty"`
	DesiredInstances *int32              `cbor:"5,keyasint,omitempty" json:"desired_instances,omitempty"`
	ScalingMode      *string             `cbor:"6,keyasint,omitempty" json:"scaling_mode,omitempty"`
	Routes           *[]string           `cbor:"7,keyasint,omitempty" json:"routes,omitempty"`
	CrashCount       *int64              `cbor:"8,keyasint,omitempty" json:"crash_count,omitempty"`
	CooldownSeconds  *int32              `cbor:"9,keyasint,omitempty" json:"cooldown_seconds,omitempty"`
}

type AppInfo struct {
	data appInfoData
}

func (v *AppInfo) HasName() bool {
	return v.data.Name != nil
}

func (v *AppInfo) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AppInfo) SetName(name string) {
	v.data.Name = &name
}

func (v *AppInfo) HasCreatedAt() bool {
	return v.data.CreatedAt != nil
}

func (v *AppInfo) CreatedAt() *standard.Timestamp {
	return v.data.CreatedAt
}

func (v *AppInfo) SetCreatedAt(created_at *standard.Timestamp) {
	v.data.CreatedAt = created_at
}

func (v *AppInfo) HasCurrentVersion() bool {
	return v.data.CurrentVersion != nil
}

func (v *AppInfo) CurrentVersion() *VersionInfo {
	return v.data.CurrentVersion
}

func (v *AppInfo) SetCurrentVersion(current_version *VersionInfo) {
	v.data.CurrentVersion = current_version
}

func (v *AppInfo) HasHealth() bool {
	return v.data.Health != nil
}

func (v *AppInfo) Health() string {
	if v.data.Health == nil {
		return ""
	}
	return *v.data.Health
}

func (v *AppInfo) SetHealth(health string) {
	v.data.Health = &health
}

func (v *AppInfo) HasReadyInstances() bool {
	return v.data.ReadyInstances != nil
}

func (v *AppInfo) ReadyInstances() int32 {
	if v.data.ReadyInstances == nil {
		return 0
	}
	return *v.data.ReadyInstances
}

func (v *AppInfo) SetReadyInstances(ready_instances int32) {
	v.data.ReadyInstances = &ready_instances
}

func (v *AppInfo) HasDesiredInstances() bool {
	return v.data.DesiredInstances != nil
}

func (v *AppInfo) DesiredInstances() int32 {
	if v.data.DesiredInstances == nil {
		return 0
	}
	return *v.data.DesiredInstances
}

func (v *AppInfo) SetDesiredInstances(desired_instances int32) {
	v.data.DesiredInstances = &desired_instances
}

func (v *AppInfo) HasScalingMode() bool {
	return v.data.ScalingMode != nil
}

func (v *AppInfo) ScalingMode() string {
	if v.data.ScalingMode == nil {
		return ""
	}
	return *v.data.ScalingMode
}

func (v *AppInfo) SetScalingMode(scaling_mode string) {
	v.data.ScalingMode = &scaling_mode
}

func (v *AppInfo) HasRoutes() bool {
	return v.data.Routes != nil
}

func (v *AppInfo) Routes() []string {
	if v.data.Routes == nil {
		return nil
	}
	return *v.data.Routes
}

func (v *AppInfo) SetRoutes(routes []string) {
	x := slices.Clone(routes)
	v.data.Routes = &x
}

func (v *AppInfo) HasCrashCount() bool {
	return v.data.CrashCount != nil
}

func (v *AppInfo) CrashCount() int64 {
	if v.data.CrashCount == nil {
		return 0
	}
	return *v.data.CrashCount
}

func (v *AppInfo) SetCrashCount(crash_count int64) {
	v.data.CrashCount = &crash_count
}

func (v *AppInfo) HasCooldownSeconds() bool {
	return v.data.CooldownSeconds != nil
}

func (v *AppInfo) CooldownSeconds() int32 {
	if v.data.CooldownSeconds == nil {
		return 0
	}
	return *v.data.CooldownSeconds
}

func (v *AppInfo) SetCooldownSeconds(cooldown_seconds int32) {
	v.data.CooldownSeconds = &cooldown_seconds
}

func (v *AppInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AppInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AppInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AppInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type cpuUsageData struct {
	Start *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"start,omitempty"`
	Cores *float64            `cbor:"1,keyasint,omitempty" json:"cores,omitempty"`
}

type CpuUsage struct {
	data cpuUsageData
}

func (v *CpuUsage) HasStart() bool {
	return v.data.Start != nil
}

func (v *CpuUsage) Start() *standard.Timestamp {
	return v.data.Start
}

func (v *CpuUsage) SetStart(start *standard.Timestamp) {
	v.data.Start = start
}

func (v *CpuUsage) HasCores() bool {
	return v.data.Cores != nil
}

func (v *CpuUsage) Cores() float64 {
	if v.data.Cores == nil {
		return 0
	}
	return *v.data.Cores
}

func (v *CpuUsage) SetCores(cores float64) {
	v.data.Cores = &cores
}

func (v *CpuUsage) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CpuUsage) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CpuUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CpuUsage) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type memoryUsageData struct {
	Timestamp *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"timestamp,omitempty"`
	Bytes     *int64              `cbor:"1,keyasint,omitempty" json:"bytes,omitempty"`
}

type MemoryUsage struct {
	data memoryUsageData
}

func (v *MemoryUsage) HasTimestamp() bool {
	return v.data.Timestamp != nil
}

func (v *MemoryUsage) Timestamp() *standard.Timestamp {
	return v.data.Timestamp
}

func (v *MemoryUsage) SetTimestamp(timestamp *standard.Timestamp) {
	v.data.Timestamp = timestamp
}

func (v *MemoryUsage) HasBytes() bool {
	return v.data.Bytes != nil
}

func (v *MemoryUsage) Bytes() int64 {
	if v.data.Bytes == nil {
		return 0
	}
	return *v.data.Bytes
}

func (v *MemoryUsage) SetBytes(bytes int64) {
	v.data.Bytes = &bytes
}

func (v *MemoryUsage) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *MemoryUsage) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *MemoryUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *MemoryUsage) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type requestStatData struct {
	Timestamp     *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"timestamp,omitempty"`
	Count         *int64              `cbor:"1,keyasint,omitempty" json:"count,omitempty"`
	AvgDurationMs *float64            `cbor:"2,keyasint,omitempty" json:"avg_duration_ms,omitempty"`
	ErrorRate     *float64            `cbor:"3,keyasint,omitempty" json:"error_rate,omitempty"`
	P95DurationMs *float64            `cbor:"4,keyasint,omitempty" json:"p95_duration_ms,omitempty"`
	P99DurationMs *float64            `cbor:"5,keyasint,omitempty" json:"p99_duration_ms,omitempty"`
}

type RequestStat struct {
	data requestStatData
}

func (v *RequestStat) HasTimestamp() bool {
	return v.data.Timestamp != nil
}

func (v *RequestStat) Timestamp() *standard.Timestamp {
	return v.data.Timestamp
}

func (v *RequestStat) SetTimestamp(timestamp *standard.Timestamp) {
	v.data.Timestamp = timestamp
}

func (v *RequestStat) HasCount() bool {
	return v.data.Count != nil
}

func (v *RequestStat) Count() int64 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *RequestStat) SetCount(count int64) {
	v.data.Count = &count
}

func (v *RequestStat) HasAvgDurationMs() bool {
	return v.data.AvgDurationMs != nil
}

func (v *RequestStat) AvgDurationMs() float64 {
	if v.data.AvgDurationMs == nil {
		return 0
	}
	return *v.data.AvgDurationMs
}

func (v *RequestStat) SetAvgDurationMs(avgDurationMs float64) {
	v.data.AvgDurationMs = &avgDurationMs
}

func (v *RequestStat) HasErrorRate() bool {
	return v.data.ErrorRate != nil
}

func (v *RequestStat) ErrorRate() float64 {
	if v.data.ErrorRate == nil {
		return 0
	}
	return *v.data.ErrorRate
}

func (v *RequestStat) SetErrorRate(errorRate float64) {
	v.data.ErrorRate = &errorRate
}

func (v *RequestStat) HasP95DurationMs() bool {
	return v.data.P95DurationMs != nil
}

func (v *RequestStat) P95DurationMs() float64 {
	if v.data.P95DurationMs == nil {
		return 0
	}
	return *v.data.P95DurationMs
}

func (v *RequestStat) SetP95DurationMs(p95DurationMs float64) {
	v.data.P95DurationMs = &p95DurationMs
}

func (v *RequestStat) HasP99DurationMs() bool {
	return v.data.P99DurationMs != nil
}

func (v *RequestStat) P99DurationMs() float64 {
	if v.data.P99DurationMs == nil {
		return 0
	}
	return *v.data.P99DurationMs
}

func (v *RequestStat) SetP99DurationMs(p99DurationMs float64) {
	v.data.P99DurationMs = &p99DurationMs
}

func (v *RequestStat) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RequestStat) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RequestStat) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RequestStat) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type pathStatData struct {
	Path          *string  `cbor:"0,keyasint,omitempty" json:"path,omitempty"`
	Count         *int64   `cbor:"1,keyasint,omitempty" json:"count,omitempty"`
	AvgDurationMs *float64 `cbor:"2,keyasint,omitempty" json:"avg_duration_ms,omitempty"`
	ErrorRate     *float64 `cbor:"3,keyasint,omitempty" json:"error_rate,omitempty"`
}

type PathStat struct {
	data pathStatData
}

func (v *PathStat) HasPath() bool {
	return v.data.Path != nil
}

func (v *PathStat) Path() string {
	if v.data.Path == nil {
		return ""
	}
	return *v.data.Path
}

func (v *PathStat) SetPath(path string) {
	v.data.Path = &path
}

func (v *PathStat) HasCount() bool {
	return v.data.Count != nil
}

func (v *PathStat) Count() int64 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *PathStat) SetCount(count int64) {
	v.data.Count = &count
}

func (v *PathStat) HasAvgDurationMs() bool {
	return v.data.AvgDurationMs != nil
}

func (v *PathStat) AvgDurationMs() float64 {
	if v.data.AvgDurationMs == nil {
		return 0
	}
	return *v.data.AvgDurationMs
}

func (v *PathStat) SetAvgDurationMs(avgDurationMs float64) {
	v.data.AvgDurationMs = &avgDurationMs
}

func (v *PathStat) HasErrorRate() bool {
	return v.data.ErrorRate != nil
}

func (v *PathStat) ErrorRate() float64 {
	if v.data.ErrorRate == nil {
		return 0
	}
	return *v.data.ErrorRate
}

func (v *PathStat) SetErrorRate(errorRate float64) {
	v.data.ErrorRate = &errorRate
}

func (v *PathStat) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *PathStat) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *PathStat) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *PathStat) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type errorBreakdownData struct {
	StatusCode *int32   `cbor:"0,keyasint,omitempty" json:"status_code,omitempty"`
	Count      *int64   `cbor:"1,keyasint,omitempty" json:"count,omitempty"`
	Percentage *float64 `cbor:"2,keyasint,omitempty" json:"percentage,omitempty"`
}

type ErrorBreakdown struct {
	data errorBreakdownData
}

func (v *ErrorBreakdown) HasStatusCode() bool {
	return v.data.StatusCode != nil
}

func (v *ErrorBreakdown) StatusCode() int32 {
	if v.data.StatusCode == nil {
		return 0
	}
	return *v.data.StatusCode
}

func (v *ErrorBreakdown) SetStatusCode(statusCode int32) {
	v.data.StatusCode = &statusCode
}

func (v *ErrorBreakdown) HasCount() bool {
	return v.data.Count != nil
}

func (v *ErrorBreakdown) Count() int64 {
	if v.data.Count == nil {
		return 0
	}
	return *v.data.Count
}

func (v *ErrorBreakdown) SetCount(count int64) {
	v.data.Count = &count
}

func (v *ErrorBreakdown) HasPercentage() bool {
	return v.data.Percentage != nil
}

func (v *ErrorBreakdown) Percentage() float64 {
	if v.data.Percentage == nil {
		return 0
	}
	return *v.data.Percentage
}

func (v *ErrorBreakdown) SetPercentage(percentage float64) {
	v.data.Percentage = &percentage
}

func (v *ErrorBreakdown) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ErrorBreakdown) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ErrorBreakdown) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ErrorBreakdown) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type boundPortData struct {
	Port    *int64  `cbor:"0,keyasint,omitempty" json:"port,omitempty"`
	Address *string `cbor:"1,keyasint,omitempty" json:"address,omitempty"`
}

type BoundPort struct {
	data boundPortData
}

func (v *BoundPort) HasPort() bool {
	return v.data.Port != nil
}

func (v *BoundPort) Port() int64 {
	if v.data.Port == nil {
		return 0
	}
	return *v.data.Port
}

func (v *BoundPort) SetPort(port int64) {
	v.data.Port = &port
}

func (v *BoundPort) HasAddress() bool {
	return v.data.Address != nil
}

func (v *BoundPort) Address() string {
	if v.data.Address == nil {
		return ""
	}
	return *v.data.Address
}

func (v *BoundPort) SetAddress(address string) {
	v.data.Address = &address
}

func (v *BoundPort) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *BoundPort) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *BoundPort) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *BoundPort) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type poolStatusData struct {
	Name      *string          `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Windows   *[]*WindowStatus `cbor:"1,keyasint,omitempty" json:"windows,omitempty"`
	Idle      *int32           `cbor:"2,keyasint,omitempty" json:"idle,omitempty"`
	IdleUsage *int64           `cbor:"3,keyasint,omitempty" json:"idle_usage,omitempty"`
}

type PoolStatus struct {
	data poolStatusData
}

func (v *PoolStatus) HasName() bool {
	return v.data.Name != nil
}

func (v *PoolStatus) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *PoolStatus) SetName(name string) {
	v.data.Name = &name
}

func (v *PoolStatus) HasWindows() bool {
	return v.data.Windows != nil
}

func (v *PoolStatus) Windows() []*WindowStatus {
	if v.data.Windows == nil {
		return nil
	}
	return *v.data.Windows
}

func (v *PoolStatus) SetWindows(windows []*WindowStatus) {
	x := slices.Clone(windows)
	v.data.Windows = &x
}

func (v *PoolStatus) HasIdle() bool {
	return v.data.Idle != nil
}

func (v *PoolStatus) Idle() int32 {
	if v.data.Idle == nil {
		return 0
	}
	return *v.data.Idle
}

func (v *PoolStatus) SetIdle(idle int32) {
	v.data.Idle = &idle
}

func (v *PoolStatus) HasIdleUsage() bool {
	return v.data.IdleUsage != nil
}

func (v *PoolStatus) IdleUsage() int64 {
	if v.data.IdleUsage == nil {
		return 0
	}
	return *v.data.IdleUsage
}

func (v *PoolStatus) SetIdleUsage(idleUsage int64) {
	v.data.IdleUsage = &idleUsage
}

func (v *PoolStatus) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *PoolStatus) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *PoolStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *PoolStatus) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type windowStatusData struct {
	Version *string `cbor:"0,keyasint,omitempty" json:"version,omitempty"`
	Leases  *int32  `cbor:"1,keyasint,omitempty" json:"leases,omitempty"`
	Usage   *int64  `cbor:"2,keyasint,omitempty" json:"usage,omitempty"`
}

type WindowStatus struct {
	data windowStatusData
}

func (v *WindowStatus) HasVersion() bool {
	return v.data.Version != nil
}

func (v *WindowStatus) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *WindowStatus) SetVersion(version string) {
	v.data.Version = &version
}

func (v *WindowStatus) HasLeases() bool {
	return v.data.Leases != nil
}

func (v *WindowStatus) Leases() int32 {
	if v.data.Leases == nil {
		return 0
	}
	return *v.data.Leases
}

func (v *WindowStatus) SetLeases(leases int32) {
	v.data.Leases = &leases
}

func (v *WindowStatus) HasUsage() bool {
	return v.data.Usage != nil
}

func (v *WindowStatus) Usage() int64 {
	if v.data.Usage == nil {
		return 0
	}
	return *v.data.Usage
}

func (v *WindowStatus) SetUsage(usage int64) {
	v.data.Usage = &usage
}

func (v *WindowStatus) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *WindowStatus) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *WindowStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *WindowStatus) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type applicationStatusData struct {
	Name              *string             `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Pools             *[]*PoolStatus      `cbor:"1,keyasint,omitempty" json:"pools,omitempty"`
	LastMinCPU        *float64            `cbor:"2,keyasint,omitempty" json:"last_min_c_p_u,omitempty"`
	LastHourCPU       *float64            `cbor:"3,keyasint,omitempty" json:"last_hour_c_p_u,omitempty"`
	LastDayCPU        *float64            `cbor:"4,keyasint,omitempty" json:"last_day_c_p_u,omitempty"`
	CpuOverHour       *[]*CpuUsage        `cbor:"5,keyasint,omitempty" json:"cpu_over_hour,omitempty"`
	MemoryOverHour    *[]*MemoryUsage     `cbor:"6,keyasint,omitempty" json:"memory_over_hour,omitempty"`
	ActiveVersion     *string             `cbor:"7,keyasint,omitempty" json:"active_version,omitempty"`
	LastDeploy        *standard.Timestamp `cbor:"8,keyasint,omitempty" json:"last_deploy,omitempty"`
	Addons            *[]*AddonInstance   `cbor:"9,keyasint,omitempty" json:"addons,omitempty"`
	RequestsPerSecond *float64            `cbor:"10,keyasint,omitempty" json:"requests_per_second,omitempty"`
	RequestStats      *[]*RequestStat     `cbor:"11,keyasint,omitempty" json:"request_stats,omitempty"`
	TopPaths          *[]*PathStat        `cbor:"12,keyasint,omitempty" json:"top_paths,omitempty"`
	ErrorBreakdown    *[]*ErrorBreakdown  `cbor:"13,keyasint,omitempty" json:"error_breakdown,omitempty"`
	Health            *string             `cbor:"14,keyasint,omitempty" json:"health,omitempty"`
	ReadyInstances    *int32              `cbor:"15,keyasint,omitempty" json:"ready_instances,omitempty"`
	DesiredInstances  *int32              `cbor:"16,keyasint,omitempty" json:"desired_instances,omitempty"`
	CrashCount        *int64              `cbor:"17,keyasint,omitempty" json:"crash_count,omitempty"`
	CooldownSeconds   *int32              `cbor:"18,keyasint,omitempty" json:"cooldown_seconds,omitempty"`
	BoundPorts        *[]*BoundPort       `cbor:"19,keyasint,omitempty" json:"bound_ports,omitempty"`
	WorkloadRole      *string             `cbor:"20,keyasint,omitempty" json:"workload_role,omitempty"`
}

type ApplicationStatus struct {
	data applicationStatusData
}

func (v *ApplicationStatus) HasName() bool {
	return v.data.Name != nil
}

func (v *ApplicationStatus) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *ApplicationStatus) SetName(name string) {
	v.data.Name = &name
}

func (v *ApplicationStatus) HasPools() bool {
	return v.data.Pools != nil
}

func (v *ApplicationStatus) Pools() []*PoolStatus {
	if v.data.Pools == nil {
		return nil
	}
	return *v.data.Pools
}

func (v *ApplicationStatus) SetPools(pools []*PoolStatus) {
	x := slices.Clone(pools)
	v.data.Pools = &x
}

func (v *ApplicationStatus) HasLastMinCPU() bool {
	return v.data.LastMinCPU != nil
}

func (v *ApplicationStatus) LastMinCPU() float64 {
	if v.data.LastMinCPU == nil {
		return 0
	}
	return *v.data.LastMinCPU
}

func (v *ApplicationStatus) SetLastMinCPU(lastMinCPU float64) {
	v.data.LastMinCPU = &lastMinCPU
}

func (v *ApplicationStatus) HasLastHourCPU() bool {
	return v.data.LastHourCPU != nil
}

func (v *ApplicationStatus) LastHourCPU() float64 {
	if v.data.LastHourCPU == nil {
		return 0
	}
	return *v.data.LastHourCPU
}

func (v *ApplicationStatus) SetLastHourCPU(lastHourCPU float64) {
	v.data.LastHourCPU = &lastHourCPU
}

func (v *ApplicationStatus) HasLastDayCPU() bool {
	return v.data.LastDayCPU != nil
}

func (v *ApplicationStatus) LastDayCPU() float64 {
	if v.data.LastDayCPU == nil {
		return 0
	}
	return *v.data.LastDayCPU
}

func (v *ApplicationStatus) SetLastDayCPU(lastDayCPU float64) {
	v.data.LastDayCPU = &lastDayCPU
}

func (v *ApplicationStatus) HasCpuOverHour() bool {
	return v.data.CpuOverHour != nil
}

func (v *ApplicationStatus) CpuOverHour() []*CpuUsage {
	if v.data.CpuOverHour == nil {
		return nil
	}
	return *v.data.CpuOverHour
}

func (v *ApplicationStatus) SetCpuOverHour(cpuOverHour []*CpuUsage) {
	x := slices.Clone(cpuOverHour)
	v.data.CpuOverHour = &x
}

func (v *ApplicationStatus) HasMemoryOverHour() bool {
	return v.data.MemoryOverHour != nil
}

func (v *ApplicationStatus) MemoryOverHour() []*MemoryUsage {
	if v.data.MemoryOverHour == nil {
		return nil
	}
	return *v.data.MemoryOverHour
}

func (v *ApplicationStatus) SetMemoryOverHour(memoryOverHour []*MemoryUsage) {
	x := slices.Clone(memoryOverHour)
	v.data.MemoryOverHour = &x
}

func (v *ApplicationStatus) HasActiveVersion() bool {
	return v.data.ActiveVersion != nil
}

func (v *ApplicationStatus) ActiveVersion() string {
	if v.data.ActiveVersion == nil {
		return ""
	}
	return *v.data.ActiveVersion
}

func (v *ApplicationStatus) SetActiveVersion(activeVersion string) {
	v.data.ActiveVersion = &activeVersion
}

func (v *ApplicationStatus) HasLastDeploy() bool {
	return v.data.LastDeploy != nil
}

func (v *ApplicationStatus) LastDeploy() *standard.Timestamp {
	return v.data.LastDeploy
}

func (v *ApplicationStatus) SetLastDeploy(lastDeploy *standard.Timestamp) {
	v.data.LastDeploy = lastDeploy
}

func (v *ApplicationStatus) HasAddons() bool {
	return v.data.Addons != nil
}

func (v *ApplicationStatus) Addons() []*AddonInstance {
	if v.data.Addons == nil {
		return nil
	}
	return *v.data.Addons
}

func (v *ApplicationStatus) SetAddons(addons []*AddonInstance) {
	x := slices.Clone(addons)
	v.data.Addons = &x
}

func (v *ApplicationStatus) HasRequestsPerSecond() bool {
	return v.data.RequestsPerSecond != nil
}

func (v *ApplicationStatus) RequestsPerSecond() float64 {
	if v.data.RequestsPerSecond == nil {
		return 0
	}
	return *v.data.RequestsPerSecond
}

func (v *ApplicationStatus) SetRequestsPerSecond(requestsPerSecond float64) {
	v.data.RequestsPerSecond = &requestsPerSecond
}

func (v *ApplicationStatus) HasRequestStats() bool {
	return v.data.RequestStats != nil
}

func (v *ApplicationStatus) RequestStats() []*RequestStat {
	if v.data.RequestStats == nil {
		return nil
	}
	return *v.data.RequestStats
}

func (v *ApplicationStatus) SetRequestStats(requestStats []*RequestStat) {
	x := slices.Clone(requestStats)
	v.data.RequestStats = &x
}

func (v *ApplicationStatus) HasTopPaths() bool {
	return v.data.TopPaths != nil
}

func (v *ApplicationStatus) TopPaths() []*PathStat {
	if v.data.TopPaths == nil {
		return nil
	}
	return *v.data.TopPaths
}

func (v *ApplicationStatus) SetTopPaths(topPaths []*PathStat) {
	x := slices.Clone(topPaths)
	v.data.TopPaths = &x
}

func (v *ApplicationStatus) HasErrorBreakdown() bool {
	return v.data.ErrorBreakdown != nil
}

func (v *ApplicationStatus) ErrorBreakdown() []*ErrorBreakdown {
	if v.data.ErrorBreakdown == nil {
		return nil
	}
	return *v.data.ErrorBreakdown
}

func (v *ApplicationStatus) SetErrorBreakdown(errorBreakdown []*ErrorBreakdown) {
	x := slices.Clone(errorBreakdown)
	v.data.ErrorBreakdown = &x
}

func (v *ApplicationStatus) HasHealth() bool {
	return v.data.Health != nil
}

func (v *ApplicationStatus) Health() string {
	if v.data.Health == nil {
		return ""
	}
	return *v.data.Health
}

func (v *ApplicationStatus) SetHealth(health string) {
	v.data.Health = &health
}

func (v *ApplicationStatus) HasReadyInstances() bool {
	return v.data.ReadyInstances != nil
}

func (v *ApplicationStatus) ReadyInstances() int32 {
	if v.data.ReadyInstances == nil {
		return 0
	}
	return *v.data.ReadyInstances
}

func (v *ApplicationStatus) SetReadyInstances(readyInstances int32) {
	v.data.ReadyInstances = &readyInstances
}

func (v *ApplicationStatus) HasDesiredInstances() bool {
	return v.data.DesiredInstances != nil
}

func (v *ApplicationStatus) DesiredInstances() int32 {
	if v.data.DesiredInstances == nil {
		return 0
	}
	return *v.data.DesiredInstances
}

func (v *ApplicationStatus) SetDesiredInstances(desiredInstances int32) {
	v.data.DesiredInstances = &desiredInstances
}

func (v *ApplicationStatus) HasCrashCount() bool {
	return v.data.CrashCount != nil
}

func (v *ApplicationStatus) CrashCount() int64 {
	if v.data.CrashCount == nil {
		return 0
	}
	return *v.data.CrashCount
}

func (v *ApplicationStatus) SetCrashCount(crashCount int64) {
	v.data.CrashCount = &crashCount
}

func (v *ApplicationStatus) HasCooldownSeconds() bool {
	return v.data.CooldownSeconds != nil
}

func (v *ApplicationStatus) CooldownSeconds() int32 {
	if v.data.CooldownSeconds == nil {
		return 0
	}
	return *v.data.CooldownSeconds
}

func (v *ApplicationStatus) SetCooldownSeconds(cooldownSeconds int32) {
	v.data.CooldownSeconds = &cooldownSeconds
}

func (v *ApplicationStatus) HasBoundPorts() bool {
	return v.data.BoundPorts != nil
}

func (v *ApplicationStatus) BoundPorts() []*BoundPort {
	if v.data.BoundPorts == nil {
		return nil
	}
	return *v.data.BoundPorts
}

func (v *ApplicationStatus) SetBoundPorts(boundPorts []*BoundPort) {
	x := slices.Clone(boundPorts)
	v.data.BoundPorts = &x
}

func (v *ApplicationStatus) HasWorkloadRole() bool {
	return v.data.WorkloadRole != nil
}

func (v *ApplicationStatus) WorkloadRole() string {
	if v.data.WorkloadRole == nil {
		return ""
	}
	return *v.data.WorkloadRole
}

func (v *ApplicationStatus) SetWorkloadRole(workloadRole string) {
	v.data.WorkloadRole = &workloadRole
}

func (v *ApplicationStatus) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *ApplicationStatus) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *ApplicationStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *ApplicationStatus) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logEntryData struct {
	Timestamp  *standard.Timestamp `cbor:"0,keyasint,omitempty" json:"timestamp,omitempty"`
	Line       *string             `cbor:"1,keyasint,omitempty" json:"line,omitempty"`
	Stream     *string             `cbor:"2,keyasint,omitempty" json:"stream,omitempty"`
	Source     *string             `cbor:"3,keyasint,omitempty" json:"source,omitempty"`
	Attributes *map[string]string  `cbor:"4,keyasint,omitempty" json:"attributes,omitempty"`
}

type LogEntry struct {
	data logEntryData
}

func (v *LogEntry) HasTimestamp() bool {
	return v.data.Timestamp != nil
}

func (v *LogEntry) Timestamp() *standard.Timestamp {
	return v.data.Timestamp
}

func (v *LogEntry) SetTimestamp(timestamp *standard.Timestamp) {
	v.data.Timestamp = timestamp
}

func (v *LogEntry) HasLine() bool {
	return v.data.Line != nil
}

func (v *LogEntry) Line() string {
	if v.data.Line == nil {
		return ""
	}
	return *v.data.Line
}

func (v *LogEntry) SetLine(line string) {
	v.data.Line = &line
}

func (v *LogEntry) HasStream() bool {
	return v.data.Stream != nil
}

func (v *LogEntry) Stream() string {
	if v.data.Stream == nil {
		return ""
	}
	return *v.data.Stream
}

func (v *LogEntry) SetStream(stream string) {
	v.data.Stream = &stream
}

func (v *LogEntry) HasSource() bool {
	return v.data.Source != nil
}

func (v *LogEntry) Source() string {
	if v.data.Source == nil {
		return ""
	}
	return *v.data.Source
}

func (v *LogEntry) SetSource(source string) {
	v.data.Source = &source
}

func (v *LogEntry) HasAttributes() bool {
	return v.data.Attributes != nil
}

func (v *LogEntry) Attributes() map[string]string {
	if v.data.Attributes == nil {
		return nil
	}
	return *v.data.Attributes
}

func (v *LogEntry) SetAttributes(attributes map[string]string) {
	x := maps.Clone(attributes)
	v.data.Attributes = &x
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

type logChunkData struct {
	Entries *[]*LogEntry `cbor:"0,keyasint,omitempty" json:"entries,omitempty"`
}

type LogChunk struct {
	data logChunkData
}

func (v *LogChunk) HasEntries() bool {
	return v.data.Entries != nil
}

func (v *LogChunk) Entries() []*LogEntry {
	if v.data.Entries == nil {
		return nil
	}
	return *v.data.Entries
}

func (v *LogChunk) SetEntries(entries []*LogEntry) {
	x := slices.Clone(entries)
	v.data.Entries = &x
}

func (v *LogChunk) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogChunk) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogChunk) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogChunk) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type userInfoData struct {
	Subject *string `cbor:"0,keyasint,omitempty" json:"subject,omitempty"`
}

type UserInfo struct {
	data userInfoData
}

func (v *UserInfo) HasSubject() bool {
	return v.data.Subject != nil
}

func (v *UserInfo) Subject() string {
	if v.data.Subject == nil {
		return ""
	}
	return *v.data.Subject
}

func (v *UserInfo) SetSubject(subject string) {
	v.data.Subject = &subject
}

func (v *UserInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UserInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UserInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UserInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type diskConfigData struct {
	Id       *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Name     *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	Capacity *int64  `cbor:"2,keyasint,omitempty" json:"capacity,omitempty"`
}

type DiskConfig struct {
	data diskConfigData
}

func (v *DiskConfig) HasId() bool {
	return v.data.Id != nil
}

func (v *DiskConfig) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DiskConfig) SetId(id string) {
	v.data.Id = &id
}

func (v *DiskConfig) HasName() bool {
	return v.data.Name != nil
}

func (v *DiskConfig) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *DiskConfig) SetName(name string) {
	v.data.Name = &name
}

func (v *DiskConfig) HasCapacity() bool {
	return v.data.Capacity != nil
}

func (v *DiskConfig) Capacity() int64 {
	if v.data.Capacity == nil {
		return 0
	}
	return *v.data.Capacity
}

func (v *DiskConfig) SetCapacity(capacity int64) {
	v.data.Capacity = &capacity
}

func (v *DiskConfig) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DiskConfig) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DiskConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DiskConfig) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonInstanceData struct {
	Id      *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Name    *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	Addon   *string `cbor:"2,keyasint,omitempty" json:"addon,omitempty"`
	Variant *string `cbor:"3,keyasint,omitempty" json:"variant,omitempty"`
	Version *string `cbor:"4,keyasint,omitempty" json:"version,omitempty"`
}

type AddonInstance struct {
	data addonInstanceData
}

func (v *AddonInstance) HasId() bool {
	return v.data.Id != nil
}

func (v *AddonInstance) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *AddonInstance) SetId(id string) {
	v.data.Id = &id
}

func (v *AddonInstance) HasName() bool {
	return v.data.Name != nil
}

func (v *AddonInstance) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AddonInstance) SetName(name string) {
	v.data.Name = &name
}

func (v *AddonInstance) HasAddon() bool {
	return v.data.Addon != nil
}

func (v *AddonInstance) Addon() string {
	if v.data.Addon == nil {
		return ""
	}
	return *v.data.Addon
}

func (v *AddonInstance) SetAddon(addon string) {
	v.data.Addon = &addon
}

func (v *AddonInstance) HasVariant() bool {
	return v.data.Variant != nil
}

func (v *AddonInstance) Variant() string {
	if v.data.Variant == nil {
		return ""
	}
	return *v.data.Variant
}

func (v *AddonInstance) SetVariant(variant string) {
	v.data.Variant = &variant
}

func (v *AddonInstance) HasVersion() bool {
	return v.data.Version != nil
}

func (v *AddonInstance) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *AddonInstance) SetVersion(version string) {
	v.data.Version = &version
}

func (v *AddonInstance) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonInstance) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonInstance) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonInstance) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runInfoData struct {
	Id        *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	Task      *string `cbor:"1,keyasint,omitempty" json:"task,omitempty"`
	Trigger   *string `cbor:"2,keyasint,omitempty" json:"trigger,omitempty"`
	Status    *string `cbor:"3,keyasint,omitempty" json:"status,omitempty"`
	Command   *string `cbor:"4,keyasint,omitempty" json:"command,omitempty"`
	ExitCode  *int32  `cbor:"5,keyasint,omitempty" json:"exit_code,omitempty"`
	Attempt   *int32  `cbor:"7,keyasint,omitempty" json:"attempt,omitempty"`
	StartedAt *int64  `cbor:"8,keyasint,omitempty" json:"started_at,omitempty"`
	EndedAt   *int64  `cbor:"9,keyasint,omitempty" json:"ended_at,omitempty"`
	Sandbox   *string `cbor:"10,keyasint,omitempty" json:"sandbox,omitempty"`
	Version   *string `cbor:"11,keyasint,omitempty" json:"version,omitempty"`
}

type RunInfo struct {
	data runInfoData
}

func (v *RunInfo) HasId() bool {
	return v.data.Id != nil
}

func (v *RunInfo) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *RunInfo) SetId(id string) {
	v.data.Id = &id
}

func (v *RunInfo) HasTask() bool {
	return v.data.Task != nil
}

func (v *RunInfo) Task() string {
	if v.data.Task == nil {
		return ""
	}
	return *v.data.Task
}

func (v *RunInfo) SetTask(task string) {
	v.data.Task = &task
}

func (v *RunInfo) HasTrigger() bool {
	return v.data.Trigger != nil
}

func (v *RunInfo) Trigger() string {
	if v.data.Trigger == nil {
		return ""
	}
	return *v.data.Trigger
}

func (v *RunInfo) SetTrigger(trigger string) {
	v.data.Trigger = &trigger
}

func (v *RunInfo) HasStatus() bool {
	return v.data.Status != nil
}

func (v *RunInfo) Status() string {
	if v.data.Status == nil {
		return ""
	}
	return *v.data.Status
}

func (v *RunInfo) SetStatus(status string) {
	v.data.Status = &status
}

func (v *RunInfo) HasCommand() bool {
	return v.data.Command != nil
}

func (v *RunInfo) Command() string {
	if v.data.Command == nil {
		return ""
	}
	return *v.data.Command
}

func (v *RunInfo) SetCommand(command string) {
	v.data.Command = &command
}

func (v *RunInfo) HasExitCode() bool {
	return v.data.ExitCode != nil
}

func (v *RunInfo) ExitCode() int32 {
	if v.data.ExitCode == nil {
		return 0
	}
	return *v.data.ExitCode
}

func (v *RunInfo) SetExitCode(exit_code int32) {
	v.data.ExitCode = &exit_code
}

func (v *RunInfo) HasAttempt() bool {
	return v.data.Attempt != nil
}

func (v *RunInfo) Attempt() int32 {
	if v.data.Attempt == nil {
		return 0
	}
	return *v.data.Attempt
}

func (v *RunInfo) SetAttempt(attempt int32) {
	v.data.Attempt = &attempt
}

func (v *RunInfo) HasStartedAt() bool {
	return v.data.StartedAt != nil
}

func (v *RunInfo) StartedAt() int64 {
	if v.data.StartedAt == nil {
		return 0
	}
	return *v.data.StartedAt
}

func (v *RunInfo) SetStartedAt(started_at int64) {
	v.data.StartedAt = &started_at
}

func (v *RunInfo) HasEndedAt() bool {
	return v.data.EndedAt != nil
}

func (v *RunInfo) EndedAt() int64 {
	if v.data.EndedAt == nil {
		return 0
	}
	return *v.data.EndedAt
}

func (v *RunInfo) SetEndedAt(ended_at int64) {
	v.data.EndedAt = &ended_at
}

func (v *RunInfo) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *RunInfo) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *RunInfo) SetSandbox(sandbox string) {
	v.data.Sandbox = &sandbox
}

func (v *RunInfo) HasVersion() bool {
	return v.data.Version != nil
}

func (v *RunInfo) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *RunInfo) SetVersion(version string) {
	v.data.Version = &version
}

func (v *RunInfo) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunInfo) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunInfo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudNewArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type CrudNewArgs struct {
	call rpc.Call
	data crudNewArgsData
}

func (v *CrudNewArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *CrudNewArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *CrudNewArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudNewArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudNewArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudNewArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudNewResultsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type CrudNewResults struct {
	call rpc.Call
	data crudNewResultsData
}

func (v *CrudNewResults) SetId(id string) {
	v.data.Id = &id
}

func (v *CrudNewResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudNewResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudNewResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudNewResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetConfigurationArgsData struct {
	App           *string        `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Configuration *Configuration `cbor:"1,keyasint,omitempty" json:"configuration,omitempty"`
}

type CrudSetConfigurationArgs struct {
	call rpc.Call
	data crudSetConfigurationArgsData
}

func (v *CrudSetConfigurationArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudSetConfigurationArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudSetConfigurationArgs) HasConfiguration() bool {
	return v.data.Configuration != nil
}

func (v *CrudSetConfigurationArgs) Configuration() *Configuration {
	return v.data.Configuration
}

func (v *CrudSetConfigurationArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetConfigurationArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetConfigurationArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetConfigurationArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetConfigurationResultsData struct {
	VersionId      *string `cbor:"0,keyasint,omitempty" json:"versionId,omitempty"`
	VersionShortId *string `cbor:"1,keyasint,omitempty" json:"versionShortId,omitempty"`
}

type CrudSetConfigurationResults struct {
	call rpc.Call
	data crudSetConfigurationResultsData
}

func (v *CrudSetConfigurationResults) SetVersionId(versionId string) {
	v.data.VersionId = &versionId
}

func (v *CrudSetConfigurationResults) SetVersionShortId(versionShortId string) {
	v.data.VersionShortId = &versionShortId
}

func (v *CrudSetConfigurationResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetConfigurationResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetConfigurationResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetConfigurationResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudGetConfigurationArgsData struct {
	App *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
}

type CrudGetConfigurationArgs struct {
	call rpc.Call
	data crudGetConfigurationArgsData
}

func (v *CrudGetConfigurationArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudGetConfigurationArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudGetConfigurationArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudGetConfigurationArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudGetConfigurationArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudGetConfigurationArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudGetConfigurationResultsData struct {
	Configuration  *Configuration `cbor:"0,keyasint,omitempty" json:"configuration,omitempty"`
	VersionId      *string        `cbor:"1,keyasint,omitempty" json:"versionId,omitempty"`
	VersionShortId *string        `cbor:"2,keyasint,omitempty" json:"versionShortId,omitempty"`
	WorkloadRole   *string        `cbor:"3,keyasint,omitempty" json:"workloadRole,omitempty"`
}

type CrudGetConfigurationResults struct {
	call rpc.Call
	data crudGetConfigurationResultsData
}

func (v *CrudGetConfigurationResults) SetConfiguration(configuration *Configuration) {
	v.data.Configuration = configuration
}

func (v *CrudGetConfigurationResults) SetVersionId(versionId string) {
	v.data.VersionId = &versionId
}

func (v *CrudGetConfigurationResults) SetVersionShortId(versionShortId string) {
	v.data.VersionShortId = &versionShortId
}

func (v *CrudGetConfigurationResults) SetWorkloadRole(workloadRole string) {
	v.data.WorkloadRole = &workloadRole
}

func (v *CrudGetConfigurationResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudGetConfigurationResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudGetConfigurationResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudGetConfigurationResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetHostArgsData struct {
	App  *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Host *string `cbor:"1,keyasint,omitempty" json:"host,omitempty"`
}

type CrudSetHostArgs struct {
	call rpc.Call
	data crudSetHostArgsData
}

func (v *CrudSetHostArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudSetHostArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudSetHostArgs) HasHost() bool {
	return v.data.Host != nil
}

func (v *CrudSetHostArgs) Host() string {
	if v.data.Host == nil {
		return ""
	}
	return *v.data.Host
}

func (v *CrudSetHostArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetHostArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetHostArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetHostArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetHostResultsData struct{}

type CrudSetHostResults struct {
	call rpc.Call
	data crudSetHostResultsData
}

func (v *CrudSetHostResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetHostResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetHostResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetHostResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetWorkloadRoleArgsData struct {
	App  *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Role *string `cbor:"1,keyasint,omitempty" json:"role,omitempty"`
}

type CrudSetWorkloadRoleArgs struct {
	call rpc.Call
	data crudSetWorkloadRoleArgsData
}

func (v *CrudSetWorkloadRoleArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudSetWorkloadRoleArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudSetWorkloadRoleArgs) HasRole() bool {
	return v.data.Role != nil
}

func (v *CrudSetWorkloadRoleArgs) Role() string {
	if v.data.Role == nil {
		return ""
	}
	return *v.data.Role
}

func (v *CrudSetWorkloadRoleArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetWorkloadRoleArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetWorkloadRoleArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetWorkloadRoleArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetWorkloadRoleResultsData struct{}

type CrudSetWorkloadRoleResults struct {
	call rpc.Call
	data crudSetWorkloadRoleResultsData
}

func (v *CrudSetWorkloadRoleResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetWorkloadRoleResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetWorkloadRoleResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetWorkloadRoleResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudListArgsData struct{}

type CrudListArgs struct {
	call rpc.Call
	data crudListArgsData
}

func (v *CrudListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudListResultsData struct {
	Apps *[]*AppInfo `cbor:"0,keyasint,omitempty" json:"apps,omitempty"`
}

type CrudListResults struct {
	call rpc.Call
	data crudListResultsData
}

func (v *CrudListResults) SetApps(apps []*AppInfo) {
	x := slices.Clone(apps)
	v.data.Apps = &x
}

func (v *CrudListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudDestroyArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type CrudDestroyArgs struct {
	call rpc.Call
	data crudDestroyArgsData
}

func (v *CrudDestroyArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *CrudDestroyArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *CrudDestroyArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudDestroyArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudDestroyArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudDestroyArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudDestroyResultsData struct{}

type CrudDestroyResults struct {
	call rpc.Call
	data crudDestroyResultsData
}

func (v *CrudDestroyResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudDestroyResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudDestroyResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudDestroyResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetEnvVarArgsData struct {
	App       *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Key       *string `cbor:"1,keyasint,omitempty" json:"key,omitempty"`
	Value     *string `cbor:"2,keyasint,omitempty" json:"value,omitempty"`
	Sensitive *bool   `cbor:"3,keyasint,omitempty" json:"sensitive,omitempty"`
	Service   *string `cbor:"4,keyasint,omitempty" json:"service,omitempty"`
}

type CrudSetEnvVarArgs struct {
	call rpc.Call
	data crudSetEnvVarArgsData
}

func (v *CrudSetEnvVarArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudSetEnvVarArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudSetEnvVarArgs) HasKey() bool {
	return v.data.Key != nil
}

func (v *CrudSetEnvVarArgs) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *CrudSetEnvVarArgs) HasValue() bool {
	return v.data.Value != nil
}

func (v *CrudSetEnvVarArgs) Value() string {
	if v.data.Value == nil {
		return ""
	}
	return *v.data.Value
}

func (v *CrudSetEnvVarArgs) HasSensitive() bool {
	return v.data.Sensitive != nil
}

func (v *CrudSetEnvVarArgs) Sensitive() bool {
	if v.data.Sensitive == nil {
		return false
	}
	return *v.data.Sensitive
}

func (v *CrudSetEnvVarArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *CrudSetEnvVarArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *CrudSetEnvVarArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetEnvVarArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetEnvVarArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetEnvVarArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetEnvVarResultsData struct {
	VersionId      *string `cbor:"0,keyasint,omitempty" json:"versionId,omitempty"`
	VersionShortId *string `cbor:"1,keyasint,omitempty" json:"versionShortId,omitempty"`
}

type CrudSetEnvVarResults struct {
	call rpc.Call
	data crudSetEnvVarResultsData
}

func (v *CrudSetEnvVarResults) SetVersionId(versionId string) {
	v.data.VersionId = &versionId
}

func (v *CrudSetEnvVarResults) SetVersionShortId(versionShortId string) {
	v.data.VersionShortId = &versionShortId
}

func (v *CrudSetEnvVarResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetEnvVarResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetEnvVarResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetEnvVarResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetEnvVarsArgsData struct {
	App     *string        `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Vars    *[]*NamedValue `cbor:"1,keyasint,omitempty" json:"vars,omitempty"`
	Service *string        `cbor:"2,keyasint,omitempty" json:"service,omitempty"`
}

type CrudSetEnvVarsArgs struct {
	call rpc.Call
	data crudSetEnvVarsArgsData
}

func (v *CrudSetEnvVarsArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudSetEnvVarsArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudSetEnvVarsArgs) HasVars() bool {
	return v.data.Vars != nil
}

func (v *CrudSetEnvVarsArgs) Vars() []*NamedValue {
	if v.data.Vars == nil {
		return nil
	}
	return *v.data.Vars
}

func (v *CrudSetEnvVarsArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *CrudSetEnvVarsArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *CrudSetEnvVarsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetEnvVarsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetEnvVarsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetEnvVarsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetEnvVarsResultsData struct {
	VersionId      *string `cbor:"0,keyasint,omitempty" json:"versionId,omitempty"`
	VersionShortId *string `cbor:"1,keyasint,omitempty" json:"versionShortId,omitempty"`
}

type CrudSetEnvVarsResults struct {
	call rpc.Call
	data crudSetEnvVarsResultsData
}

func (v *CrudSetEnvVarsResults) SetVersionId(versionId string) {
	v.data.VersionId = &versionId
}

func (v *CrudSetEnvVarsResults) SetVersionShortId(versionShortId string) {
	v.data.VersionShortId = &versionShortId
}

func (v *CrudSetEnvVarsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetEnvVarsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetEnvVarsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetEnvVarsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetInitialEnvVarsArgsData struct {
	App     *string        `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Vars    *[]*NamedValue `cbor:"1,keyasint,omitempty" json:"vars,omitempty"`
	Service *string        `cbor:"2,keyasint,omitempty" json:"service,omitempty"`
}

type CrudSetInitialEnvVarsArgs struct {
	call rpc.Call
	data crudSetInitialEnvVarsArgsData
}

func (v *CrudSetInitialEnvVarsArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudSetInitialEnvVarsArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudSetInitialEnvVarsArgs) HasVars() bool {
	return v.data.Vars != nil
}

func (v *CrudSetInitialEnvVarsArgs) Vars() []*NamedValue {
	if v.data.Vars == nil {
		return nil
	}
	return *v.data.Vars
}

func (v *CrudSetInitialEnvVarsArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *CrudSetInitialEnvVarsArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *CrudSetInitialEnvVarsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetInitialEnvVarsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetInitialEnvVarsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetInitialEnvVarsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudSetInitialEnvVarsResultsData struct {
	ConfigVersionId *string `cbor:"0,keyasint,omitempty" json:"configVersionId,omitempty"`
}

type CrudSetInitialEnvVarsResults struct {
	call rpc.Call
	data crudSetInitialEnvVarsResultsData
}

func (v *CrudSetInitialEnvVarsResults) SetConfigVersionId(configVersionId string) {
	v.data.ConfigVersionId = &configVersionId
}

func (v *CrudSetInitialEnvVarsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudSetInitialEnvVarsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudSetInitialEnvVarsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudSetInitialEnvVarsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudDeleteEnvVarArgsData struct {
	App     *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Key     *string `cbor:"1,keyasint,omitempty" json:"key,omitempty"`
	Service *string `cbor:"2,keyasint,omitempty" json:"service,omitempty"`
}

type CrudDeleteEnvVarArgs struct {
	call rpc.Call
	data crudDeleteEnvVarArgsData
}

func (v *CrudDeleteEnvVarArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudDeleteEnvVarArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudDeleteEnvVarArgs) HasKey() bool {
	return v.data.Key != nil
}

func (v *CrudDeleteEnvVarArgs) Key() string {
	if v.data.Key == nil {
		return ""
	}
	return *v.data.Key
}

func (v *CrudDeleteEnvVarArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *CrudDeleteEnvVarArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *CrudDeleteEnvVarArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudDeleteEnvVarArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudDeleteEnvVarArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudDeleteEnvVarArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudDeleteEnvVarResultsData struct {
	VersionId      *string `cbor:"0,keyasint,omitempty" json:"versionId,omitempty"`
	VersionShortId *string `cbor:"1,keyasint,omitempty" json:"versionShortId,omitempty"`
	DeletedSource  *string `cbor:"2,keyasint,omitempty" json:"deletedSource,omitempty"`
}

type CrudDeleteEnvVarResults struct {
	call rpc.Call
	data crudDeleteEnvVarResultsData
}

func (v *CrudDeleteEnvVarResults) SetVersionId(versionId string) {
	v.data.VersionId = &versionId
}

func (v *CrudDeleteEnvVarResults) SetVersionShortId(versionShortId string) {
	v.data.VersionShortId = &versionShortId
}

func (v *CrudDeleteEnvVarResults) SetDeletedSource(deletedSource string) {
	v.data.DeletedSource = &deletedSource
}

func (v *CrudDeleteEnvVarResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudDeleteEnvVarResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudDeleteEnvVarResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudDeleteEnvVarResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudRestartArgsData struct {
	App     *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Service *string `cbor:"1,keyasint,omitempty" json:"service,omitempty"`
}

type CrudRestartArgs struct {
	call rpc.Call
	data crudRestartArgsData
}

func (v *CrudRestartArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *CrudRestartArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *CrudRestartArgs) HasService() bool {
	return v.data.Service != nil
}

func (v *CrudRestartArgs) Service() string {
	if v.data.Service == nil {
		return ""
	}
	return *v.data.Service
}

func (v *CrudRestartArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudRestartArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudRestartArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudRestartArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type crudRestartResultsData struct {
	RestartedPools   *int32 `cbor:"0,keyasint,omitempty" json:"restarted_pools,omitempty"`
	StoppedSandboxes *int32 `cbor:"1,keyasint,omitempty" json:"stopped_sandboxes,omitempty"`
}

type CrudRestartResults struct {
	call rpc.Call
	data crudRestartResultsData
}

func (v *CrudRestartResults) SetRestartedPools(restarted_pools int32) {
	v.data.RestartedPools = &restarted_pools
}

func (v *CrudRestartResults) SetStoppedSandboxes(stopped_sandboxes int32) {
	v.data.StoppedSandboxes = &stopped_sandboxes
}

func (v *CrudRestartResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *CrudRestartResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *CrudRestartResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *CrudRestartResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type CrudNew struct {
	rpc.Call
	args    CrudNewArgs
	results CrudNewResults
}

func (t *CrudNew) Args() *CrudNewArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudNew) Results() *CrudNewResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudSetConfiguration struct {
	rpc.Call
	args    CrudSetConfigurationArgs
	results CrudSetConfigurationResults
}

func (t *CrudSetConfiguration) Args() *CrudSetConfigurationArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudSetConfiguration) Results() *CrudSetConfigurationResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudGetConfiguration struct {
	rpc.Call
	args    CrudGetConfigurationArgs
	results CrudGetConfigurationResults
}

func (t *CrudGetConfiguration) Args() *CrudGetConfigurationArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudGetConfiguration) Results() *CrudGetConfigurationResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudSetHost struct {
	rpc.Call
	args    CrudSetHostArgs
	results CrudSetHostResults
}

func (t *CrudSetHost) Args() *CrudSetHostArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudSetHost) Results() *CrudSetHostResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudSetWorkloadRole struct {
	rpc.Call
	args    CrudSetWorkloadRoleArgs
	results CrudSetWorkloadRoleResults
}

func (t *CrudSetWorkloadRole) Args() *CrudSetWorkloadRoleArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudSetWorkloadRole) Results() *CrudSetWorkloadRoleResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudList struct {
	rpc.Call
	args    CrudListArgs
	results CrudListResults
}

func (t *CrudList) Args() *CrudListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudList) Results() *CrudListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudDestroy struct {
	rpc.Call
	args    CrudDestroyArgs
	results CrudDestroyResults
}

func (t *CrudDestroy) Args() *CrudDestroyArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudDestroy) Results() *CrudDestroyResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudSetEnvVar struct {
	rpc.Call
	args    CrudSetEnvVarArgs
	results CrudSetEnvVarResults
}

func (t *CrudSetEnvVar) Args() *CrudSetEnvVarArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudSetEnvVar) Results() *CrudSetEnvVarResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudSetEnvVars struct {
	rpc.Call
	args    CrudSetEnvVarsArgs
	results CrudSetEnvVarsResults
}

func (t *CrudSetEnvVars) Args() *CrudSetEnvVarsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudSetEnvVars) Results() *CrudSetEnvVarsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudSetInitialEnvVars struct {
	rpc.Call
	args    CrudSetInitialEnvVarsArgs
	results CrudSetInitialEnvVarsResults
}

func (t *CrudSetInitialEnvVars) Args() *CrudSetInitialEnvVarsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudSetInitialEnvVars) Results() *CrudSetInitialEnvVarsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudDeleteEnvVar struct {
	rpc.Call
	args    CrudDeleteEnvVarArgs
	results CrudDeleteEnvVarResults
}

func (t *CrudDeleteEnvVar) Args() *CrudDeleteEnvVarArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudDeleteEnvVar) Results() *CrudDeleteEnvVarResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type CrudRestart struct {
	rpc.Call
	args    CrudRestartArgs
	results CrudRestartResults
}

func (t *CrudRestart) Args() *CrudRestartArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *CrudRestart) Results() *CrudRestartResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Crud interface {
	New(ctx context.Context, state *CrudNew) error
	SetConfiguration(ctx context.Context, state *CrudSetConfiguration) error
	GetConfiguration(ctx context.Context, state *CrudGetConfiguration) error
	SetHost(ctx context.Context, state *CrudSetHost) error
	SetWorkloadRole(ctx context.Context, state *CrudSetWorkloadRole) error
	List(ctx context.Context, state *CrudList) error
	Destroy(ctx context.Context, state *CrudDestroy) error
	SetEnvVar(ctx context.Context, state *CrudSetEnvVar) error
	SetEnvVars(ctx context.Context, state *CrudSetEnvVars) error
	SetInitialEnvVars(ctx context.Context, state *CrudSetInitialEnvVars) error
	DeleteEnvVar(ctx context.Context, state *CrudDeleteEnvVar) error
	Restart(ctx context.Context, state *CrudRestart) error
}

type reexportCrud struct {
	client rpc.Client
}

func (reexportCrud) New(ctx context.Context, state *CrudNew) error {
	panic("not implemented")
}

func (reexportCrud) SetConfiguration(ctx context.Context, state *CrudSetConfiguration) error {
	panic("not implemented")
}

func (reexportCrud) GetConfiguration(ctx context.Context, state *CrudGetConfiguration) error {
	panic("not implemented")
}

func (reexportCrud) SetHost(ctx context.Context, state *CrudSetHost) error {
	panic("not implemented")
}

func (reexportCrud) SetWorkloadRole(ctx context.Context, state *CrudSetWorkloadRole) error {
	panic("not implemented")
}

func (reexportCrud) List(ctx context.Context, state *CrudList) error {
	panic("not implemented")
}

func (reexportCrud) Destroy(ctx context.Context, state *CrudDestroy) error {
	panic("not implemented")
}

func (reexportCrud) SetEnvVar(ctx context.Context, state *CrudSetEnvVar) error {
	panic("not implemented")
}

func (reexportCrud) SetEnvVars(ctx context.Context, state *CrudSetEnvVars) error {
	panic("not implemented")
}

func (reexportCrud) SetInitialEnvVars(ctx context.Context, state *CrudSetInitialEnvVars) error {
	panic("not implemented")
}

func (reexportCrud) DeleteEnvVar(ctx context.Context, state *CrudDeleteEnvVar) error {
	panic("not implemented")
}

func (reexportCrud) Restart(ctx context.Context, state *CrudRestart) error {
	panic("not implemented")
}

func (t reexportCrud) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptCrud(t Crud) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "new",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"name"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "POST",
				Path:       "/api/v1/apps",
				Body:       "*",
				PathParams: []string{},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.New(ctx, &CrudNew{Call: call})
			},
		},
		{
			Name:          "setConfiguration",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "configuration"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "PUT",
				Path:       "/api/v1/apps/{app}/config",
				Body:       "*",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetConfiguration(ctx, &CrudSetConfiguration{Call: call})
			},
		},
		{
			Name:          "getConfiguration",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"app"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/apps/{app}/config",
				Body:       "",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetConfiguration(ctx, &CrudGetConfiguration{Call: call})
			},
		},
		{
			Name:          "setHost",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "host"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "PUT",
				Path:       "/api/v1/apps/{app}/host",
				Body:       "*",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetHost(ctx, &CrudSetHost{Call: call})
			},
		},
		{
			Name:          "setWorkloadRole",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "role"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetWorkloadRole(ctx, &CrudSetWorkloadRole{Call: call})
			},
		},
		{
			Name:          "list",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/apps",
				Body:       "",
				PathParams: []string{},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &CrudList{Call: call})
			},
		},
		{
			Name:          "destroy",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"name"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "DELETE",
				Path:       "/api/v1/apps/{name}",
				Body:       "",
				PathParams: []string{"name"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Destroy(ctx, &CrudDestroy{Call: call})
			},
		},
		{
			Name:          "setEnvVar",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "key", "value", "sensitive", "service"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "PUT",
				Path:       "/api/v1/apps/{app}/env/{key}",
				Body:       "*",
				PathParams: []string{"app", "key"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetEnvVar(ctx, &CrudSetEnvVar{Call: call})
			},
		},
		{
			Name:          "setEnvVars",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "vars", "service"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "PUT",
				Path:       "/api/v1/apps/{app}/env",
				Body:       "*",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetEnvVars(ctx, &CrudSetEnvVars{Call: call})
			},
		},
		{
			Name:          "setInitialEnvVars",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "vars", "service"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "POST",
				Path:       "/api/v1/apps/{app}/initial-env",
				Body:       "*",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SetInitialEnvVars(ctx, &CrudSetInitialEnvVars{Call: call})
			},
		},
		{
			Name:          "deleteEnvVar",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "key", "service"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "DELETE",
				Path:       "/api/v1/apps/{app}/env/{key}",
				Body:       "",
				PathParams: []string{"app", "key"},
				Query: []rpc.HTTPParam{
					{Name: "service", Kind: "string"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.DeleteEnvVar(ctx, &CrudDeleteEnvVar{Call: call})
			},
		},
		{
			Name:          "restart",
			InterfaceName: "Crud",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "service"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "POST",
				Path:       "/api/v1/apps/{app}/restart",
				Body:       "*",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Restart(ctx, &CrudRestart{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type CrudClient struct {
	rpc.Client
}

func NewCrudClient(client rpc.Client) *CrudClient {
	return &CrudClient{Client: client}
}

func (c CrudClient) Export() Crud {
	return reexportCrud{client: c.Client}
}

type CrudClientNewResults struct {
	client rpc.Client
	data   crudNewResultsData
}

func (v *CrudClientNewResults) HasId() bool {
	return v.data.Id != nil
}

func (v *CrudClientNewResults) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v CrudClient) New(ctx context.Context, name string) (*CrudClientNewResults, error) {
	args := CrudNewArgs{}
	args.data.Name = &name

	var ret crudNewResultsData

	err := v.Call(ctx, "new", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientNewResults{client: v.Client, data: ret}, nil
}

type CrudClientSetConfigurationResults struct {
	client rpc.Client
	data   crudSetConfigurationResultsData
}

func (v *CrudClientSetConfigurationResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *CrudClientSetConfigurationResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v *CrudClientSetConfigurationResults) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *CrudClientSetConfigurationResults) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v CrudClient) SetConfiguration(ctx context.Context, app string, configuration *Configuration) (*CrudClientSetConfigurationResults, error) {
	args := CrudSetConfigurationArgs{}
	args.data.App = &app
	args.data.Configuration = configuration

	var ret crudSetConfigurationResultsData

	err := v.Call(ctx, "setConfiguration", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientSetConfigurationResults{client: v.Client, data: ret}, nil
}

type CrudClientGetConfigurationResults struct {
	client rpc.Client
	data   crudGetConfigurationResultsData
}

func (v *CrudClientGetConfigurationResults) HasConfiguration() bool {
	return v.data.Configuration != nil
}

func (v *CrudClientGetConfigurationResults) Configuration() *Configuration {
	return v.data.Configuration
}

func (v *CrudClientGetConfigurationResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *CrudClientGetConfigurationResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v *CrudClientGetConfigurationResults) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *CrudClientGetConfigurationResults) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v *CrudClientGetConfigurationResults) HasWorkloadRole() bool {
	return v.data.WorkloadRole != nil
}

func (v *CrudClientGetConfigurationResults) WorkloadRole() string {
	if v.data.WorkloadRole == nil {
		return ""
	}
	return *v.data.WorkloadRole
}

func (v CrudClient) GetConfiguration(ctx context.Context, app string) (*CrudClientGetConfigurationResults, error) {
	args := CrudGetConfigurationArgs{}
	args.data.App = &app

	var ret crudGetConfigurationResultsData

	err := v.Call(ctx, "getConfiguration", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientGetConfigurationResults{client: v.Client, data: ret}, nil
}

type CrudClientSetHostResults struct {
	client rpc.Client
	data   crudSetHostResultsData
}

func (v CrudClient) SetHost(ctx context.Context, app string, host string) (*CrudClientSetHostResults, error) {
	args := CrudSetHostArgs{}
	args.data.App = &app
	args.data.Host = &host

	var ret crudSetHostResultsData

	err := v.Call(ctx, "setHost", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientSetHostResults{client: v.Client, data: ret}, nil
}

type CrudClientSetWorkloadRoleResults struct {
	client rpc.Client
	data   crudSetWorkloadRoleResultsData
}

func (v CrudClient) SetWorkloadRole(ctx context.Context, app string, role string) (*CrudClientSetWorkloadRoleResults, error) {
	args := CrudSetWorkloadRoleArgs{}
	args.data.App = &app
	args.data.Role = &role

	var ret crudSetWorkloadRoleResultsData

	err := v.Call(ctx, "setWorkloadRole", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientSetWorkloadRoleResults{client: v.Client, data: ret}, nil
}

type CrudClientListResults struct {
	client rpc.Client
	data   crudListResultsData
}

func (v *CrudClientListResults) HasApps() bool {
	return v.data.Apps != nil
}

func (v *CrudClientListResults) Apps() []*AppInfo {
	if v.data.Apps == nil {
		return nil
	}
	return *v.data.Apps
}

func (v CrudClient) List(ctx context.Context) (*CrudClientListResults, error) {
	args := CrudListArgs{}

	var ret crudListResultsData

	err := v.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientListResults{client: v.Client, data: ret}, nil
}

type CrudClientDestroyResults struct {
	client rpc.Client
	data   crudDestroyResultsData
}

func (v CrudClient) Destroy(ctx context.Context, name string) (*CrudClientDestroyResults, error) {
	args := CrudDestroyArgs{}
	args.data.Name = &name

	var ret crudDestroyResultsData

	err := v.Call(ctx, "destroy", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientDestroyResults{client: v.Client, data: ret}, nil
}

type CrudClientSetEnvVarResults struct {
	client rpc.Client
	data   crudSetEnvVarResultsData
}

func (v *CrudClientSetEnvVarResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *CrudClientSetEnvVarResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v *CrudClientSetEnvVarResults) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *CrudClientSetEnvVarResults) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v CrudClient) SetEnvVar(ctx context.Context, app string, key string, value string, sensitive bool, service string) (*CrudClientSetEnvVarResults, error) {
	args := CrudSetEnvVarArgs{}
	args.data.App = &app
	args.data.Key = &key
	args.data.Value = &value
	args.data.Sensitive = &sensitive
	args.data.Service = &service

	var ret crudSetEnvVarResultsData

	err := v.Call(ctx, "setEnvVar", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientSetEnvVarResults{client: v.Client, data: ret}, nil
}

type CrudClientSetEnvVarsResults struct {
	client rpc.Client
	data   crudSetEnvVarsResultsData
}

func (v *CrudClientSetEnvVarsResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *CrudClientSetEnvVarsResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v *CrudClientSetEnvVarsResults) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *CrudClientSetEnvVarsResults) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v CrudClient) SetEnvVars(ctx context.Context, app string, vars []*NamedValue, service string) (*CrudClientSetEnvVarsResults, error) {
	args := CrudSetEnvVarsArgs{}
	args.data.App = &app
	x := slices.Clone(vars)
	args.data.Vars = &x
	args.data.Service = &service

	var ret crudSetEnvVarsResultsData

	err := v.Call(ctx, "setEnvVars", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientSetEnvVarsResults{client: v.Client, data: ret}, nil
}

type CrudClientSetInitialEnvVarsResults struct {
	client rpc.Client
	data   crudSetInitialEnvVarsResultsData
}

func (v *CrudClientSetInitialEnvVarsResults) HasConfigVersionId() bool {
	return v.data.ConfigVersionId != nil
}

func (v *CrudClientSetInitialEnvVarsResults) ConfigVersionId() string {
	if v.data.ConfigVersionId == nil {
		return ""
	}
	return *v.data.ConfigVersionId
}

func (v CrudClient) SetInitialEnvVars(ctx context.Context, app string, vars []*NamedValue, service string) (*CrudClientSetInitialEnvVarsResults, error) {
	args := CrudSetInitialEnvVarsArgs{}
	args.data.App = &app
	x := slices.Clone(vars)
	args.data.Vars = &x
	args.data.Service = &service

	var ret crudSetInitialEnvVarsResultsData

	err := v.Call(ctx, "setInitialEnvVars", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientSetInitialEnvVarsResults{client: v.Client, data: ret}, nil
}

type CrudClientDeleteEnvVarResults struct {
	client rpc.Client
	data   crudDeleteEnvVarResultsData
}

func (v *CrudClientDeleteEnvVarResults) HasVersionId() bool {
	return v.data.VersionId != nil
}

func (v *CrudClientDeleteEnvVarResults) VersionId() string {
	if v.data.VersionId == nil {
		return ""
	}
	return *v.data.VersionId
}

func (v *CrudClientDeleteEnvVarResults) HasVersionShortId() bool {
	return v.data.VersionShortId != nil
}

func (v *CrudClientDeleteEnvVarResults) VersionShortId() string {
	if v.data.VersionShortId == nil {
		return ""
	}
	return *v.data.VersionShortId
}

func (v *CrudClientDeleteEnvVarResults) HasDeletedSource() bool {
	return v.data.DeletedSource != nil
}

func (v *CrudClientDeleteEnvVarResults) DeletedSource() string {
	if v.data.DeletedSource == nil {
		return ""
	}
	return *v.data.DeletedSource
}

func (v CrudClient) DeleteEnvVar(ctx context.Context, app string, key string, service string) (*CrudClientDeleteEnvVarResults, error) {
	args := CrudDeleteEnvVarArgs{}
	args.data.App = &app
	args.data.Key = &key
	args.data.Service = &service

	var ret crudDeleteEnvVarResultsData

	err := v.Call(ctx, "deleteEnvVar", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientDeleteEnvVarResults{client: v.Client, data: ret}, nil
}

type CrudClientRestartResults struct {
	client rpc.Client
	data   crudRestartResultsData
}

func (v *CrudClientRestartResults) HasRestartedPools() bool {
	return v.data.RestartedPools != nil
}

func (v *CrudClientRestartResults) RestartedPools() int32 {
	if v.data.RestartedPools == nil {
		return 0
	}
	return *v.data.RestartedPools
}

func (v *CrudClientRestartResults) HasStoppedSandboxes() bool {
	return v.data.StoppedSandboxes != nil
}

func (v *CrudClientRestartResults) StoppedSandboxes() int32 {
	if v.data.StoppedSandboxes == nil {
		return 0
	}
	return *v.data.StoppedSandboxes
}

func (v CrudClient) Restart(ctx context.Context, app string, service string) (*CrudClientRestartResults, error) {
	args := CrudRestartArgs{}
	args.data.App = &app
	args.data.Service = &service

	var ret crudRestartResultsData

	err := v.Call(ctx, "restart", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &CrudClientRestartResults{client: v.Client, data: ret}, nil
}

type userQueryWhoAmIArgsData struct{}

type UserQueryWhoAmIArgs struct {
	call rpc.Call
	data userQueryWhoAmIArgsData
}

func (v *UserQueryWhoAmIArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UserQueryWhoAmIArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UserQueryWhoAmIArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UserQueryWhoAmIArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type userQueryWhoAmIResultsData struct {
	Info *UserInfo `cbor:"0,keyasint,omitempty" json:"info,omitempty"`
}

type UserQueryWhoAmIResults struct {
	call rpc.Call
	data userQueryWhoAmIResultsData
}

func (v *UserQueryWhoAmIResults) SetInfo(info *UserInfo) {
	v.data.Info = info
}

func (v *UserQueryWhoAmIResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *UserQueryWhoAmIResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *UserQueryWhoAmIResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *UserQueryWhoAmIResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type UserQueryWhoAmI struct {
	rpc.Call
	args    UserQueryWhoAmIArgs
	results UserQueryWhoAmIResults
}

func (t *UserQueryWhoAmI) Args() *UserQueryWhoAmIArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *UserQueryWhoAmI) Results() *UserQueryWhoAmIResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type UserQuery interface {
	WhoAmI(ctx context.Context, state *UserQueryWhoAmI) error
}

type reexportUserQuery struct {
	client rpc.Client
}

func (reexportUserQuery) WhoAmI(ctx context.Context, state *UserQueryWhoAmI) error {
	panic("not implemented")
}

func (t reexportUserQuery) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptUserQuery(t UserQuery) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "whoAmI",
			InterfaceName: "UserQuery",
			Index:         0,
			Public:        false,
			Params:        []string{},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.WhoAmI(ctx, &UserQueryWhoAmI{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type UserQueryClient struct {
	rpc.Client
}

func NewUserQueryClient(client rpc.Client) *UserQueryClient {
	return &UserQueryClient{Client: client}
}

func (c UserQueryClient) Export() UserQuery {
	return reexportUserQuery{client: c.Client}
}

type UserQueryClientWhoAmIResults struct {
	client rpc.Client
	data   userQueryWhoAmIResultsData
}

func (v *UserQueryClientWhoAmIResults) HasInfo() bool {
	return v.data.Info != nil
}

func (v *UserQueryClientWhoAmIResults) Info() *UserInfo {
	return v.data.Info
}

func (v UserQueryClient) WhoAmI(ctx context.Context) (*UserQueryClientWhoAmIResults, error) {
	args := UserQueryWhoAmIArgs{}

	var ret userQueryWhoAmIResultsData

	err := v.Call(ctx, "whoAmI", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &UserQueryClientWhoAmIResults{client: v.Client, data: ret}, nil
}

type appStatusAppInfoArgsData struct {
	Application *string `cbor:"0,keyasint,omitempty" json:"application,omitempty"`
}

type AppStatusAppInfoArgs struct {
	call rpc.Call
	data appStatusAppInfoArgsData
}

func (v *AppStatusAppInfoArgs) HasApplication() bool {
	return v.data.Application != nil
}

func (v *AppStatusAppInfoArgs) Application() string {
	if v.data.Application == nil {
		return ""
	}
	return *v.data.Application
}

func (v *AppStatusAppInfoArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AppStatusAppInfoArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AppStatusAppInfoArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AppStatusAppInfoArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type appStatusAppInfoResultsData struct {
	Status *ApplicationStatus `cbor:"0,keyasint,omitempty" json:"status,omitempty"`
}

type AppStatusAppInfoResults struct {
	call rpc.Call
	data appStatusAppInfoResultsData
}

func (v *AppStatusAppInfoResults) SetStatus(status *ApplicationStatus) {
	v.data.Status = status
}

func (v *AppStatusAppInfoResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AppStatusAppInfoResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AppStatusAppInfoResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AppStatusAppInfoResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type AppStatusAppInfo struct {
	rpc.Call
	args    AppStatusAppInfoArgs
	results AppStatusAppInfoResults
}

func (t *AppStatusAppInfo) Args() *AppStatusAppInfoArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AppStatusAppInfo) Results() *AppStatusAppInfoResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AppStatus interface {
	AppInfo(ctx context.Context, state *AppStatusAppInfo) error
}

type reexportAppStatus struct {
	client rpc.Client
}

func (reexportAppStatus) AppInfo(ctx context.Context, state *AppStatusAppInfo) error {
	panic("not implemented")
}

func (t reexportAppStatus) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptAppStatus(t AppStatus) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "appInfo",
			InterfaceName: "AppStatus",
			Index:         0,
			Public:        false,
			Params:        []string{"application"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/apps/{application}/status",
				Body:       "",
				PathParams: []string{"application"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.AppInfo(ctx, &AppStatusAppInfo{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type AppStatusClient struct {
	rpc.Client
}

func NewAppStatusClient(client rpc.Client) *AppStatusClient {
	return &AppStatusClient{Client: client}
}

func (c AppStatusClient) Export() AppStatus {
	return reexportAppStatus{client: c.Client}
}

type AppStatusClientAppInfoResults struct {
	client rpc.Client
	data   appStatusAppInfoResultsData
}

func (v *AppStatusClientAppInfoResults) HasStatus() bool {
	return v.data.Status != nil
}

func (v *AppStatusClientAppInfoResults) Status() *ApplicationStatus {
	return v.data.Status
}

func (v AppStatusClient) AppInfo(ctx context.Context, application string) (*AppStatusClientAppInfoResults, error) {
	args := AppStatusAppInfoArgs{}
	args.data.Application = &application

	var ret appStatusAppInfoResultsData

	err := v.Call(ctx, "appInfo", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AppStatusClientAppInfoResults{client: v.Client, data: ret}, nil
}

type logsAppLogsArgsData struct {
	Application *string             `cbor:"0,keyasint,omitempty" json:"application,omitempty"`
	From        *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"from,omitempty"`
	Follow      *bool               `cbor:"2,keyasint,omitempty" json:"follow,omitempty"`
}

type LogsAppLogsArgs struct {
	call rpc.Call
	data logsAppLogsArgsData
}

func (v *LogsAppLogsArgs) HasApplication() bool {
	return v.data.Application != nil
}

func (v *LogsAppLogsArgs) Application() string {
	if v.data.Application == nil {
		return ""
	}
	return *v.data.Application
}

func (v *LogsAppLogsArgs) HasFrom() bool {
	return v.data.From != nil
}

func (v *LogsAppLogsArgs) From() *standard.Timestamp {
	return v.data.From
}

func (v *LogsAppLogsArgs) HasFollow() bool {
	return v.data.Follow != nil
}

func (v *LogsAppLogsArgs) Follow() bool {
	if v.data.Follow == nil {
		return false
	}
	return *v.data.Follow
}

func (v *LogsAppLogsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsAppLogsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsAppLogsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsAppLogsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logsAppLogsResultsData struct {
	Logs *[]*LogEntry `cbor:"0,keyasint,omitempty" json:"logs,omitempty"`
}

type LogsAppLogsResults struct {
	call rpc.Call
	data logsAppLogsResultsData
}

func (v *LogsAppLogsResults) SetLogs(logs []*LogEntry) {
	x := slices.Clone(logs)
	v.data.Logs = &x
}

func (v *LogsAppLogsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsAppLogsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsAppLogsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsAppLogsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logsSandboxLogsArgsData struct {
	Sandbox *string             `cbor:"0,keyasint,omitempty" json:"sandbox,omitempty"`
	From    *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"from,omitempty"`
	Follow  *bool               `cbor:"2,keyasint,omitempty" json:"follow,omitempty"`
}

type LogsSandboxLogsArgs struct {
	call rpc.Call
	data logsSandboxLogsArgsData
}

func (v *LogsSandboxLogsArgs) HasSandbox() bool {
	return v.data.Sandbox != nil
}

func (v *LogsSandboxLogsArgs) Sandbox() string {
	if v.data.Sandbox == nil {
		return ""
	}
	return *v.data.Sandbox
}

func (v *LogsSandboxLogsArgs) HasFrom() bool {
	return v.data.From != nil
}

func (v *LogsSandboxLogsArgs) From() *standard.Timestamp {
	return v.data.From
}

func (v *LogsSandboxLogsArgs) HasFollow() bool {
	return v.data.Follow != nil
}

func (v *LogsSandboxLogsArgs) Follow() bool {
	if v.data.Follow == nil {
		return false
	}
	return *v.data.Follow
}

func (v *LogsSandboxLogsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsSandboxLogsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsSandboxLogsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsSandboxLogsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logsSandboxLogsResultsData struct {
	Logs *[]*LogEntry `cbor:"0,keyasint,omitempty" json:"logs,omitempty"`
}

type LogsSandboxLogsResults struct {
	call rpc.Call
	data logsSandboxLogsResultsData
}

func (v *LogsSandboxLogsResults) SetLogs(logs []*LogEntry) {
	x := slices.Clone(logs)
	v.data.Logs = &x
}

func (v *LogsSandboxLogsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsSandboxLogsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsSandboxLogsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsSandboxLogsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logsStreamLogsArgsData struct {
	Target *LogTarget          `cbor:"0,keyasint,omitempty" json:"target,omitempty"`
	From   *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"from,omitempty"`
	Follow *bool               `cbor:"2,keyasint,omitempty" json:"follow,omitempty"`
	Logs   *rpc.Capability     `cbor:"3,keyasint,omitempty" json:"logs,omitempty"`
}

type LogsStreamLogsArgs struct {
	call rpc.Call
	data logsStreamLogsArgsData
}

func (v *LogsStreamLogsArgs) HasTarget() bool {
	return v.data.Target != nil
}

func (v *LogsStreamLogsArgs) Target() *LogTarget {
	return v.data.Target
}

func (v *LogsStreamLogsArgs) HasFrom() bool {
	return v.data.From != nil
}

func (v *LogsStreamLogsArgs) From() *standard.Timestamp {
	return v.data.From
}

func (v *LogsStreamLogsArgs) HasFollow() bool {
	return v.data.Follow != nil
}

func (v *LogsStreamLogsArgs) Follow() bool {
	if v.data.Follow == nil {
		return false
	}
	return *v.data.Follow
}

func (v *LogsStreamLogsArgs) HasLogs() bool {
	return v.data.Logs != nil
}

func (v *LogsStreamLogsArgs) Logs() *stream.SendStreamClient[*LogEntry] {
	if v.data.Logs == nil {
		return nil
	}
	return &stream.SendStreamClient[*LogEntry]{Client: v.call.NewClient(v.data.Logs)}
}

func (v *LogsStreamLogsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsStreamLogsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsStreamLogsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsStreamLogsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logsStreamLogsResultsData struct{}

type LogsStreamLogsResults struct {
	call rpc.Call
	data logsStreamLogsResultsData
}

func (v *LogsStreamLogsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsStreamLogsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsStreamLogsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsStreamLogsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logsStreamLogChunksArgsData struct {
	Target *LogTarget          `cbor:"0,keyasint,omitempty" json:"target,omitempty"`
	From   *standard.Timestamp `cbor:"1,keyasint,omitempty" json:"from,omitempty"`
	Follow *bool               `cbor:"2,keyasint,omitempty" json:"follow,omitempty"`
	Filter *string             `cbor:"3,keyasint,omitempty" json:"filter,omitempty"`
	Chunks *rpc.Capability     `cbor:"4,keyasint,omitempty" json:"chunks,omitempty"`
	To     *standard.Timestamp `cbor:"5,keyasint,omitempty" json:"to,omitempty"`
}

type LogsStreamLogChunksArgs struct {
	call rpc.Call
	data logsStreamLogChunksArgsData
}

func (v *LogsStreamLogChunksArgs) HasTarget() bool {
	return v.data.Target != nil
}

func (v *LogsStreamLogChunksArgs) Target() *LogTarget {
	return v.data.Target
}

func (v *LogsStreamLogChunksArgs) HasFrom() bool {
	return v.data.From != nil
}

func (v *LogsStreamLogChunksArgs) From() *standard.Timestamp {
	return v.data.From
}

func (v *LogsStreamLogChunksArgs) HasFollow() bool {
	return v.data.Follow != nil
}

func (v *LogsStreamLogChunksArgs) Follow() bool {
	if v.data.Follow == nil {
		return false
	}
	return *v.data.Follow
}

func (v *LogsStreamLogChunksArgs) HasFilter() bool {
	return v.data.Filter != nil
}

func (v *LogsStreamLogChunksArgs) Filter() string {
	if v.data.Filter == nil {
		return ""
	}
	return *v.data.Filter
}

func (v *LogsStreamLogChunksArgs) HasChunks() bool {
	return v.data.Chunks != nil
}

func (v *LogsStreamLogChunksArgs) Chunks() *stream.SendStreamClient[*LogChunk] {
	if v.data.Chunks == nil {
		return nil
	}
	return &stream.SendStreamClient[*LogChunk]{Client: v.call.NewClient(v.data.Chunks)}
}

func (v *LogsStreamLogChunksArgs) HasTo() bool {
	return v.data.To != nil
}

func (v *LogsStreamLogChunksArgs) To() *standard.Timestamp {
	return v.data.To
}

func (v *LogsStreamLogChunksArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsStreamLogChunksArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsStreamLogChunksArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsStreamLogChunksArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type logsStreamLogChunksResultsData struct{}

type LogsStreamLogChunksResults struct {
	call rpc.Call
	data logsStreamLogChunksResultsData
}

func (v *LogsStreamLogChunksResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *LogsStreamLogChunksResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *LogsStreamLogChunksResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *LogsStreamLogChunksResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type LogsAppLogs struct {
	rpc.Call
	args    LogsAppLogsArgs
	results LogsAppLogsResults
}

func (t *LogsAppLogs) Args() *LogsAppLogsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *LogsAppLogs) Results() *LogsAppLogsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type LogsSandboxLogs struct {
	rpc.Call
	args    LogsSandboxLogsArgs
	results LogsSandboxLogsResults
}

func (t *LogsSandboxLogs) Args() *LogsSandboxLogsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *LogsSandboxLogs) Results() *LogsSandboxLogsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type LogsStreamLogs struct {
	rpc.Call
	args    LogsStreamLogsArgs
	results LogsStreamLogsResults
}

func (t *LogsStreamLogs) Args() *LogsStreamLogsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *LogsStreamLogs) Results() *LogsStreamLogsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type LogsStreamLogChunks struct {
	rpc.Call
	args    LogsStreamLogChunksArgs
	results LogsStreamLogChunksResults
}

func (t *LogsStreamLogChunks) Args() *LogsStreamLogChunksArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *LogsStreamLogChunks) Results() *LogsStreamLogChunksResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Logs interface {
	AppLogs(ctx context.Context, state *LogsAppLogs) error
	SandboxLogs(ctx context.Context, state *LogsSandboxLogs) error
	StreamLogs(ctx context.Context, state *LogsStreamLogs) error
	StreamLogChunks(ctx context.Context, state *LogsStreamLogChunks) error
}

type reexportLogs struct {
	client rpc.Client
}

func (reexportLogs) AppLogs(ctx context.Context, state *LogsAppLogs) error {
	panic("not implemented")
}

func (reexportLogs) SandboxLogs(ctx context.Context, state *LogsSandboxLogs) error {
	panic("not implemented")
}

func (reexportLogs) StreamLogs(ctx context.Context, state *LogsStreamLogs) error {
	panic("not implemented")
}

func (reexportLogs) StreamLogChunks(ctx context.Context, state *LogsStreamLogChunks) error {
	panic("not implemented")
}

func (t reexportLogs) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptLogs(t Logs) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "appLogs",
			InterfaceName: "Logs",
			Index:         0,
			Public:        false,
			Params:        []string{"application", "from", "follow"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/apps/{application}/logs",
				Body:       "",
				PathParams: []string{"application"},
				Query: []rpc.HTTPParam{
					{Name: "from", Kind: "timestamp"},
					{Name: "follow", Kind: "bool"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.AppLogs(ctx, &LogsAppLogs{Call: call})
			},
		},
		{
			Name:          "sandboxLogs",
			InterfaceName: "Logs",
			Index:         0,
			Public:        false,
			Params:        []string{"sandbox", "from", "follow"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/sandboxes/{sandbox}/logs",
				Body:       "",
				PathParams: []string{"sandbox"},
				Query: []rpc.HTTPParam{
					{Name: "from", Kind: "timestamp"},
					{Name: "follow", Kind: "bool"},
				},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.SandboxLogs(ctx, &LogsSandboxLogs{Call: call})
			},
		},
		{
			Name:          "streamLogs",
			InterfaceName: "Logs",
			Index:         0,
			Public:        false,
			Params:        []string{"target", "from", "follow", "logs"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.StreamLogs(ctx, &LogsStreamLogs{Call: call})
			},
		},
		{
			Name:          "streamLogChunks",
			InterfaceName: "Logs",
			Index:         0,
			Public:        false,
			Params:        []string{"target", "from", "follow", "filter", "chunks", "to"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.StreamLogChunks(ctx, &LogsStreamLogChunks{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type LogsClient struct {
	rpc.Client
}

func NewLogsClient(client rpc.Client) *LogsClient {
	return &LogsClient{Client: client}
}

func (c LogsClient) Export() Logs {
	return reexportLogs{client: c.Client}
}

type LogsClientAppLogsResults struct {
	client rpc.Client
	data   logsAppLogsResultsData
}

func (v *LogsClientAppLogsResults) HasLogs() bool {
	return v.data.Logs != nil
}

func (v *LogsClientAppLogsResults) Logs() []*LogEntry {
	if v.data.Logs == nil {
		return nil
	}
	return *v.data.Logs
}

func (v LogsClient) AppLogs(ctx context.Context, application string, from *standard.Timestamp, follow bool) (*LogsClientAppLogsResults, error) {
	args := LogsAppLogsArgs{}
	args.data.Application = &application
	args.data.From = from
	args.data.Follow = &follow

	var ret logsAppLogsResultsData

	err := v.Call(ctx, "appLogs", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &LogsClientAppLogsResults{client: v.Client, data: ret}, nil
}

type LogsClientSandboxLogsResults struct {
	client rpc.Client
	data   logsSandboxLogsResultsData
}

func (v *LogsClientSandboxLogsResults) HasLogs() bool {
	return v.data.Logs != nil
}

func (v *LogsClientSandboxLogsResults) Logs() []*LogEntry {
	if v.data.Logs == nil {
		return nil
	}
	return *v.data.Logs
}

func (v LogsClient) SandboxLogs(ctx context.Context, sandbox string, from *standard.Timestamp, follow bool) (*LogsClientSandboxLogsResults, error) {
	args := LogsSandboxLogsArgs{}
	args.data.Sandbox = &sandbox
	args.data.From = from
	args.data.Follow = &follow

	var ret logsSandboxLogsResultsData

	err := v.Call(ctx, "sandboxLogs", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &LogsClientSandboxLogsResults{client: v.Client, data: ret}, nil
}

type LogsClientStreamLogsResults struct {
	client rpc.Client
	data   logsStreamLogsResultsData
}

func (v LogsClient) StreamLogs(ctx context.Context, target *LogTarget, from *standard.Timestamp, follow bool, logs stream.SendStream[*LogEntry]) (*LogsClientStreamLogsResults, error) {
	args := LogsStreamLogsArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Target = target
	args.data.From = from
	args.data.Follow = &follow
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[*LogEntry](logs), logs)
		args.data.Logs = c
		caps[oid] = ic
	}

	var ret logsStreamLogsResultsData

	err := v.CallWithCaps(ctx, "streamLogs", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &LogsClientStreamLogsResults{client: v.Client, data: ret}, nil
}

type LogsClientStreamLogChunksResults struct {
	client rpc.Client
	data   logsStreamLogChunksResultsData
}

func (v LogsClient) StreamLogChunks(ctx context.Context, target *LogTarget, from *standard.Timestamp, follow bool, filter string, chunks stream.SendStream[*LogChunk], to *standard.Timestamp) (*LogsClientStreamLogChunksResults, error) {
	args := LogsStreamLogChunksArgs{}
	caps := map[rpc.OID]*rpc.InlineCapability{}
	args.data.Target = target
	args.data.From = from
	args.data.Follow = &follow
	args.data.Filter = &filter
	{
		ic, oid, c := v.NewInlineCapability(stream.AdaptSendStream[*LogChunk](chunks), chunks)
		args.data.Chunks = c
		caps[oid] = ic
	}
	args.data.To = to

	var ret logsStreamLogChunksResultsData

	err := v.CallWithCaps(ctx, "streamLogChunks", &args, &ret, caps)
	if err != nil {
		return nil, err
	}

	return &LogsClientStreamLogChunksResults{client: v.Client, data: ret}, nil
}

type disksNewArgsData struct {
	Name     *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Capacity *int64  `cbor:"1,keyasint,omitempty" json:"capacity,omitempty"`
}

type DisksNewArgs struct {
	call rpc.Call
	data disksNewArgsData
}

func (v *DisksNewArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *DisksNewArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *DisksNewArgs) HasCapacity() bool {
	return v.data.Capacity != nil
}

func (v *DisksNewArgs) Capacity() int64 {
	if v.data.Capacity == nil {
		return 0
	}
	return *v.data.Capacity
}

func (v *DisksNewArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksNewArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksNewArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksNewArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksNewResultsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DisksNewResults struct {
	call rpc.Call
	data disksNewResultsData
}

func (v *DisksNewResults) SetId(id string) {
	v.data.Id = &id
}

func (v *DisksNewResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksNewResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksNewResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksNewResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksGetByIdArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DisksGetByIdArgs struct {
	call rpc.Call
	data disksGetByIdArgsData
}

func (v *DisksGetByIdArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DisksGetByIdArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DisksGetByIdArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksGetByIdArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksGetByIdArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksGetByIdArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksGetByIdResultsData struct {
	Config *DiskConfig `cbor:"0,keyasint,omitempty" json:"config,omitempty"`
}

type DisksGetByIdResults struct {
	call rpc.Call
	data disksGetByIdResultsData
}

func (v *DisksGetByIdResults) SetConfig(config *DiskConfig) {
	v.data.Config = config
}

func (v *DisksGetByIdResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksGetByIdResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksGetByIdResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksGetByIdResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksGetByNameArgsData struct {
	Name *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
}

type DisksGetByNameArgs struct {
	call rpc.Call
	data disksGetByNameArgsData
}

func (v *DisksGetByNameArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *DisksGetByNameArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *DisksGetByNameArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksGetByNameArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksGetByNameArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksGetByNameArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksGetByNameResultsData struct {
	Config *DiskConfig `cbor:"0,keyasint,omitempty" json:"config,omitempty"`
}

type DisksGetByNameResults struct {
	call rpc.Call
	data disksGetByNameResultsData
}

func (v *DisksGetByNameResults) SetConfig(config *DiskConfig) {
	v.data.Config = config
}

func (v *DisksGetByNameResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksGetByNameResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksGetByNameResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksGetByNameResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksListArgsData struct{}

type DisksListArgs struct {
	call rpc.Call
	data disksListArgsData
}

func (v *DisksListArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksListArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksListArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksListArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksListResultsData struct {
	Disks *[]*DiskConfig `cbor:"0,keyasint,omitempty" json:"disks,omitempty"`
}

type DisksListResults struct {
	call rpc.Call
	data disksListResultsData
}

func (v *DisksListResults) SetDisks(disks []*DiskConfig) {
	x := slices.Clone(disks)
	v.data.Disks = &x
}

func (v *DisksListResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksListResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksListResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksListResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksDeleteArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type DisksDeleteArgs struct {
	call rpc.Call
	data disksDeleteArgsData
}

func (v *DisksDeleteArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *DisksDeleteArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *DisksDeleteArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksDeleteArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksDeleteArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksDeleteArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type disksDeleteResultsData struct{}

type DisksDeleteResults struct {
	call rpc.Call
	data disksDeleteResultsData
}

func (v *DisksDeleteResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *DisksDeleteResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *DisksDeleteResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *DisksDeleteResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type DisksNew struct {
	rpc.Call
	args    DisksNewArgs
	results DisksNewResults
}

func (t *DisksNew) Args() *DisksNewArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksNew) Results() *DisksNewResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DisksGetById struct {
	rpc.Call
	args    DisksGetByIdArgs
	results DisksGetByIdResults
}

func (t *DisksGetById) Args() *DisksGetByIdArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksGetById) Results() *DisksGetByIdResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DisksGetByName struct {
	rpc.Call
	args    DisksGetByNameArgs
	results DisksGetByNameResults
}

func (t *DisksGetByName) Args() *DisksGetByNameArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksGetByName) Results() *DisksGetByNameResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DisksList struct {
	rpc.Call
	args    DisksListArgs
	results DisksListResults
}

func (t *DisksList) Args() *DisksListArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksList) Results() *DisksListResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type DisksDelete struct {
	rpc.Call
	args    DisksDeleteArgs
	results DisksDeleteResults
}

func (t *DisksDelete) Args() *DisksDeleteArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *DisksDelete) Results() *DisksDeleteResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Disks interface {
	New(ctx context.Context, state *DisksNew) error
	GetById(ctx context.Context, state *DisksGetById) error
	GetByName(ctx context.Context, state *DisksGetByName) error
	List(ctx context.Context, state *DisksList) error
	Delete(ctx context.Context, state *DisksDelete) error
}

type reexportDisks struct {
	client rpc.Client
}

func (reexportDisks) New(ctx context.Context, state *DisksNew) error {
	panic("not implemented")
}

func (reexportDisks) GetById(ctx context.Context, state *DisksGetById) error {
	panic("not implemented")
}

func (reexportDisks) GetByName(ctx context.Context, state *DisksGetByName) error {
	panic("not implemented")
}

func (reexportDisks) List(ctx context.Context, state *DisksList) error {
	panic("not implemented")
}

func (reexportDisks) Delete(ctx context.Context, state *DisksDelete) error {
	panic("not implemented")
}

func (t reexportDisks) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptDisks(t Disks) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "new",
			InterfaceName: "Disks",
			Index:         0,
			Public:        false,
			Params:        []string{"name", "capacity"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "POST",
				Path:       "/api/v1/disks",
				Body:       "*",
				PathParams: []string{},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.New(ctx, &DisksNew{Call: call})
			},
		},
		{
			Name:          "getById",
			InterfaceName: "Disks",
			Index:         0,
			Public:        false,
			Params:        []string{"id"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/disks/{id}",
				Body:       "",
				PathParams: []string{"id"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetById(ctx, &DisksGetById{Call: call})
			},
		},
		{
			Name:          "getByName",
			InterfaceName: "Disks",
			Index:         0,
			Public:        false,
			Params:        []string{"name"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/disks/by-name/{name}",
				Body:       "",
				PathParams: []string{"name"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetByName(ctx, &DisksGetByName{Call: call})
			},
		},
		{
			Name:          "list",
			InterfaceName: "Disks",
			Index:         0,
			Public:        false,
			Params:        []string{},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/disks",
				Body:       "",
				PathParams: []string{},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.List(ctx, &DisksList{Call: call})
			},
		},
		{
			Name:          "delete",
			InterfaceName: "Disks",
			Index:         0,
			Public:        false,
			Params:        []string{"id"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "DELETE",
				Path:       "/api/v1/disks/{id}",
				Body:       "",
				PathParams: []string{"id"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.Delete(ctx, &DisksDelete{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type DisksClient struct {
	rpc.Client
}

func NewDisksClient(client rpc.Client) *DisksClient {
	return &DisksClient{Client: client}
}

func (c DisksClient) Export() Disks {
	return reexportDisks{client: c.Client}
}

type DisksClientNewResults struct {
	client rpc.Client
	data   disksNewResultsData
}

func (v *DisksClientNewResults) HasId() bool {
	return v.data.Id != nil
}

func (v *DisksClientNewResults) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v DisksClient) New(ctx context.Context, name string, capacity int64) (*DisksClientNewResults, error) {
	args := DisksNewArgs{}
	args.data.Name = &name
	args.data.Capacity = &capacity

	var ret disksNewResultsData

	err := v.Call(ctx, "new", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientNewResults{client: v.Client, data: ret}, nil
}

type DisksClientGetByIdResults struct {
	client rpc.Client
	data   disksGetByIdResultsData
}

func (v *DisksClientGetByIdResults) HasConfig() bool {
	return v.data.Config != nil
}

func (v *DisksClientGetByIdResults) Config() *DiskConfig {
	return v.data.Config
}

func (v DisksClient) GetById(ctx context.Context, id string) (*DisksClientGetByIdResults, error) {
	args := DisksGetByIdArgs{}
	args.data.Id = &id

	var ret disksGetByIdResultsData

	err := v.Call(ctx, "getById", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientGetByIdResults{client: v.Client, data: ret}, nil
}

type DisksClientGetByNameResults struct {
	client rpc.Client
	data   disksGetByNameResultsData
}

func (v *DisksClientGetByNameResults) HasConfig() bool {
	return v.data.Config != nil
}

func (v *DisksClientGetByNameResults) Config() *DiskConfig {
	return v.data.Config
}

func (v DisksClient) GetByName(ctx context.Context, name string) (*DisksClientGetByNameResults, error) {
	args := DisksGetByNameArgs{}
	args.data.Name = &name

	var ret disksGetByNameResultsData

	err := v.Call(ctx, "getByName", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientGetByNameResults{client: v.Client, data: ret}, nil
}

type DisksClientListResults struct {
	client rpc.Client
	data   disksListResultsData
}

func (v *DisksClientListResults) HasDisks() bool {
	return v.data.Disks != nil
}

func (v *DisksClientListResults) Disks() []*DiskConfig {
	if v.data.Disks == nil {
		return nil
	}
	return *v.data.Disks
}

func (v DisksClient) List(ctx context.Context) (*DisksClientListResults, error) {
	args := DisksListArgs{}

	var ret disksListResultsData

	err := v.Call(ctx, "list", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientListResults{client: v.Client, data: ret}, nil
}

type DisksClientDeleteResults struct {
	client rpc.Client
	data   disksDeleteResultsData
}

func (v DisksClient) Delete(ctx context.Context, id string) (*DisksClientDeleteResults, error) {
	args := DisksDeleteArgs{}
	args.data.Id = &id

	var ret disksDeleteResultsData

	err := v.Call(ctx, "delete", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &DisksClientDeleteResults{client: v.Client, data: ret}, nil
}

type addonsCreateInstanceArgsData struct {
	Name    *string `cbor:"0,keyasint,omitempty" json:"name,omitempty"`
	Addon   *string `cbor:"1,keyasint,omitempty" json:"addon,omitempty"`
	Variant *string `cbor:"2,keyasint,omitempty" json:"variant,omitempty"`
	App     *string `cbor:"3,keyasint,omitempty" json:"app,omitempty"`
	Version *string `cbor:"4,keyasint,omitempty" json:"version,omitempty"`
}

type AddonsCreateInstanceArgs struct {
	call rpc.Call
	data addonsCreateInstanceArgsData
}

func (v *AddonsCreateInstanceArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *AddonsCreateInstanceArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AddonsCreateInstanceArgs) HasAddon() bool {
	return v.data.Addon != nil
}

func (v *AddonsCreateInstanceArgs) Addon() string {
	if v.data.Addon == nil {
		return ""
	}
	return *v.data.Addon
}

func (v *AddonsCreateInstanceArgs) HasVariant() bool {
	return v.data.Variant != nil
}

func (v *AddonsCreateInstanceArgs) Variant() string {
	if v.data.Variant == nil {
		return ""
	}
	return *v.data.Variant
}

func (v *AddonsCreateInstanceArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AddonsCreateInstanceArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AddonsCreateInstanceArgs) HasVersion() bool {
	return v.data.Version != nil
}

func (v *AddonsCreateInstanceArgs) Version() string {
	if v.data.Version == nil {
		return ""
	}
	return *v.data.Version
}

func (v *AddonsCreateInstanceArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsCreateInstanceArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsCreateInstanceArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsCreateInstanceArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsCreateInstanceResultsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type AddonsCreateInstanceResults struct {
	call rpc.Call
	data addonsCreateInstanceResultsData
}

func (v *AddonsCreateInstanceResults) SetId(id string) {
	v.data.Id = &id
}

func (v *AddonsCreateInstanceResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsCreateInstanceResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsCreateInstanceResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsCreateInstanceResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsListInstancesArgsData struct {
	App *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
}

type AddonsListInstancesArgs struct {
	call rpc.Call
	data addonsListInstancesArgsData
}

func (v *AddonsListInstancesArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AddonsListInstancesArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AddonsListInstancesArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsListInstancesArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsListInstancesArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsListInstancesArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsListInstancesResultsData struct {
	Addons *[]*AddonInstance `cbor:"0,keyasint,omitempty" json:"addons,omitempty"`
}

type AddonsListInstancesResults struct {
	call rpc.Call
	data addonsListInstancesResultsData
}

func (v *AddonsListInstancesResults) SetAddons(addons []*AddonInstance) {
	x := slices.Clone(addons)
	v.data.Addons = &x
}

func (v *AddonsListInstancesResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsListInstancesResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsListInstancesResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsListInstancesResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsRotateCredentialArgsData struct {
	App        *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Name       *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
	Credential *string `cbor:"2,keyasint,omitempty" json:"credential,omitempty"`
}

type AddonsRotateCredentialArgs struct {
	call rpc.Call
	data addonsRotateCredentialArgsData
}

func (v *AddonsRotateCredentialArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AddonsRotateCredentialArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AddonsRotateCredentialArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *AddonsRotateCredentialArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AddonsRotateCredentialArgs) HasCredential() bool {
	return v.data.Credential != nil
}

func (v *AddonsRotateCredentialArgs) Credential() string {
	if v.data.Credential == nil {
		return ""
	}
	return *v.data.Credential
}

func (v *AddonsRotateCredentialArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsRotateCredentialArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsRotateCredentialArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsRotateCredentialArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsRotateCredentialResultsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type AddonsRotateCredentialResults struct {
	call rpc.Call
	data addonsRotateCredentialResultsData
}

func (v *AddonsRotateCredentialResults) SetId(id string) {
	v.data.Id = &id
}

func (v *AddonsRotateCredentialResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsRotateCredentialResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsRotateCredentialResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsRotateCredentialResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsDeleteInstanceArgsData struct {
	App  *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Name *string `cbor:"1,keyasint,omitempty" json:"name,omitempty"`
}

type AddonsDeleteInstanceArgs struct {
	call rpc.Call
	data addonsDeleteInstanceArgsData
}

func (v *AddonsDeleteInstanceArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *AddonsDeleteInstanceArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *AddonsDeleteInstanceArgs) HasName() bool {
	return v.data.Name != nil
}

func (v *AddonsDeleteInstanceArgs) Name() string {
	if v.data.Name == nil {
		return ""
	}
	return *v.data.Name
}

func (v *AddonsDeleteInstanceArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsDeleteInstanceArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsDeleteInstanceArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsDeleteInstanceArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type addonsDeleteInstanceResultsData struct{}

type AddonsDeleteInstanceResults struct {
	call rpc.Call
	data addonsDeleteInstanceResultsData
}

func (v *AddonsDeleteInstanceResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *AddonsDeleteInstanceResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *AddonsDeleteInstanceResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *AddonsDeleteInstanceResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type AddonsCreateInstance struct {
	rpc.Call
	args    AddonsCreateInstanceArgs
	results AddonsCreateInstanceResults
}

func (t *AddonsCreateInstance) Args() *AddonsCreateInstanceArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AddonsCreateInstance) Results() *AddonsCreateInstanceResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AddonsListInstances struct {
	rpc.Call
	args    AddonsListInstancesArgs
	results AddonsListInstancesResults
}

func (t *AddonsListInstances) Args() *AddonsListInstancesArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AddonsListInstances) Results() *AddonsListInstancesResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AddonsRotateCredential struct {
	rpc.Call
	args    AddonsRotateCredentialArgs
	results AddonsRotateCredentialResults
}

func (t *AddonsRotateCredential) Args() *AddonsRotateCredentialArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AddonsRotateCredential) Results() *AddonsRotateCredentialResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type AddonsDeleteInstance struct {
	rpc.Call
	args    AddonsDeleteInstanceArgs
	results AddonsDeleteInstanceResults
}

func (t *AddonsDeleteInstance) Args() *AddonsDeleteInstanceArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *AddonsDeleteInstance) Results() *AddonsDeleteInstanceResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Addons interface {
	CreateInstance(ctx context.Context, state *AddonsCreateInstance) error
	ListInstances(ctx context.Context, state *AddonsListInstances) error
	RotateCredential(ctx context.Context, state *AddonsRotateCredential) error
	DeleteInstance(ctx context.Context, state *AddonsDeleteInstance) error
}

type reexportAddons struct {
	client rpc.Client
}

func (reexportAddons) CreateInstance(ctx context.Context, state *AddonsCreateInstance) error {
	panic("not implemented")
}

func (reexportAddons) ListInstances(ctx context.Context, state *AddonsListInstances) error {
	panic("not implemented")
}

func (reexportAddons) RotateCredential(ctx context.Context, state *AddonsRotateCredential) error {
	panic("not implemented")
}

func (reexportAddons) DeleteInstance(ctx context.Context, state *AddonsDeleteInstance) error {
	panic("not implemented")
}

func (t reexportAddons) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptAddons(t Addons) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "createInstance",
			InterfaceName: "Addons",
			Index:         0,
			Public:        false,
			Params:        []string{"name", "addon", "variant", "app", "version"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "POST",
				Path:       "/api/v1/apps/{app}/addons",
				Body:       "*",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.CreateInstance(ctx, &AddonsCreateInstance{Call: call})
			},
		},
		{
			Name:          "listInstances",
			InterfaceName: "Addons",
			Index:         0,
			Public:        false,
			Params:        []string{"app"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "GET",
				Path:       "/api/v1/apps/{app}/addons",
				Body:       "",
				PathParams: []string{"app"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListInstances(ctx, &AddonsListInstances{Call: call})
			},
		},
		{
			Name:          "rotateCredential",
			InterfaceName: "Addons",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "name", "credential"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "POST",
				Path:       "/api/v1/apps/{app}/addons/{name}/rotate",
				Body:       "*",
				PathParams: []string{"app", "name"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.RotateCredential(ctx, &AddonsRotateCredential{Call: call})
			},
		},
		{
			Name:          "deleteInstance",
			InterfaceName: "Addons",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "name"},
			HTTP: &rpc.HTTPBinding{
				Verb:       "DELETE",
				Path:       "/api/v1/apps/{app}/addons/{name}",
				Body:       "",
				PathParams: []string{"app", "name"},
			},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.DeleteInstance(ctx, &AddonsDeleteInstance{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type AddonsClient struct {
	rpc.Client
}

func NewAddonsClient(client rpc.Client) *AddonsClient {
	return &AddonsClient{Client: client}
}

func (c AddonsClient) Export() Addons {
	return reexportAddons{client: c.Client}
}

type AddonsClientCreateInstanceResults struct {
	client rpc.Client
	data   addonsCreateInstanceResultsData
}

func (v *AddonsClientCreateInstanceResults) HasId() bool {
	return v.data.Id != nil
}

func (v *AddonsClientCreateInstanceResults) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v AddonsClient) CreateInstance(ctx context.Context, name string, addon string, variant string, app string, version string) (*AddonsClientCreateInstanceResults, error) {
	args := AddonsCreateInstanceArgs{}
	args.data.Name = &name
	args.data.Addon = &addon
	args.data.Variant = &variant
	args.data.App = &app
	args.data.Version = &version

	var ret addonsCreateInstanceResultsData

	err := v.Call(ctx, "createInstance", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AddonsClientCreateInstanceResults{client: v.Client, data: ret}, nil
}

type AddonsClientListInstancesResults struct {
	client rpc.Client
	data   addonsListInstancesResultsData
}

func (v *AddonsClientListInstancesResults) HasAddons() bool {
	return v.data.Addons != nil
}

func (v *AddonsClientListInstancesResults) Addons() []*AddonInstance {
	if v.data.Addons == nil {
		return nil
	}
	return *v.data.Addons
}

func (v AddonsClient) ListInstances(ctx context.Context, app string) (*AddonsClientListInstancesResults, error) {
	args := AddonsListInstancesArgs{}
	args.data.App = &app

	var ret addonsListInstancesResultsData

	err := v.Call(ctx, "listInstances", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AddonsClientListInstancesResults{client: v.Client, data: ret}, nil
}

type AddonsClientRotateCredentialResults struct {
	client rpc.Client
	data   addonsRotateCredentialResultsData
}

func (v *AddonsClientRotateCredentialResults) HasId() bool {
	return v.data.Id != nil
}

func (v *AddonsClientRotateCredentialResults) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v AddonsClient) RotateCredential(ctx context.Context, app string, name string, credential string) (*AddonsClientRotateCredentialResults, error) {
	args := AddonsRotateCredentialArgs{}
	args.data.App = &app
	args.data.Name = &name
	args.data.Credential = &credential

	var ret addonsRotateCredentialResultsData

	err := v.Call(ctx, "rotateCredential", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AddonsClientRotateCredentialResults{client: v.Client, data: ret}, nil
}

type AddonsClientDeleteInstanceResults struct {
	client rpc.Client
	data   addonsDeleteInstanceResultsData
}

func (v AddonsClient) DeleteInstance(ctx context.Context, app string, name string) (*AddonsClientDeleteInstanceResults, error) {
	args := AddonsDeleteInstanceArgs{}
	args.data.App = &app
	args.data.Name = &name

	var ret addonsDeleteInstanceResultsData

	err := v.Call(ctx, "deleteInstance", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &AddonsClientDeleteInstanceResults{client: v.Client, data: ret}, nil
}

type runsCreateRunArgsData struct {
	App     *string   `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Task    *string   `cbor:"1,keyasint,omitempty" json:"task,omitempty"`
	Command *[]string `cbor:"2,keyasint,omitempty" json:"command,omitempty"`
	Tty     *bool     `cbor:"3,keyasint,omitempty" json:"tty,omitempty"`
}

type RunsCreateRunArgs struct {
	call rpc.Call
	data runsCreateRunArgsData
}

func (v *RunsCreateRunArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *RunsCreateRunArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *RunsCreateRunArgs) HasTask() bool {
	return v.data.Task != nil
}

func (v *RunsCreateRunArgs) Task() string {
	if v.data.Task == nil {
		return ""
	}
	return *v.data.Task
}

func (v *RunsCreateRunArgs) HasCommand() bool {
	return v.data.Command != nil
}

func (v *RunsCreateRunArgs) Command() []string {
	if v.data.Command == nil {
		return nil
	}
	return *v.data.Command
}

func (v *RunsCreateRunArgs) HasTty() bool {
	return v.data.Tty != nil
}

func (v *RunsCreateRunArgs) Tty() bool {
	if v.data.Tty == nil {
		return false
	}
	return *v.data.Tty
}

func (v *RunsCreateRunArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunsCreateRunArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunsCreateRunArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunsCreateRunArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runsCreateRunResultsData struct {
	Id          *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
	SandboxName *string `cbor:"1,keyasint,omitempty" json:"sandbox_name,omitempty"`
}

type RunsCreateRunResults struct {
	call rpc.Call
	data runsCreateRunResultsData
}

func (v *RunsCreateRunResults) SetId(id string) {
	v.data.Id = &id
}

func (v *RunsCreateRunResults) SetSandboxName(sandbox_name string) {
	v.data.SandboxName = &sandbox_name
}

func (v *RunsCreateRunResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunsCreateRunResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunsCreateRunResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunsCreateRunResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runsListRunsArgsData struct {
	App   *string `cbor:"0,keyasint,omitempty" json:"app,omitempty"`
	Task  *string `cbor:"1,keyasint,omitempty" json:"task,omitempty"`
	Limit *int32  `cbor:"2,keyasint,omitempty" json:"limit,omitempty"`
}

type RunsListRunsArgs struct {
	call rpc.Call
	data runsListRunsArgsData
}

func (v *RunsListRunsArgs) HasApp() bool {
	return v.data.App != nil
}

func (v *RunsListRunsArgs) App() string {
	if v.data.App == nil {
		return ""
	}
	return *v.data.App
}

func (v *RunsListRunsArgs) HasTask() bool {
	return v.data.Task != nil
}

func (v *RunsListRunsArgs) Task() string {
	if v.data.Task == nil {
		return ""
	}
	return *v.data.Task
}

func (v *RunsListRunsArgs) HasLimit() bool {
	return v.data.Limit != nil
}

func (v *RunsListRunsArgs) Limit() int32 {
	if v.data.Limit == nil {
		return 0
	}
	return *v.data.Limit
}

func (v *RunsListRunsArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunsListRunsArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunsListRunsArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunsListRunsArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runsListRunsResultsData struct {
	Runs *[]*RunInfo `cbor:"0,keyasint,omitempty" json:"runs,omitempty"`
}

type RunsListRunsResults struct {
	call rpc.Call
	data runsListRunsResultsData
}

func (v *RunsListRunsResults) SetRuns(runs []*RunInfo) {
	x := slices.Clone(runs)
	v.data.Runs = &x
}

func (v *RunsListRunsResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunsListRunsResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunsListRunsResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunsListRunsResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runsGetRunArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type RunsGetRunArgs struct {
	call rpc.Call
	data runsGetRunArgsData
}

func (v *RunsGetRunArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *RunsGetRunArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *RunsGetRunArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunsGetRunArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunsGetRunArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunsGetRunArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runsGetRunResultsData struct {
	Run *RunInfo `cbor:"0,keyasint,omitempty" json:"run,omitempty"`
}

type RunsGetRunResults struct {
	call rpc.Call
	data runsGetRunResultsData
}

func (v *RunsGetRunResults) SetRun(run *RunInfo) {
	v.data.Run = run
}

func (v *RunsGetRunResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunsGetRunResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunsGetRunResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunsGetRunResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runsCancelRunArgsData struct {
	Id *string `cbor:"0,keyasint,omitempty" json:"id,omitempty"`
}

type RunsCancelRunArgs struct {
	call rpc.Call
	data runsCancelRunArgsData
}

func (v *RunsCancelRunArgs) HasId() bool {
	return v.data.Id != nil
}

func (v *RunsCancelRunArgs) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *RunsCancelRunArgs) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunsCancelRunArgs) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunsCancelRunArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunsCancelRunArgs) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type runsCancelRunResultsData struct {
	Canceled *bool `cbor:"0,keyasint,omitempty" json:"canceled,omitempty"`
}

type RunsCancelRunResults struct {
	call rpc.Call
	data runsCancelRunResultsData
}

func (v *RunsCancelRunResults) SetCanceled(canceled bool) {
	v.data.Canceled = &canceled
}

func (v *RunsCancelRunResults) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(v.data)
}

func (v *RunsCancelRunResults) UnmarshalCBOR(data []byte) error {
	return cbor.Unmarshal(data, &v.data)
}

func (v *RunsCancelRunResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.data)
}

func (v *RunsCancelRunResults) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.data)
}

type RunsCreateRun struct {
	rpc.Call
	args    RunsCreateRunArgs
	results RunsCreateRunResults
}

func (t *RunsCreateRun) Args() *RunsCreateRunArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunsCreateRun) Results() *RunsCreateRunResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunsListRuns struct {
	rpc.Call
	args    RunsListRunsArgs
	results RunsListRunsResults
}

func (t *RunsListRuns) Args() *RunsListRunsArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunsListRuns) Results() *RunsListRunsResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunsGetRun struct {
	rpc.Call
	args    RunsGetRunArgs
	results RunsGetRunResults
}

func (t *RunsGetRun) Args() *RunsGetRunArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunsGetRun) Results() *RunsGetRunResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type RunsCancelRun struct {
	rpc.Call
	args    RunsCancelRunArgs
	results RunsCancelRunResults
}

func (t *RunsCancelRun) Args() *RunsCancelRunArgs {
	args := &t.args
	if args.call != nil {
		return args
	}
	args.call = t.Call
	t.Call.Args(args)
	return args
}

func (t *RunsCancelRun) Results() *RunsCancelRunResults {
	results := &t.results
	if results.call != nil {
		return results
	}
	results.call = t.Call
	t.Call.Results(results)
	return results
}

type Runs interface {
	CreateRun(ctx context.Context, state *RunsCreateRun) error
	ListRuns(ctx context.Context, state *RunsListRuns) error
	GetRun(ctx context.Context, state *RunsGetRun) error
	CancelRun(ctx context.Context, state *RunsCancelRun) error
}

type reexportRuns struct {
	client rpc.Client
}

func (reexportRuns) CreateRun(ctx context.Context, state *RunsCreateRun) error {
	panic("not implemented")
}

func (reexportRuns) ListRuns(ctx context.Context, state *RunsListRuns) error {
	panic("not implemented")
}

func (reexportRuns) GetRun(ctx context.Context, state *RunsGetRun) error {
	panic("not implemented")
}

func (reexportRuns) CancelRun(ctx context.Context, state *RunsCancelRun) error {
	panic("not implemented")
}

func (t reexportRuns) CapabilityClient() rpc.Client {
	return t.client
}

func AdaptRuns(t Runs) *rpc.Interface {
	methods := []rpc.Method{
		{
			Name:          "createRun",
			InterfaceName: "Runs",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "task", "command", "tty"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.CreateRun(ctx, &RunsCreateRun{Call: call})
			},
		},
		{
			Name:          "listRuns",
			InterfaceName: "Runs",
			Index:         0,
			Public:        false,
			Params:        []string{"app", "task", "limit"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.ListRuns(ctx, &RunsListRuns{Call: call})
			},
		},
		{
			Name:          "getRun",
			InterfaceName: "Runs",
			Index:         0,
			Public:        false,
			Params:        []string{"id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.GetRun(ctx, &RunsGetRun{Call: call})
			},
		},
		{
			Name:          "cancelRun",
			InterfaceName: "Runs",
			Index:         0,
			Public:        false,
			Params:        []string{"id"},
			Handler: func(ctx context.Context, call rpc.Call) error {
				return t.CancelRun(ctx, &RunsCancelRun{Call: call})
			},
		},
	}

	return rpc.NewInterface(methods, t)
}

type RunsClient struct {
	rpc.Client
}

func NewRunsClient(client rpc.Client) *RunsClient {
	return &RunsClient{Client: client}
}

func (c RunsClient) Export() Runs {
	return reexportRuns{client: c.Client}
}

type RunsClientCreateRunResults struct {
	client rpc.Client
	data   runsCreateRunResultsData
}

func (v *RunsClientCreateRunResults) HasId() bool {
	return v.data.Id != nil
}

func (v *RunsClientCreateRunResults) Id() string {
	if v.data.Id == nil {
		return ""
	}
	return *v.data.Id
}

func (v *RunsClientCreateRunResults) HasSandboxName() bool {
	return v.data.SandboxName != nil
}

func (v *RunsClientCreateRunResults) SandboxName() string {
	if v.data.SandboxName == nil {
		return ""
	}
	return *v.data.SandboxName
}

func (v RunsClient) CreateRun(ctx context.Context, app string, task string, command []string, tty bool) (*RunsClientCreateRunResults, error) {
	args := RunsCreateRunArgs{}
	args.data.App = &app
	args.data.Task = &task
	x := slices.Clone(command)
	args.data.Command = &x
	args.data.Tty = &tty

	var ret runsCreateRunResultsData

	err := v.Call(ctx, "createRun", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunsClientCreateRunResults{client: v.Client, data: ret}, nil
}

type RunsClientListRunsResults struct {
	client rpc.Client
	data   runsListRunsResultsData
}

func (v *RunsClientListRunsResults) HasRuns() bool {
	return v.data.Runs != nil
}

func (v *RunsClientListRunsResults) Runs() []*RunInfo {
	if v.data.Runs == nil {
		return nil
	}
	return *v.data.Runs
}

func (v RunsClient) ListRuns(ctx context.Context, app string, task string, limit int32) (*RunsClientListRunsResults, error) {
	args := RunsListRunsArgs{}
	args.data.App = &app
	args.data.Task = &task
	args.data.Limit = &limit

	var ret runsListRunsResultsData

	err := v.Call(ctx, "listRuns", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunsClientListRunsResults{client: v.Client, data: ret}, nil
}

type RunsClientGetRunResults struct {
	client rpc.Client
	data   runsGetRunResultsData
}

func (v *RunsClientGetRunResults) HasRun() bool {
	return v.data.Run != nil
}

func (v *RunsClientGetRunResults) Run() *RunInfo {
	return v.data.Run
}

func (v RunsClient) GetRun(ctx context.Context, id string) (*RunsClientGetRunResults, error) {
	args := RunsGetRunArgs{}
	args.data.Id = &id

	var ret runsGetRunResultsData

	err := v.Call(ctx, "getRun", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunsClientGetRunResults{client: v.Client, data: ret}, nil
}

type RunsClientCancelRunResults struct {
	client rpc.Client
	data   runsCancelRunResultsData
}

func (v *RunsClientCancelRunResults) HasCanceled() bool {
	return v.data.Canceled != nil
}

func (v *RunsClientCancelRunResults) Canceled() bool {
	if v.data.Canceled == nil {
		return false
	}
	return *v.data.Canceled
}

func (v RunsClient) CancelRun(ctx context.Context, id string) (*RunsClientCancelRunResults, error) {
	args := RunsCancelRunArgs{}
	args.data.Id = &id

	var ret runsCancelRunResultsData

	err := v.Call(ctx, "cancelRun", &args, &ret)
	if err != nil {
		return nil, err
	}

	return &RunsClientCancelRunResults{client: v.Client, data: ret}, nil
}
