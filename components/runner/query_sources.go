package runner

import (
	"context"
	"errors"
	"strings"
	"time"

	events "github.com/containerd/containerd/api/events"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/typeurl/v2"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/pkg/entity"
	query "miren.dev/runtime/pkg/portalquery"
)

func (r *SandboxHost) queryEngine() query.Engine {
	return query.Engine{Sources: map[string]query.CustomSource{
		"sandboxes": {
			Fields:        []string{"sandbox_id", "container_id", "app", "version", "pid", "state", "cgroup"},
			NumericFields: []string{"pid"}, Snapshots: r.querySandboxes,
		},
		"sandbox_events": {
			Fields:        []string{"sandbox_id", "container_id", "app", "version", "action", "pid", "exit_status"},
			NumericFields: []string{"pid", "exit_status"}, Events: r.querySandboxEvents,
		},
	}}
}

// Only explicitly selected runtime metadata crosses the query boundary. Container
// specs and sandbox entities may contain resolved secrets and are never returned.
func (r *SandboxHost) queryContainer(ctx context.Context, c containerd.Container) (map[string]any, error) {
	labels, err := c.Labels(ctx)
	if err != nil {
		return nil, err
	}
	id := labels[sandbox.SandboxEntityLabel]
	if id == "" {
		return nil, nil
	}
	version := labels["runtime.computer/version-entity"]
	app := ""
	if version != "" {
		var v core_v1alpha.AppVersion
		if err := r.access.entityBase.GetById(ctx, entity.Id(version), &v); err == nil {
			app = strings.TrimPrefix(v.App.String(), "app/")
		}
	}
	return map[string]any{"sandbox_id": id, "container_id": c.ID(), "version": version, "app": app}, nil
}

func (r *SandboxHost) querySandboxes(ctx context.Context, req query.MonitorRequest) (query.Snapshot, error) {
	ctx = namespaces.WithNamespace(ctx, r.deps.Namespace)
	containers, err := r.deps.CC.Containers(ctx)
	if err != nil {
		return query.Snapshot{}, err
	}
	rows := make([]map[string]any, 0)
	for _, c := range containers {
		row, err := r.queryContainer(ctx, c)
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return query.Snapshot{}, err
		}
		if row == nil {
			continue
		}
		row["pid"], row["state"], row["cgroup"] = uint32(0), "no_task", ""
		if task, err := c.Task(ctx, nil); err == nil {
			status, err := task.Status(ctx)
			if err != nil && !errdefs.IsNotFound(err) {
				return query.Snapshot{}, err
			}
			if err == nil {
				row["pid"], row["state"] = task.Pid(), string(status.Status)
			}
		} else if !errdefs.IsNotFound(err) {
			return query.Snapshot{}, err
		}
		if spec, err := c.Spec(ctx); err == nil && spec.Linux != nil {
			row["cgroup"] = spec.Linux.CgroupsPath
		} else if err != nil && !errdefs.IsNotFound(err) {
			return query.Snapshot{}, err
		}
		if req.Matches(query.Event{Fields: row}) {
			rows = append(rows, row)
		}
	}
	return query.Snapshot{Source: req.Source, Time: time.Now(), Data: rows}, nil
}

func sandboxTaskEvent(value any) (string, map[string]any) {
	switch v := value.(type) {
	case *events.TaskStart:
		return v.ContainerID, map[string]any{"action": "start", "pid": v.Pid}
	case *events.TaskExit:
		// Exec-process exits are not container task exits.
		if v.ID != v.ContainerID {
			return "", nil
		}
		return v.ContainerID, map[string]any{"action": "exit", "pid": v.Pid, "exit_status": v.ExitStatus}
	case *events.TaskOOM:
		return v.ContainerID, map[string]any{"action": "oom"}
	}
	return "", nil
}

func (r *SandboxHost) querySandboxEvents(ctx context.Context, _ query.MonitorRequest, emit func(query.Event) error) error {
	ctx, cancel := context.WithCancel(namespaces.WithNamespace(ctx, r.deps.Namespace))
	defer cancel()
	stream, errs := r.deps.CC.EventService().Subscribe(ctx,
		`topic=="/tasks/start"`, `topic=="/tasks/exit"`, `topic=="/tasks/oom"`, `topic=="/containers/delete"`)
	// Keep metadata until deletion, so a task exit still has its identity even
	// when the controller deletes the container before we process the event.
	known := make(map[string]map[string]any)
	containers, err := r.deps.CC.Containers(ctx)
	if err != nil {
		return err
	}
	for _, c := range containers {
		row, err := r.queryContainer(ctx, c)
		if err != nil && !errdefs.IsNotFound(err) {
			return err
		}
		if row != nil {
			known[c.ID()] = row
		}
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-errs:
			if !ok || err == nil {
				return errors.New("containerd event subscription closed")
			}
			return err
		case envelope, ok := <-stream:
			if !ok {
				return errors.New("containerd event subscription closed")
			}
			if envelope.Namespace != r.deps.Namespace {
				continue
			}
			value, err := typeurl.UnmarshalAny(envelope.Event)
			if err != nil {
				return err
			}
			if deleted, ok := value.(*events.ContainerDelete); ok {
				delete(known, deleted.ID)
				continue
			}
			id, fields := sandboxTaskEvent(value)
			if id == "" {
				continue
			}
			row := known[id]
			if row == nil {
				c, err := r.deps.CC.LoadContainer(ctx, id)
				if errdefs.IsNotFound(err) {
					continue
				}
				if err != nil {
					return err
				}
				row, err = r.queryContainer(ctx, c)
				if errdefs.IsNotFound(err) {
					continue
				}
				if err != nil {
					return err
				}
				if row == nil {
					continue
				}
				known[id] = row
			}
			for key, value := range row {
				fields[key] = value
			}
			if err := emit(query.Event{Time: envelope.Timestamp, Fields: fields, Data: fields}); err != nil {
				return err
			}
		}
	}
}
