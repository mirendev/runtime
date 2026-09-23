package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	runcopts "github.com/containerd/containerd/api/types/runc/options"
	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/errdefs"
	"github.com/containerd/typeurl/v2"
	"github.com/opencontainers/runtime-spec/specs-go"
	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/appspec"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/entity"
)

// Checkpoints are local, single-use caches, never images or deploy artifacts.
// A retained container owns its writable snapshot; neither is shared with a
// replacement sandbox. Interrupted operations discard the cache and cold boot.
type checkpointRuntime interface {
	Save(context.Context, *compute.Sandbox, *entity.Meta) error
	Restore(context.Context, *compute.Sandbox, *entity.Meta) error
	Fallback(context.Context, *compute.Sandbox) error
}

type containerCheckpoint struct{ c *SandboxController }

type checkpointManifest struct {
	SpecHash    string
	ConfigHash  string
	OCIHash     string
	SnapshotKey string
	BootID      string
	CreatedAt   time.Time
}

func (m checkpointManifest) matches(sb *compute.Sandbox, info containers.Container, configHash, bootID string, now time.Time) bool {
	return info.Spec != nil && m.ConfigHash != "" && m.ConfigHash == configHash &&
		m.SpecHash == checkpointHash(sb.Spec) && m.OCIHash == checkpointHash(info.Spec.GetValue()) &&
		m.SnapshotKey == info.SnapshotKey && m.BootID == bootID &&
		!now.Before(m.CreatedAt) && now.Sub(m.CreatedAt) < computeapi.CheckpointRetention
}

func checkpointHash(v any) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func (c *SandboxController) checkpointPath(id entity.Id) string {
	return filepath.Join(c.DataPath, "checkpoints", id.PathSafe())
}

