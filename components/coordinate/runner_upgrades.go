package coordinate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/lifecyclesync"
	"miren.dev/runtime/pkg/release"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/runnerlifecycle"
	"miren.dev/runtime/pkg/serverlifecycle"
	lifecyclesrv "miren.dev/runtime/servers/serverlifecycle"
)

// RunnerUpgradeOptions tunes the walk.
type RunnerUpgradeOptions struct {
	// StepTimeout bounds one runner's operation, download included. The
	// runner's own ready timeout is inside it.
	StepTimeout time.Duration
	// ReadyTimeout is how long a runner that finished its operation gets to
	// come back READY on the new build in the node inventory.
	ReadyTimeout time.Duration
	// NotReadyGrace is how long a runner that is not READY when its turn
	// comes gets to become so before it is skipped. Right after the server
	// restart that precedes the walk, every runner is re-establishing its
	// session, and that must not read as a fleet of dead runners.
	NotReadyGrace time.Duration
	PollInterval  time.Duration
	// AdoptRetry is how long to keep trying for the operation lock while the
	// executor that handed off is still letting go of it.
	AdoptRetry time.Duration
	// RetryFor bounds how long a walk keeps retrying after an error that is
	// not a step outcome (the inventory or the ledger not answering) before
	// the operation is failed for good. RetryBackoff is the first wait
	// between attempts; it doubles up to RetryBackoffMax.
	RetryFor        time.Duration
	RetryBackoff    time.Duration
	RetryBackoffMax time.Duration
}

func DefaultRunnerUpgradeOptions() RunnerUpgradeOptions {
	return RunnerUpgradeOptions{
		StepTimeout:     20 * time.Minute,
		ReadyTimeout:    3 * time.Minute,
		NotReadyGrace:   90 * time.Second,
		PollInterval:    2 * time.Second,
		AdoptRetry:      30 * time.Second,
		RetryFor:        10 * time.Minute,
		RetryBackoff:    2 * time.Second,
		RetryBackoffMax: 30 * time.Second,
	}
}

// RunnerLifecycleClient is one runner's ledger as the walker drives it.
type RunnerLifecycleClient interface {
	Start(ctx context.Context, op *serverlifecycle.Operation) (*serverlifecycle.Operation, error)
	Get(ctx context.Context, id string) (*serverlifecycle.Operation, error)
	Close()
}

// ErrRunnerUnmanaged: the runner answers RPC but does not serve a lifecycle
// ledger, so it predates managed upgrades.
var ErrRunnerUnmanaged = errors.New("runner does not serve a lifecycle ledger")

// RunnerDialer reaches a runner's ledger at its API address.
type RunnerDialer func(ctx context.Context, address string) (RunnerLifecycleClient, error)

// NodeInventory is the walker's view of the cluster's nodes.
type NodeInventory interface {
	// Runners lists every node that is not the coordinator, by name.
	Runners(ctx context.Context) ([]*compute_v1alpha.Node, error)
	// Runner finds one node by runner id; nil when it is gone.
	Runner(ctx context.Context, runnerID string) (*compute_v1alpha.Node, error)
	SetScheduling(ctx context.Context, node *compute_v1alpha.Node, scheduling entity.Id) error
}

// RunnerUpgrader finishes upgrade operations the executor handed off. The
// executor stops once the server it restarted reports ready; the server is
// then the one with the node inventory, the RPC to every runner, and the
// build that knows the runner protocol. It walks the runners one at a time:
// a bad build shows up on the first runner and stops there, and each step is
// gated on the runner coming back READY on the new build before the next
// begins. Steps are checkpointed on the record, so a server that restarts
// mid-walk resumes where it was.
type RunnerUpgrader struct {
	log        *slog.Logger
	store      *serverlifecycle.Store
	watcher    *lifecyclesync.Watcher
	instanceID string
	nodes      NodeInventory
	dial       RunnerDialer
	opts       RunnerUpgradeOptions

	mu      sync.Mutex
	driving map[string]bool
	wg      sync.WaitGroup
}

