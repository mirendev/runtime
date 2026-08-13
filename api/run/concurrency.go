package run

import (
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/appconfig"
)

// MaxConcurrent resolves a task's ceiling on simultaneous runs.
//
// Shared rather than duplicated because two sides have to agree on the number:
// the run controller enforces it at admission, and the app server quotes it
// when refusing a manual invoke. If those disagree the refusal is a lie -- it
// names a limit the controller does not apply -- so they read it from here.
//
// task is nil when the app never declared it, which is the ordinary case for
// the console convention and the reason the two defaults differ.
func MaxConcurrent(task *core_v1alpha.ConfigSpecTasks, taskName string) int64 {
	if task != nil && task.MaxConcurrent > 0 {
		return task.MaxConcurrent
	}
	if taskName == appconfig.ConsoleName {
		return appconfig.ConsoleMaxConcurrent
	}
	return appconfig.DefaultTaskMaxConcurrent
}
