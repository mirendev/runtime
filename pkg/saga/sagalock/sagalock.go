// Package sagalock keeps saga definitions from changing shape without a
// version decision.
//
// Each package that defines production sagas commits a lock file recording
// every definition's version and the shape recovery depends on: its actions
// and the keys that connect them. A test in that package registers its
// definitions and calls Check, which fails when a definition's shape differs
// from the lock at the same version. The fix is never to just re-record: it is
// to bump the version and declare, with ResumesFrom, whether executions
// recorded at the old version can be resumed under the new shape. Then re-run
// with SAGA_LOCK_UPDATE=1 to record the new version.
//
// The lock is readable on purpose. A shape change lands in review as a diff
// of action names and keys, next to the ResumesFrom decision it forced.
package sagalock

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"miren.dev/runtime/pkg/saga"
)

// LockFile is where Check looks for a package's lock, relative to the package
// directory a test runs in.
const LockFile = "testdata/saga-definitions.lock.json"

// UpdateEnv names the environment variable that lets Check rewrite the lock.
// It never lets a same-version shape change through.
const UpdateEnv = "SAGA_LOCK_UPDATE"

// Entry is one definition as the lock records it.
type Entry struct {
	Version     int               `json:"version"`
	ResumesFrom []int             `json:"resumes_from,omitempty"`
	Actions     map[string]Action `json:"actions"`
}

// Action is the part of an action recovery depends on: what it is called and
// which keys tie it to the others.
type Action struct {
	Inputs       []string `json:"inputs,omitempty"`
	Outputs      []string `json:"outputs,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// EntryFor reduces a definition to its lock entry.
func EntryFor(def *saga.Definition) Entry {
	entry := Entry{
		Version:     def.Version,
		ResumesFrom: def.ResumesFrom,
		Actions:     make(map[string]Action, len(def.Actions)),
	}
	for name, node := range def.Actions {
		entry.Actions[name] = Action{
			Inputs:       sorted(node.InputKeys),
			Outputs:      sorted(node.OutputKeys),
			Dependencies: sorted(node.Dependencies),
		}
	}
	return entry
}

func sorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

// Load reads a lock file. A missing file is an empty lock.
func Load(path string) (map[string]Entry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var lock map[string]Entry
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return lock, nil
}

// Compare checks registered definitions against a lock. Breaking problems
// are ones no re-record can fix; stale ones only mean the lock needs
// rewriting to match a decision already made in code.
func Compare(lock map[string]Entry, defs []*saga.Definition) (breaking, stale []string) {
	seen := make(map[string]bool, len(defs))
	for _, def := range defs {
		seen[def.Name] = true
		current := EntryFor(def)

		old, ok := lock[def.Name]
		switch {
		case !ok:
			stale = append(stale, fmt.Sprintf("saga %q is not in the lock yet", def.Name))
		case def.Version < old.Version:
			breaking = append(breaking, fmt.Sprintf(
				"saga %q went from v%d back to v%d; executions recorded at v%d would meet a binary that has never heard of their version",
				def.Name, old.Version, def.Version, old.Version))
		case def.Version == old.Version && !reflect.DeepEqual(old.Actions, current.Actions):
			breaking = append(breaking, fmt.Sprintf(
				"saga %q changed shape without a version bump (still v%d). An execution started under the old shape "+
					"would be resumed against the new one. Bump Version and declare with ResumesFrom whether v%d "+
					"executions are safe to resume (see pkg/saga/doc.go), then re-run with %s=1",
				def.Name, def.Version, def.Version, UpdateEnv))
		case !reflect.DeepEqual(old, current):
			stale = append(stale, fmt.Sprintf("saga %q moved to v%d; the lock still records v%d", def.Name, def.Version, old.Version))
		}
	}

	for _, name := range slices.Sorted(maps.Keys(lock)) {
		if !seen[name] {
			stale = append(stale, fmt.Sprintf("saga %q is in the lock but no longer registered", name))
		}
	}
	return breaking, stale
}

// Check compares every definition in registry with the package's lock file.
// With SAGA_LOCK_UPDATE set and nothing breaking, it rewrites the lock
// instead of failing on stale entries.
func Check(t testing.TB, registry *saga.Registry) {
	t.Helper()

	defs := registry.Definitions()
	if len(defs) == 0 {
		t.Fatal("no saga definitions registered; the lock test has nothing to check")
	}

	lock, err := Load(LockFile)
	if err != nil {
		t.Fatal(err)
	}

	breaking, stale := Compare(lock, defs)
	for _, msg := range breaking {
		t.Error(msg)
	}
	if len(breaking) > 0 || len(stale) == 0 {
		return
	}

	if os.Getenv(UpdateEnv) == "" {
		for _, msg := range stale {
			t.Errorf("%s; re-run with %s=1 to record it", msg, UpdateEnv)
		}
		return
	}

	updated := make(map[string]Entry, len(defs))
	for _, def := range defs {
		updated[def.Name] = EntryFor(def)
	}
	data, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(LockFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LockFile, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("recorded %d saga definitions in %s", len(defs), LockFile)
}