func (c *SandboxController) pruneCheckpointFiles(owners map[string]bool) error {
	root := filepath.Join(c.DataPath, "checkpoints")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !owners[entry.Name()] {
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkCRIU(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Probe the running kernel and privileges, not the distribution. The actual
	// containerd checkpoint remains the final capability/compatibility test.
	return exec.CommandContext(ctx, "criu", "check").Run()
}

func (c *SandboxController) reconcileCheckpoint(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) error {
	current, _, err := c.ops.GetSandbox(ctx, sb.ID.String())
	if err != nil {
		return err
	}
	if current.Status != sb.Status {
		return nil
	}
	if sb.Status == compute.HIBERNATED {
		return nil // The pool owns wake-up, invalidation, and retention.
	}
	runtime := c.checkpoints
	if runtime == nil {
		runtime = &containerCheckpoint{c}
	}
	ctx = namespaces.WithNamespace(ctx, c.Namespace)
	opCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if sb.Status == compute.HIBERNATING {
		err = runtime.Save(opCtx, sb, meta)
		if err == nil {
			err = c.checkpointTransition(ctx, sb.ID, compute.HIBERNATING, compute.HIBERNATED)
		}
	} else {
		err = runtime.Restore(opCtx, sb, meta)
		if err == nil {
			err = c.checkpointTransition(ctx, sb.ID, compute.RESTORING, compute.RUNNING)
		}
	}
	if err == nil {
		c.Log.Info("sandbox checkpoint transition complete", "sandbox", sb.ID, "from", sb.Status)
		return nil
	}
	c.Log.Debug("checkpoint unavailable, using ordinary sandbox lifecycle", "sandbox", sb.ID, "error", err)
	return runtime.Fallback(ctx, sb)
}

func (r *containerCheckpoint) Fallback(ctx context.Context, sb *compute.Sandbox) error {
	c := r.c
	if sb.Status == compute.HIBERNATING {
		// Unsupported runners must retain ordinary SIGTERM/shutdown-timeout
		// semantics. No restored process or cold replacement exists yet.
		return c.StopSandbox(ctx, sb.ID, sb)
	}
	// Fence even a partially restored task's watcher before deliberately
	// killing it; its exit is cleanup, not a failure of the cold replacement.
	if err := c.fenceRestoredTask(ctx, sb.ID); err != nil {
		return err
	}
	// Discard must confirm tasks are gone before allowing cold startup. The
	// ordinary stop path is best-effort and by itself is not that guarantee.
	if cleanupErr := r.Discard(ctx, sb); cleanupErr != nil {
		return fmt.Errorf("discarding checkpoint: %w", cleanupErr)
	}
	current, meta, getErr := c.ops.GetSandbox(ctx, sb.ID.String())
	if getErr != nil {
		return getErr
	}
	if current.Status == compute.RESTORING {
		if _, err := c.validateCheckpointPool(ctx, current, meta); err == nil {
			// Keep the pending capacity reservation throughout fallback, so waiting
			// requests neither fail fast nor cause a second instance to be created.
			if err := c.stopSandbox(ctx, sb.ID, sb, compute.RESTORING); err != nil {
				return err
			}
			return c.createSandboxViaSaga(ctx, current, false)
		}
	}
	return c.StopSandbox(ctx, sb.ID, sb)
}

func (c *SandboxController) checkpointTransition(ctx context.Context, id entity.Id, from, to compute.SandboxStatus) error {
	sb, meta, err := c.ops.GetSandbox(ctx, id.String())
	if err != nil {
		return err
	}
	if sb.Status != from {
		return fmt.Errorf("checkpoint claim changed from %s to %s", from, sb.Status)
	}
	patch := &compute.Sandbox{Status: to}
	// Only completed checkpoint transitions stamp lifecycle timestamps.
	//exhaustive:ignore
	switch to {
	case compute.RUNNING:
		patch.LastActivity = time.Now()
	case compute.HIBERNATED:
		patch.HibernatedAt = time.Now()
	}
	_, err = c.ops.PatchSandbox(ctx, entity.New(entity.DBId, id, patch.Encode).Attrs(), meta.Revision)
	return err
}

func checkpointContainerID(sb *compute.Sandbox) string {
	return fmt.Sprintf("%s-%s", containerPrefix(sb.ID), sb.Spec.Container[0].Name)
}

func (c *SandboxController) validateCheckpointPool(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) (string, error) {
	md := core_v1alpha.MD(meta.Entity)
	id, ok := md.Labels.Get("pool")
	if !ok {
		return "", errors.New("checkpoint has no owning pool")
	}
	resp, err := c.EAC.Get(ctx, id)
	if err != nil {
		return "", err
	}
	var pool compute.SandboxPool
	pool.Decode(resp.Entity().Entity())
	if !slices.Contains(pool.ReferencedByVersions, sb.Spec.Version) || !reflect.DeepEqual(pool.SandboxSpec, sb.Spec) {
		return "", errors.New("checkpoint pool was decommissioned or changed")
	}
	return c.checkpointConfigHash(ctx, sb, &pool)
}

// Check the live config, not only the pool's last reconciled spec. In particular,
// addon bindings can change before the launcher refreshes a pool. CRIU resumes
// cached in-process values; editing an OCI env entry cannot update them.
func (c *SandboxController) checkpointConfigHash(ctx context.Context, sb *compute.Sandbox, pool *compute.SandboxPool) (string, error) {
	resp, err := c.EAC.Get(ctx, sb.Spec.Version.String())
	if err != nil {
		return "", err
	}
	var ver core_v1alpha.AppVersion
	ver.Decode(resp.Entity().Entity())
	if ver.App == "" || ver.App != pool.App || ver.ID != sb.Spec.Version {
		return "", errors.New("checkpoint has no matching app version")
	}
	cfg, err := coreutil.ResolveRuntimeConfig(ctx, c.EAC, &ver)
	if err != nil {
		return "", err
	}
	for _, v := range cfg.Variables {
		if v.Sensitive || v.Backend != "" || v.Source == coreutil.SourceAddon {
			return "", errors.New("checkpoint cannot refresh an environment credential")
		}
	}
	for _, svc := range cfg.Services {
		if svc.Name != pool.Service {
			continue
		}
		for _, v := range svc.Env {
			if v.Sensitive || v.Backend != "" {
				return "", errors.New("checkpoint cannot refresh a service credential")
			}
		}
	}
	appResp, err := c.EAC.Get(ctx, ver.App.String())
	if err != nil {
		return "", err
	}
	appMD := core_v1alpha.MD(appResp.Entity().Entity())
	image := ver.ImageUrl
	for _, svc := range cfg.Services {
		if svc.Name == pool.Service && svc.Image != "" {
			image = containerdx.NormalizeImageReference(svc.Image)
		}
	}
	desired, err := appspec.Build(nil, appspec.Options{
		AppID: ver.App, AppName: appMD.Name, Version: &ver, Config: cfg, Service: pool.Service, Image: image,
	})
	if err != nil {
		return "", err
	}
	// Build assembles user env from a map; its order has no significance here.
	actual := sb.Spec
	actual.Container = slices.Clone(sb.Spec.Container)
	actual.Container[0].Env = slices.Clone(sb.Spec.Container[0].Env)
	slices.Sort(actual.Container[0].Env)
	slices.Sort(desired.Container[0].Env)
	if !reflect.DeepEqual(actual, *desired) {
		return "", errors.New("checkpoint app version, config, or environment changed")
	}
	return checkpointHash(struct {
		Version core_v1alpha.AppVersion
		Config  core_v1alpha.ConfigSpec
	}{ver, *cfg}), nil
}

func (c *SandboxController) fenceRestoredTask(ctx context.Context, id entity.Id) error {
	sb, meta, err := c.ops.GetSandbox(ctx, id.String())
	if err != nil {
		return err
	}
	if sb.Status != compute.RESTORING {
		return errors.New("restore claim was revoked")
	}
	_, err = c.ops.PatchSandbox(ctx, entity.New(entity.DBId, id,
		(&compute.Sandbox{RestoredAt: time.Now()}).Encode).Attrs(), meta.Revision)
	return err
}

func (r *containerCheckpoint) Save(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) error {
	c := r.c
	if !computeapi.CheckpointEligible(sb) || c.WorkloadIssuer != nil {
		return errors.New("sandbox has resources not supported by checkpointing")
	}
	configHash, err := c.validateCheckpointPool(ctx, sb, meta)
	if err != nil {
		return err
	}
	c.checkpointProbe.Do(func() { c.checkpointProbeErr = checkCRIU(ctx) })
	if c.checkpointProbeErr != nil {
		return c.checkpointProbeErr
	}
	dir := c.checkpointPath(sb.ID)
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return err
	}
	// An existing directory is evidence of an interrupted operation. Never
	// replace its memory image with a checkpoint of an uncertain live task.
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	cont, err := c.CC.LoadContainer(ctx, checkpointContainerID(sb))
	if err != nil {
		return err
	}
	info, err := cont.Info(ctx)
	if err != nil {
		return err
	}
	if info.Runtime.Name != "io.containerd.runc.v2" || info.SnapshotKey == "" || info.Spec == nil {
		return errors.New("checkpoint requires a private runc snapshot")
	}
	manifest := checkpointManifest{SpecHash: checkpointHash(sb.Spec), ConfigHash: configHash, OCIHash: checkpointHash(info.Spec.GetValue()),
		SnapshotKey: info.SnapshotKey, BootID: string(bootID), CreatedAt: time.Now()}
	task, err := cont.Task(ctx, cleanupAttach())
	if err != nil {
		return err
	}
	if c.portMonitor != nil {
		c.portMonitor.StopMonitoring(cont.ID())
	}
	// HIBERNATING has already withdrawn ingress capacity. Established TCP and
	// external Unix sockets are intentionally NOT supported: CRIU must refuse
	// them rather than resurrect a connection to an external service.
	_, err = task.Checkpoint(ctx, checkpointTaskOptions(filepath.Join(dir, "images")))
	if err != nil {
		return err
	}
	if _, err = task.Delete(ctx); err != nil {
		return err
	}
	if err = c.deleteEndpoints(ctx, sb.ID, entityFallbackIPs(sb)); err != nil {
		return err
	}
	le, _ := sandboxMetricsIdentity(sb, c.NodeId.String())
	if c.Metrics != nil {
		_ = c.Metrics.Remove(le)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(dir, "ready.json"), data, 0600)
}

func checkpointTaskOptions(path string) containerd.CheckpointTaskOpts {
	return func(info *containerd.CheckpointTaskInfo) error {
		info.Options = &runcopts.CheckpointOptions{ImagePath: path, Exit: true}
		return nil
	}
}

func (r *containerCheckpoint) Restore(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) error {
	c := r.c
	if !computeapi.CheckpointEligible(sb) || c.WorkloadIssuer != nil {
		return errors.New("sandbox is no longer checkpoint eligible")
	}
	configHash, err := c.validateCheckpointPool(ctx, sb, meta)
	if err != nil {
		return err
	}
	dir := c.checkpointPath(sb.ID)
	data, err := os.ReadFile(filepath.Join(dir, "ready.json"))
	if err != nil {
		return err
	}
	var manifest checkpointManifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	cont, err := c.CC.LoadContainer(ctx, checkpointContainerID(sb))
	if err != nil {
		return err
	}
	info, err := cont.Info(ctx)
	if err != nil {
		return err
	}
	if !manifest.matches(sb, info, configHash, string(bootID), time.Now()) {
		return errors.New("checkpoint is stale")
	}
	// Claim BEFORE making a process. A crash after this rename must cold boot,
	// never replay an image whose restored process may already have run.
	if err = os.Rename(filepath.Join(dir, "ready.json"), filepath.Join(dir, "consumed.json")); err != nil {
		return err
	}
	// Keep the pause task (and IP reservation) while asleep, but replace its
	// namespaces on wake. CRIU network-lock rules must not survive into the
	// restored app's netns. No ad-hoc firewall surgery is needed.
	pause, err := c.CC.LoadContainer(ctx, pauseContainerId(sb.ID))
	if err != nil {
		return err
	}
	if err = deleteCheckpointTask(ctx, pause); err != nil {
		return err
	}
	if err = c.fenceRestoredTask(ctx, sb.ID); err != nil {
		return err
	}
	// Exit watchers must carry this task generation, not the pre-restore one.
	sb, _, err = c.ops.GetSandbox(ctx, sb.ID.String())
	if err != nil {
		return err
	}
	addresses := make([]string, 0, len(sb.Network))
	for _, n := range sb.Network {
		addresses = append(addresses, n.Address)
	}
	ep, err := c.ops.RebuildEndpointConfig(addresses)
	if err != nil {
		return err
	}
	pt, err := c.BootInitialTask(ctx, sb, ep, pause, meta.ShortId())
	if err != nil {
		return err
	}
	spec, err := cont.Spec(ctx)
	if err != nil {
		return err
	}
	rebindCheckpointNamespaces(spec, pt.Pid())
	if err = cont.Update(ctx, func(_ context.Context, _ *containerd.Client, record *containers.Container) error {
		record.Spec, err = typeurl.MarshalAny(spec)
		return err
	}); err != nil {
		return err
	}
	c.registerSandboxDNS(ctx, sb, ep, meta)
	sl := c.logConsumer(sb, sb.Spec.Container[0].Name, meta.ShortId())
	task, err := cont.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, sl, sl.Stderr())),
		containerd.WithRestoreImagePath(filepath.Join(dir, "images")))
	if err != nil {
		return err
	}
	// Start performs the actual runc restore. Do not probe or advertise the
	// restored listener until it has returned successfully.
	if err = task.Start(ctx); err != nil {
		return err
	}
	ports, wait := tcpPortsToWait(cont.ID(), sb.Spec.Container[0].Port)
	if len(ports) > 0 && len(ep.Addresses) > 0 {
		c.portMonitor.MonitorContainer(cont.ID(), ep.Addresses[0].Addr().String(), int(pt.Pid()), ports)
	}
	for _, p := range wait {
		if err = c.WaitForPort(ctx, p.ID, p.Port, resolvePortWaitTimeout(sb.Spec.PortWaitTimeout)); err != nil {
			return err
		}
	}
	status, err := task.Status(ctx)
	if err != nil {
		return err
	}
	if status.Status != containerd.Running {
		return errors.New("restored task exited before readiness")
	}
	if err = c.ensureMetrics(ctx, sb); err != nil {
		return err
	}
	currentConfigHash, err := c.validateCheckpointPool(ctx, sb, meta)
	if err != nil {
		return err
	}
	if currentConfigHash != configHash {
		return errors.New("checkpoint config changed during restore")
	}
	if err = c.UpdateServices(ctx, sb, meta, ep); err != nil {
		return err
	}
	exitCh, err := task.Wait(ctx)
	if err != nil {
		return err
	}
	go c.monitorTaskExit(sb, cont.ID(), sb.Spec.Container[0].Name, task, exitCh)
	return os.RemoveAll(dir)
}

