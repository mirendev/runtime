package runsc

import "fmt"

type InitConfig struct {
	TraceSession SessionConfig `json:"trace_session"`
}

// SessionConfig describes a new session configuration. A session consists of a
// set of points to be enabled and sinks where the points are sent to.
type SessionConfig struct {
	// Name is the unique session name.
	Name string `json:"name,omitempty"`
	// Points is the set of points to enable in this session.
	Points []PointConfig `json:"points,omitempty"`
	// IgnoreMissing skips point and optional/context fields not found. This can
	// be used to apply a single configuration file with newer points/fields with
	// older versions which do not have them yet. Note that it may hide typos in
	// the configuration.
	//
	// This field does NOT apply to sinks.
	IgnoreMissing bool `json:"ignore_missing,omitempty"`
	// Sinks are the sinks that will process the points enabled above.
	Sinks []SinkConfig `json:"sinks,omitempty"`
}

// PointConfig describes a point to be enabled in a given session.
type PointConfig struct {
	// Name is the point to be enabled. The point must exist in the system.
	Name string `json:"name,omitempty"`
	// OptionalFields is the list of optional fields to collect from the point.
	OptionalFields []string `json:"optional_fields,omitempty"`
	// ContextFields is the list of context fields to collect.
	ContextFields []string `json:"context_fields,omitempty"`
}

// SinkConfig describes the sink that will process the points in a given
// session.
type SinkConfig struct {
	// Name is the sink to be created. The sink must exist in the system.
	Name string `json:"name,omitempty"`
	// Config is a opaque json object that is passed to the sink.
	Config map[string]any `json:"config,omitempty"`
	// IgnoreSetupError makes errors during sink setup to be ignored. Otherwise,
	// failures will prevent the container from starting.
	IgnoreSetupError bool `json:"ignore_setup_error,omitempty"`
	// Status is the runtime status for the sink.
	Status string `json:"status,omitempty"`
}

func EnterSyscallByName(name string) string {
	return "syscall/" + name + "/enter"
}

func ExitSyscallByName(name string) string {
	return "syscall/" + name + "/exit"
}

func SyscallByNumber(num int) (string, string) {
	root := fmt.Sprintf("syscall/%d", num)

	return root + "/enter", root + "/exit"
}

const (
	ContainerStart = "container/start"
	SentryClone    = "sentry/clone"
	SentryExecve   = "sentry/execve"
	SentryTaskExit = "sentry/task_exit"
)

func (s *SessionConfig) AddPoints(name ...string) {
	for _, n := range name {
		s.Points = append(s.Points, PointConfig{
			Name:          n,
			ContextFields: []string{"time", "thread_id", "container_id", "process_name"},
		})
	}
}

func (s *SessionConfig) ConnectTo(path string) {
	s.Sinks = append(s.Sinks, SinkConfig{
		Name:   "remote",
		Config: map[string]any{"entrypoint": path},
	})
}
