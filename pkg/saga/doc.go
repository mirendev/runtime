// Package saga implements the Saga pattern for distributed operations with
// crash recovery. Each saga is a sequence of steps where each step has a
// corresponding undo operation. The framework guarantees that either all
// steps complete successfully or all completed steps are rolled back.
//
// See RFD-35 for detailed design documentation.
//
// # Changing a definition
//
// An execution records the name and version of the definition that started
// it. Recovery mostly runs during upgrades, so the binary resuming an
// execution is often newer than the one that started it, and its definition
// may have changed in between. The executor will not guess whether that is
// safe. It resumes an execution only when the current definition declares it
// can, and otherwise refuses before running or undoing anything, recording why
// on the execution for `miren debug saga show` to report.
//
// Changes that keep the definition's shape are compatible and need no version
// bump: editing what an action does while keeping its name, the keys it reads
// and writes, and what its undo compensates. An execution resumed across such a
// change runs the new code for the actions it has left, and undoes its earlier
// actions with the new undo code.
//
// Changes to the shape are breaking until someone decides otherwise. That
// covers adding, removing, or renaming an action, and changing the keys that
// connect actions, which is to say the dependency order. Each one needs a
// Version bump plus a ResumesFrom declaration, and Build rejects a bump that
// has no declaration. ResumesFrom(1) says executions recorded at v1 are safe
// to drive under the new graph. An empty ResumesFrom() says they are not, and
// those executions will be refused. Things to weigh before listing a version:
//
//   - An added action will run for in-flight executions that have not reached
//     it, including ones whose earlier actions ran under the old code. It must
//     cope with the inputs those earlier actions produced.
//   - A removed or renamed action cannot be undone once it is gone. An execution
//     that already ran it is refused regardless of ResumesFrom, because nothing
//     could compensate it. If that would strand executions that matter, rename
//     the code but keep the action name, or ship the removal once no execution
//     that ran it can still be in flight.
//   - A dependency change can reorder what is left for an execution partway
//     through. Check that every remaining action's inputs are still produced
//     by something that has run or will run first.
//
// Each package that defines production sagas commits a lock of their shapes,
// checked by pkg/saga/sagalock, so a shape change cannot land at an unchanged
// version without a test failing and pointing here.
//
// An execution with nothing left to compensate and nothing in flight is
// exempt: one that never started, or one already compensating whose actions
// all failed or were undone. No work remains that a change could strand, so
// it is re-stamped at the current version and run. A running execution with
// nothing recorded is not exempt, since it may have crashed partway through
// an action whose work, a nested saga's included, exists but was never
// recorded. Completed and failed executions run nothing and are never refused, and
// a nested child that is already terminal is reported as it stands rather
// than driven again, the same as a top-level one.
//
// When an execution is refused, the remedy is a release whose definition can
// resume it, which lets it finish or compensate normally. `miren debug saga
// abandon` gives a refused execution up without compensating, for when no
// such release exists. The stalled sweep leaves refused executions alone: they
// are waiting on an operator, not stranded, and forcing one to failed would
// let a reconcile retry over work nobody undid.
//
// One known gap: the shared-server addon sagas (provision-shared-postgresql
// and provision-shared-mysql) look up their server before resuming their
// ensure-shared-* child, and treat a server older than ten minutes as stale.
// A release that can resume a refused child of theirs arrives long after
// that, so resuming one deletes what the child built before driving it on.
// Until those actions resume their own child first (MIR-2000), abandon is the
// supported route for a refused execution of these two sagas. Abandoning the
// child lets the parent clean up and unwind, but it leaves the association in
// error, so the addon then has to be destroyed and added again.
package saga