func NewRunnerUpgrader(log *slog.Logger, store *serverlifecycle.Store, watcher *lifecyclesync.Watcher, instanceID string, nodes NodeInventory, dial RunnerDialer, opts RunnerUpgradeOptions) *RunnerUpgrader {
	return &RunnerUpgrader{
		log: log, store: store, watcher: watcher, instanceID: instanceID,
		nodes: nodes, dial: dial, opts: opts,
		driving: map[string]bool{},
	}
}

// Run adopts every handed-off operation in the ledger, now and as they
// appear, until ctx ends. Subscribing before the initial scan is what keeps
// the two from missing one between them.
func (u *RunnerUpgrader) Run(ctx context.Context) error {
	sub, stop := u.watcher.Subscribe()
	defer stop()
	defer u.wg.Wait()

	if ops, err := u.store.List(); err != nil {
		u.log.Warn("could not scan lifecycle ledger for handed-off upgrades", "error", err)
	} else {
		for _, op := range ops {
			u.adopt(ctx, op)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sub.Wake():
			for _, op := range sub.Drain() {
				u.adopt(ctx, op)
			}
		}
	}
}

func (u *RunnerUpgrader) adopt(ctx context.Context, op *serverlifecycle.Operation) {
	if !op.HandedOff() {
		return
	}
	u.mu.Lock()
	if u.driving[op.ID] {
		u.mu.Unlock()
		return
	}
	u.driving[op.ID] = true
	u.mu.Unlock()

	u.wg.Go(func() {
		defer func() {
			u.mu.Lock()
			delete(u.driving, op.ID)
			u.mu.Unlock()
		}()
		if err := u.driveWithRetry(ctx, op.ID); err != nil && ctx.Err() == nil {
			u.log.Error("runner upgrade walk stopped", "operation", op.ID, "error", err)
		}
	})
}

// driveWithRetry keeps a walk going through errors that are nobody's step
// outcome: the inventory or the ledger not answering, or the lock still held
// by the executor. Steps are checkpointed, so each attempt resumes where the
// last one stopped. An error that outlasts RetryFor fails the operation on
// the record; a walk that just returned would leave the record handed off
// and holding the busy slot, with no ledger change to wake anyone to it.
func (u *RunnerUpgrader) driveWithRetry(ctx context.Context, id string) error {
	deadline := time.Now().Add(u.opts.RetryFor)
	backoff := u.opts.RetryBackoff
	for {
		err := u.drive(ctx, id)
		if err == nil || ctx.Err() != nil {
			return err
		}
		if time.Now().After(deadline) {
			return u.settleStranded(ctx, id, err)
		}
		u.log.Warn("runner upgrade walk hit an error; retrying", "operation", id, "in", backoff, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, u.opts.RetryBackoffMax)
	}
}

// settleStranded records that the walk gave up, so the operation settles
// rather than sitting handed off forever. The settlement itself is retried
// until it is durably on the record or the server is going down: the ledger
// or its lock not answering is the same kind of error that stranded the
// walk, and giving up here would leave the record handed off with nothing
// to wake anyone to it.
func (u *RunnerUpgrader) settleStranded(ctx context.Context, id string, cause error) error {
	backoff := u.opts.RetryBackoff
	for {
		err := u.failStranded(id, cause)
		if err == nil {
			return nil
		}
		u.log.Warn("could not record that the runner upgrade walk gave up; retrying", "operation", id, "in", backoff, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, u.opts.RetryBackoffMax)
	}
}