func rebindCheckpointNamespaces(spec *specs.Spec, pid uint32) {
	for i := range spec.Linux.Namespaces {
		ns := &spec.Linux.Namespaces[i]
		var name string
		switch ns.Type {
		case specs.NetworkNamespace:
			name = "net"
		case specs.IPCNamespace:
			name = "ipc"
		case specs.UTSNamespace:
			name = "uts"
		case specs.TimeNamespace:
			name = "time"
		case specs.PIDNamespace, specs.MountNamespace, specs.UserNamespace, specs.CgroupNamespace:
			continue
		default:
			continue
		}
		ns.Path = fmt.Sprintf("/proc/%d/ns/%s", pid, name)
	}
}

func deleteCheckpointTask(ctx context.Context, cont containerd.Container) error {
	task, err := cont.Task(ctx, cleanupAttach())
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = task.Delete(ctx, containerd.WithProcessKill)
	return err
}

func (r *containerCheckpoint) Discard(ctx context.Context, sb *compute.Sandbox) error {
	for _, spec := range sb.Spec.Container {
		id := fmt.Sprintf("%s-%s", containerPrefix(sb.ID), spec.Name)
		cont, err := r.c.CC.LoadContainer(ctx, id)
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err = deleteCheckpointTask(ctx, cont); err != nil {
			return err
		}
	}
	return os.RemoveAll(r.c.checkpointPath(sb.ID))
}
