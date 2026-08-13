package run

import (
	"fmt"
	"strings"

	"miren.dev/runtime/pkg/entity"
)

// SandboxName derives the sandbox a run's attempt executes in.
//
// The derivation is shared rather than duplicated because two sides depend on
// agreeing exactly: the run controller creates the sandbox under this name, and
// the app server hands the same string to a client so it can attach without
// waiting to observe the sandbox appear. Two copies of one naming rule drift
// silently -- change the prefix on one side and the other keeps returning the
// old name, leaving the client retrying against a sandbox that will never exist
// and reporting a generic attach failure.
//
// Being derived rather than allocated is also what makes creation idempotent:
// the same run and attempt always name the same sandbox, so a lost create reply
// cannot produce two sandboxes running the command.
func SandboxName(runID entity.Id, attempt int64) entity.Id {
	if attempt < 1 {
		attempt = 1
	}
	return entity.Id(fmt.Sprintf("sandbox/run-%s-a%d", strings.TrimPrefix(runID.String(), "run/"), attempt))
}