// failStranded is one attempt at the settlement, under the lock and only
// while the record is still handed off: another server may have finished
// it in the meantime.
func (u *RunnerUpgrader) failStranded(id string, cause error) error {
	unlock, err := u.store.LockOperation(id)
	if err != nil {
		return fmt.Errorf("%w (and could not lock the record to fail it: %v)", cause, err)
	}
	defer unlock()
	op, err := u.store.Get(id)
	if err != nil {
		return fmt.Errorf("%w (and could not read the record to fail it: %v)", cause, err)
	}
	if !op.HandedOff() {
		return nil
	}
	return u.finish(op, fmt.Errorf("gave up walking the runners after %s of errors: %w", u.opts.RetryFor, cause))
}

// lockOperation takes the operation lock, waiting out the executor that is
// still holding it after writing the hand-off.
func (u *RunnerUpgrader) lockOperation(ctx context.Context, id string) (func(), error) {
	deadline := time.Now().Add(u.opts.AdoptRetry)
	for {
		unlock, err := u.store.LockOperation(id)
		if err == nil {
			return unlock, nil
		}
		if !errors.Is(err, serverlifecycle.ErrLocked) || time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (u *RunnerUpgrader) drive(ctx context.Context, id string) error {
	unlock, err := u.lockOperation(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	op, err := u.store.Get(id)
	if err != nil {
		return err
	}
	if !op.HandedOff() {
		return nil
	}
	log := u.log.With("operation", op.ID)
	if op.DrivenBy != u.instanceID {
		if op.DrivenBy != "" {
			log.Info("resuming runner upgrades from a previous server instance", "previous", op.DrivenBy)
		}
		op.DrivenBy = u.instanceID
	}
	if op.Nodes == nil {
		runners, err := u.nodes.Runners(ctx)
		if err != nil {
			// Not a step outcome: the retry around the walk owns it, as
			// it does for every later inventory read.
			return fmt.Errorf("list runners: %w", err)
		}
		for _, node := range runners {
			op.Nodes = append(op.Nodes, &serverlifecycle.NodeStep{
				Name: nodeName(node), RunnerID: node.RunnerId, Phase: serverlifecycle.PhasePending,
				PreviousVersion: node.Version,
			})
		}
		log.Info("upgrading runners", "runners", len(op.Nodes), "target", op.NewVersion)
	}
	if err := u.store.Update(op); err != nil {
		return err
	}
	u.watcher.Kick()

	// The first runner is the canary: a failure before any runner is on the
	// new build says the build is suspect, and the rest are left alone. Once
	// one runner is there, a later failure is that runner's problem, and
	// stopping would leave every runner behind it on the old build for the
	// sake of one that needs a hand anyway. The walk goes on, and the
	// operation ends failed naming each runner that did not make it.
	succeeded := 0
	var failed []*serverlifecycle.NodeStep
	for i, step := range op.Nodes {
		if !step.Done() {
			if len(failed) > 0 && succeeded == 0 {
				u.setStep(op, step, serverlifecycle.StepSkipped, "not attempted: "+failed[0].Name+" failed before any runner was upgraded")
				continue
			}
			op.Progress = fmt.Sprintf("runner %d/%d: %s", i+1, len(op.Nodes), step.Name)
			if err := u.driveStep(ctx, op, step); err != nil {
				return err
			}
		}
		switch {
		case step.Succeeded():
			succeeded++
		case step.Phase != serverlifecycle.StepSkipped:
			failed = append(failed, step)
		}
	}
	if len(failed) > 0 {
		return u.finish(op, failedRunners(failed))
	}
	return u.finish(op, nil)
}

func failedRunners(failed []*serverlifecycle.NodeStep) error {
	if len(failed) == 1 {
		return fmt.Errorf("runner %s: %s", failed[0].Name, failed[0].Error)
	}
	parts := make([]string, 0, len(failed))
	for _, step := range failed {
		parts = append(parts, step.Name+": "+step.Error)
	}
	return fmt.Errorf("%d runners failed: %s", len(failed), strings.Join(parts, "; "))
}

func (u *RunnerUpgrader) finish(op *serverlifecycle.Operation, cause error) error {
	op.Progress = ""
	if cause != nil {
		op.Error = cause.Error()
		op.Phase = serverlifecycle.PhaseFailed
		u.log.Error("upgrade failed while upgrading runners", "operation", op.ID, "error", cause)
	} else {
		op.Phase = serverlifecycle.PhaseSucceeded
		u.log.Info("upgrade finished across the cluster", "operation", op.ID, "version", op.NewVersion, "runners", len(op.Nodes))
	}
	err := u.store.Update(op)
	u.watcher.Kick()
	return err
}

// setStep records a step outcome or progress and persists the operation.
func (u *RunnerUpgrader) setStep(op *serverlifecycle.Operation, step *serverlifecycle.NodeStep, phase serverlifecycle.Phase, detail string) {
	now := time.Now().UTC()
	if step.StartedAt == nil {
		step.StartedAt = &now
	}
	step.Phase = phase
	if step.Done() && !step.Succeeded() {
		step.Error = detail
		step.Progress = ""
	} else {
		step.Progress = detail
	}
	if step.Done() && step.FinishedAt == nil {
		step.FinishedAt = &now
	}
	if err := u.store.Update(op); err != nil {
		u.log.Warn("could not record runner upgrade step", "operation", op.ID, "runner", step.Name, "error", err)
	}
	u.watcher.Kick()
}

func (u *RunnerUpgrader) driveStep(ctx context.Context, op *serverlifecycle.Operation, step *serverlifecycle.NodeStep) error {
	log := u.log.With("operation", op.ID, "runner", step.Name)
	// A step with an operation id was already asked once, by a server that
	// died before seeing it through. It gets no eligibility shortcuts: the
	// runner may be mid-restart on our account, and only its record says
	// how that went.
	resumed := step.OperationID != ""

	node, err := u.readyRunner(ctx, op, step)
	if err != nil {
		return err
	}
	if node == nil {
		u.setStep(op, step, serverlifecycle.StepSkipped, "runner is no longer registered")
		return nil
	}
	// Whatever happens from here, a cordon of ours comes off. Registered
	// before any early return so a resumed step settles the cordon its
	// predecessor left, and a no-op for a step that never cordoned.
	defer func() {
		if !step.Cordoned {
			return
		}
		if err := u.nodes.SetScheduling(context.WithoutCancel(ctx), node, compute_v1alpha.NodeSchedulingSchedulableId); err != nil {
			log.Warn("could not uncordon runner after upgrade", "error", err)
			return
		}
		step.Cordoned = false
		if err := u.store.Update(op); err != nil {
			log.Warn("could not record uncordon", "error", err)
		}
	}()

	if !resumed {
		switch {
		case node.Status != compute_v1alpha.READY:
			u.setStep(op, step, serverlifecycle.StepSkipped, "runner is not ready ("+nodeStatus(node)+"); upgrade it once it is back")
			return nil
		case op.NewVersion != "" && node.Version == op.NewVersion:
			step.NewVersion = node.Version
			u.setStep(op, step, serverlifecycle.PhaseSucceeded, "already running "+node.Version)
			return nil
		}
		// Any other version is walked to the coordinator's, a runner that got
		// ahead of it included: the fleet converges on the coordinator's
		// build, and a runner is not the place to decide otherwise.
	}

	client, err := u.dial(ctx, node.ApiAddress)
	if err != nil {
		switch {
		case errors.Is(err, ErrRunnerUnmanaged):
			u.setStep(op, step, serverlifecycle.StepSkipped, "runner predates managed upgrades; run 'sudo miren upgrade' on its host once")
			return nil
		case !resumed:
			u.setStep(op, step, serverlifecycle.PhaseFailed, "cannot reach runner at "+node.ApiAddress+": "+err.Error())
			return nil
		}
		// A resumed runner may be restarting under the operation we gave
		// it; the follow keeps dialing.
		client = nil
	}

	if step.OperationID == "" {
		step.OperationID = serverlifecycle.NewID()
	}
	// Cordon so the scheduler does not place new work on a node about to
	// restart. Only our own cordon is undone afterwards; an operator's stays.
	if node.Scheduling != compute_v1alpha.CORDONED && !step.Cordoned {
		if err := u.nodes.SetScheduling(ctx, node, compute_v1alpha.NodeSchedulingCordonedId); err != nil {
			log.Warn("could not cordon runner before upgrade; continuing", "error", err)
		} else {
			step.Cordoned = true
		}
	}
	if !resumed {
		u.setStep(op, step, serverlifecycle.PhasePending, "asking runner to upgrade")
	}

	req := &serverlifecycle.Operation{
		ID: step.OperationID, Action: serverlifecycle.ActionUpgrade, RequestedBy: "coordinator:" + op.ID,
		TargetVersion: runnerTarget(op), ArtifactType: op.ArtifactType,
		NoRollback: op.NoRollback, ReadyTimeoutSeconds: op.ReadyTimeoutSeconds,
	}
	remote, err := u.followRunner(ctx, op, step, client, node.ApiAddress, req)
	if err != nil {
		return err
	}
	if remote == nil {
		return nil
	}
	u.mirror(step, remote)
	if !remote.Succeeded() {
		u.setStep(op, step, remote.Phase, remote.Error)
		return nil
	}
	u.setStep(op, step, serverlifecycle.PhaseVerifying, "waiting for the runner to rejoin on "+op.NewVersion)
	return u.awaitReady(ctx, op, step, op.NewVersion)
}

// runnerTarget is what the runners are asked to install. The coordinator
// resolved the operation's target once; the runners get that build, not the
// target string, so a release cut mid-walk cannot split the fleet. A main
// build is downloaded by channel rather than by its resolved name, so the
// channel goes through and the inventory check catches a split afterwards.
func runnerTarget(op *serverlifecycle.Operation) string {
	if op.ResolvedVersion != "" {
		if _, err := release.ParseSemVer(op.ResolvedVersion); err == nil {
			return op.ResolvedVersion
		}
	}
	return op.TargetVersion
}

// followRunner asks the runner for the operation and polls it until it is
// done. Every poll is the same idempotent Start: it creates the record the
// first time and returns it after that, so a server that died between
// minting the id and asking the runner resumes without a special case. The
// runner's RPC goes away while it restarts, so a failed poll is expected for
// a while, and the connection is replaced as needed; only the step timeout
// ends the wait. It returns nil, nil when the step was settled here. It owns
// client from the start, nil included, and closes whichever one is current
// at the end.
func (u *RunnerUpgrader) followRunner(ctx context.Context, op *serverlifecycle.Operation, step *serverlifecycle.NodeStep, client RunnerLifecycleClient, address string, req *serverlifecycle.Operation) (*serverlifecycle.Operation, error) {
	defer func() {
		if client != nil {
			client.Close()
		}
	}()
	log := u.log.With("operation", op.ID, "runner", step.Name)
	deadline := time.Now().Add(u.opts.StepTimeout)
	// remote starts as what the step last saw, so a resumed poll that keeps
	// failing still reports the phase it left off in. The step is not done,
	// by construction, so the loop always polls at least once.
	remote := &serverlifecycle.Operation{ID: req.ID, Phase: step.Phase, Progress: step.Progress}
	var lastErr error
	started := false
	for first := true; first || !remote.Done(); first = false {
		if !first {
			if time.Now().After(deadline) {
				detail := fmt.Sprintf("runner operation %s still %s after %s", req.ID, remote.Phase, u.opts.StepTimeout)
				if lastErr != nil {
					detail += " (" + lastErr.Error() + ")"
				}
				u.setStep(op, step, serverlifecycle.PhaseFailed, detail)
				return nil, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(u.opts.PollInterval):
			}
		}
		if client == nil {
			dialed, err := u.dial(ctx, address)
			if err != nil {
				lastErr = err
				continue
			}
			client = dialed
		}
		latest, err := client.Start(ctx, req)
		if err != nil {
			// Busy is the runner answering, not the runner restarting: some
			// other operation holds its ledger (an operator upgrading it by
			// hand, say), and waiting out the step timeout would not change
			// that. Our own operation never reads as busy, since a repeated
			// id returns the record.
			if strings.Contains(err.Error(), serverlifecycle.ErrBusy.Error()) {
				u.setStep(op, step, serverlifecycle.PhaseFailed, "runner has another operation in progress: "+err.Error())
				return nil, nil
			}
			lastErr = err
			client.Close()
			client = nil
			continue
		}
		lastErr = nil
		if !started {
			started = true
			log.Info("runner upgrade started", "runner_operation", latest.ID, "target", req.TargetVersion)
		}
		if latest.Phase != remote.Phase || latest.Progress != remote.Progress {
			u.mirror(step, latest)
			u.setStep(op, step, latest.Phase, latest.Progress)
		}
		remote = latest
	}
	return remote, nil
}

func (u *RunnerUpgrader) mirror(step *serverlifecycle.NodeStep, remote *serverlifecycle.Operation) {
	if remote.PreviousVersion != "" {
		step.PreviousVersion = remote.PreviousVersion
	}
	step.NewVersion = remote.NewVersion
}

// readyRunner reads the runner's node, giving it NotReadyGrace to become
// READY. The node it returns may still not be; the caller decides.
func (u *RunnerUpgrader) readyRunner(ctx context.Context, op *serverlifecycle.Operation, step *serverlifecycle.NodeStep) (*compute_v1alpha.Node, error) {
	deadline := time.Now().Add(u.opts.NotReadyGrace)
	waiting := false
	for {
		node, err := u.nodes.Runner(ctx, step.RunnerID)
		if err != nil {
			return nil, err
		}
		if node == nil || node.Status == compute_v1alpha.READY || time.Now().After(deadline) {
			return node, nil
		}
		if !waiting {
			waiting = true
			u.setStep(op, step, serverlifecycle.PhasePending, "waiting for the runner to be ready ("+nodeStatus(node)+")")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(u.opts.PollInterval):
		}
	}
}

// awaitReady waits for the node inventory to show the runner READY on the
// new build. A runner that reports its operation succeeded but never rejoins
// is a failed step: the fleet gate is the inventory, not the runner's word.
func (u *RunnerUpgrader) awaitReady(ctx context.Context, op *serverlifecycle.Operation, step *serverlifecycle.NodeStep, version string) error {
	deadline := time.Now().Add(u.opts.ReadyTimeout)
	for {
		node, err := u.nodes.Runner(ctx, step.RunnerID)
		if err != nil {
			return err
		}
		if node != nil && node.Status == compute_v1alpha.READY && (version == "" || node.Version == version) {
			step.NewVersion = node.Version
			u.setStep(op, step, serverlifecycle.PhaseSucceeded, "")
			return nil
		}
		if time.Now().After(deadline) {
			state := "gone from the inventory"
			if node != nil {
				state = fmt.Sprintf("%s on %s", nodeStatus(node), node.Version)
			}
			u.setStep(op, step, serverlifecycle.PhaseFailed, fmt.Sprintf("runner upgraded to %s but is %s after %s", version, state, u.opts.ReadyTimeout))
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(u.opts.PollInterval):
		}
	}
}

func nodeStatus(node *compute_v1alpha.Node) string {
	if node.Status == "" {
		return "unknown"
	}
	return strings.TrimPrefix(string(node.Status), "status.")
}

func nodeName(node *compute_v1alpha.Node) string {
	if node.Name != "" {
		return node.Name
	}
	return string(node.ID)
}

// entityNodes reads the node inventory through the entity API.
type entityNodes struct {
	eac *entityserver_v1alpha.EntityAccessClient
}

func (n entityNodes) list(ctx context.Context) ([]*compute_v1alpha.Node, error) {
	resp, err := n.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindNode))
	if err != nil {
		return nil, err
	}
	var nodes []*compute_v1alpha.Node
	for _, e := range resp.Values() {
		var node compute_v1alpha.Node
		node.Decode(e.Entity())
		if node.RunnerId == "" {
			continue
		}
		node.ID = entity.Id(e.Id())
		nodes = append(nodes, &node)
	}
	return nodes, nil
}

func (n entityNodes) Runners(ctx context.Context) ([]*compute_v1alpha.Node, error) {
	nodes, err := n.list(ctx)
	if err != nil {
		return nil, err
	}
	var runners []*compute_v1alpha.Node
	for _, node := range nodes {
		if role, _ := node.Constraints.Get("role"); role == "coordinator" {
			continue
		}
		runners = append(runners, node)
	}
	sort.Slice(runners, func(i, j int) bool { return nodeName(runners[i]) < nodeName(runners[j]) })
	return runners, nil
}

// Runner reads one node directly: the runner registers its node under the
// ident node/<runner id>, so the poll that gates each step does not list
// the whole inventory every time.
func (n entityNodes) Runner(ctx context.Context, runnerID string) (*compute_v1alpha.Node, error) {
	resp, err := n.eac.Get(ctx, string(compute_v1alpha.NewNodeId(runnerID).Id()))
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil
		}
		return nil, err
	}
	var node compute_v1alpha.Node
	node.Decode(resp.Entity().Entity())
	if node.RunnerId == "" {
		return nil, nil
	}
	node.ID = entity.Id(resp.Entity().Id())
	return &node, nil
}

