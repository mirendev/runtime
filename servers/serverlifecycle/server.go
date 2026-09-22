// Package serverlifecycle serves the ServerLifecycle RPC in front of the
// on-disk operation ledger in pkg/serverlifecycle.
package serverlifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	lifecycle "miren.dev/runtime/pkg/serverlifecycle"
)

// Server answers ServerLifecycle calls. Reads go straight to the file store:
// the executor writes there from its own process, so the files are the truth
// even while this server is the one being replaced.
type Server struct {
	store    *lifecycle.Store
	launcher lifecycle.Launcher
	log      *slog.Logger
	// command is the CLI subcommand that reads this ledger, for the hint in a
	// busy error.
	command string
}

var _ server_v1alpha.ServerLifecycle = (*Server)(nil)

func NewServer(store *lifecycle.Store, launcher lifecycle.Launcher, log *slog.Logger) *Server {
	return &Server{store: store, launcher: launcher, log: log, command: "server operations"}
}

// ForRunner points the CLI hints at the runner's ledger.
func (s *Server) ForRunner() *Server {
	s.command = "runner operations"
	return s
}

func (s *Server) List(_ context.Context, state *server_v1alpha.ServerLifecycleList) error {
	ops, err := s.store.List()
	if err != nil {
		return err
	}
	out := make([]*server_v1alpha.Operation, 0, len(ops))
	for _, op := range ops {
		out = append(out, ToRPC(op))
	}
	state.Results().SetOperations(out)
	return nil
}

func (s *Server) Get(_ context.Context, state *server_v1alpha.ServerLifecycleGet) error {
	op, err := s.store.Get(state.Args().Id())
	if err != nil {
		// Typed so the client can tell "no such operation" from "could not
		// reach the server": a follower gives up on the first and waits out
		// the second.
		if errors.Is(err, lifecycle.ErrNotFound) {
			return cond.NotFound("lifecycle operation", state.Args().Id())
		}
		return err
	}
	state.Results().SetOperation(ToRPC(op))
	return nil
}

func (s *Server) Start(ctx context.Context, state *server_v1alpha.ServerLifecycleStart) error {
	args := state.Args()
	requestedBy := "rpc"
	if identity := rpc.IdentityFromContext(ctx); identity != nil && identity.Subject != "" {
		requestedBy = identity.Subject
	}
	op := lifecycle.NewOperation(lifecycle.Action(args.Action()), requestedBy)
	if args.Id() != "" {
		op.ID = args.Id()
	}
	op.TargetVersion = args.TargetVersion()
	op.ArtifactType = args.ArtifactType()
	op.NoRollback = args.NoRollback()
	op.ReadyTimeoutSeconds = int(args.ReadyTimeoutSeconds())

	started, created, err := lifecycle.Start(ctx, s.store, s.launcher, op)
	if err != nil {
		if errors.Is(err, lifecycle.ErrBusy) {
			return fmt.Errorf("%w; see 'miren %s list'", err, s.command)
		}
		return err
	}
	if created {
		s.log.Info("lifecycle operation started", "operation", started.ID, "action", started.Action, "target", started.TargetVersion, "requested_by", requestedBy)
	}
	state.Results().SetOperation(ToRPC(started))
	return nil
}

// ToRPC converts a stored record to its wire shape.
func ToRPC(op *lifecycle.Operation) *server_v1alpha.Operation {
	var v server_v1alpha.Operation
	v.SetId(op.ID)
	v.SetAction(string(op.Action))
	v.SetPhase(string(op.Phase))
	v.SetRequestedBy(op.RequestedBy)
	v.SetTargetVersion(op.TargetVersion)
	v.SetResolvedVersion(op.ResolvedVersion)
	v.SetResolvedCommit(op.ResolvedCommit)
	v.SetArtifactType(op.ArtifactType)
	v.SetNoRollback(op.NoRollback)
	v.SetReadyTimeoutSeconds(int32(op.ReadyTimeoutSeconds))
	v.SetError(op.Error)
	v.SetProgress(op.Progress)
	v.SetPreviousInstanceId(op.PreviousInstanceID)
	v.SetPreviousVersion(op.PreviousVersion)
	v.SetPreviousCommit(op.PreviousCommit)
	v.SetNewInstanceId(op.NewInstanceID)
	v.SetNewVersion(op.NewVersion)
	v.SetCreatedAt(standard.ToTimestamp(op.CreatedAt))
	v.SetUpdatedAt(standard.ToTimestamp(op.UpdatedAt))
	if op.FinishedAt != nil {
		v.SetFinishedAt(standard.ToTimestamp(*op.FinishedAt))
	}
	if raw, err := json.Marshal(op); err == nil {
		v.SetRecord(string(raw))
	}
	return &v
}

// FromRPC converts a wire record back to the stored shape, for clients that
// want to reuse the store's own helpers (Done, Succeeded) on what they fetched.
// The verbatim record is preferred when the server sent one, so fields this
// schema does not name still arrive; the typed fields are the fallback.
func FromRPC(v *server_v1alpha.Operation) *lifecycle.Operation {
	if v == nil {
		return nil
	}
	if v.HasRecord() && v.Record() != "" {
		var op lifecycle.Operation
		if err := json.Unmarshal([]byte(v.Record()), &op); err == nil && op.ID != "" {
			return &op
		}
	}
	op := &lifecycle.Operation{
		ID:                  v.Id(),
		Action:              lifecycle.Action(v.Action()),
		Phase:               lifecycle.Phase(v.Phase()),
		RequestedBy:         v.RequestedBy(),
		TargetVersion:       v.TargetVersion(),
		ResolvedVersion:     v.ResolvedVersion(),
		ResolvedCommit:      v.ResolvedCommit(),
		ArtifactType:        v.ArtifactType(),
		NoRollback:          v.NoRollback(),
		ReadyTimeoutSeconds: int(v.ReadyTimeoutSeconds()),
		Error:               v.Error(),
		Progress:            v.Progress(),
		PreviousInstanceID:  v.PreviousInstanceId(),
		PreviousVersion:     v.PreviousVersion(),
		PreviousCommit:      v.PreviousCommit(),
		NewInstanceID:       v.NewInstanceId(),
		NewVersion:          v.NewVersion(),
	}
	if v.HasCreatedAt() {
		op.CreatedAt = standard.FromTimestamp(v.CreatedAt())
	}
	if v.HasUpdatedAt() {
		op.UpdatedAt = standard.FromTimestamp(v.UpdatedAt())
	}
	if v.HasFinishedAt() {
		t := standard.FromTimestamp(v.FinishedAt())
		op.FinishedAt = &t
	}
	return op
}