func (n entityNodes) SetScheduling(ctx context.Context, node *compute_v1alpha.Node, scheduling entity.Id) error {
	_, err := n.eac.Patch(ctx, []entity.Attr{
		entity.Ref(entity.DBId, node.ID),
		entity.Ref(compute_v1alpha.NodeSchedulingId, scheduling),
	}, 0)
	return err
}

// rpcRunnerDialer reaches a runner's ledger over the coordinator's RPC state,
// the same way exec and sandbox work reach it.
func rpcRunnerDialer(rs *rpc.State) RunnerDialer {
	return func(_ context.Context, address string) (RunnerLifecycleClient, error) {
		client, err := rs.Connect(address, runnerlifecycle.Service)
		if err != nil {
			if re, ok := errors.AsType[*rpc.ResolveError](err); ok && re.Kind == rpc.ResolveLookupError {
				return nil, fmt.Errorf("%w: %w", ErrRunnerUnmanaged, err)
			}
			return nil, err
		}
		return rpcRunnerClient{client: client, lifecycle: server_v1alpha.NewServerLifecycleClient(client)}, nil
	}
}

type rpcRunnerClient struct {
	client    *rpc.NetworkClient
	lifecycle *server_v1alpha.ServerLifecycleClient
}

func (c rpcRunnerClient) Start(ctx context.Context, op *serverlifecycle.Operation) (*serverlifecycle.Operation, error) {
	results, err := c.lifecycle.Start(ctx, op.ID, string(op.Action), op.TargetVersion, op.ArtifactType, op.NoRollback, int32(op.ReadyTimeoutSeconds))
	if err != nil {
		return nil, err
	}
	return lifecyclesrv.FromRPC(results.Operation()), nil
}

func (c rpcRunnerClient) Get(ctx context.Context, id string) (*serverlifecycle.Operation, error) {
	results, err := c.lifecycle.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return lifecyclesrv.FromRPC(results.Operation()), nil
}

func (c rpcRunnerClient) Close() { c.client.Close() }
