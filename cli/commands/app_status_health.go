package commands

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/standard"
)

const failureWindow = 10 * time.Minute

type serviceHealth struct {
	Service        string `json:"service"`
	Running        int    `json:"running"`
	Dead           int    `json:"dead"`
	CrashStreak    int    `json:"crash_streak"`
	CrashLooping   bool   `json:"crash_looping"`
	LastExitCode   *int64 `json:"last_exit_code,omitempty"`
	LastFailureLog string `json:"last_failure_log,omitempty"`

	lastSandbox string
	lastExit    time.Time
	lastFailure time.Time
}

type sandboxHealthRecord struct {
	Sandbox compute_v1alpha.Sandbox
	Pool    string
	Updated time.Time
}

func summarizeServiceHealth(pools []compute_v1alpha.SandboxPool, sandboxes []sandboxHealthRecord, now time.Time) []serviceHealth {
	byPool := make(map[string]*serviceHealth)
	byService := make(map[string]*serviceHealth)
	for _, pool := range pools {
		h := byService[pool.Service]
		if h == nil {
			h = &serviceHealth{Service: pool.Service}
			byService[pool.Service] = h
		}
		byPool[pool.ID.String()] = h
		if pool.DesiredInstances > pool.ReadyInstances && !pool.LastCrashTime.After(now) && pool.LastCrashTime.After(now.Add(-failureWindow)) {
			h.CrashStreak += int(pool.ConsecutiveCrashCount)
			if pool.CooldownUntil.After(now) && pool.ConsecutiveCrashCount >= 2 {
				h.CrashLooping = true
			}
		}
	}
	for _, record := range sandboxes {
		sb := record.Sandbox
		h := byPool[record.Pool]
		if h == nil {
			continue
		}
		switch sb.Status {
		case compute_v1alpha.RUNNING:
			h.Running++
		case compute_v1alpha.DEAD:
			h.Dead++
			if !sb.Exit.At.IsZero() && (h.LastExitCode == nil || sb.Exit.At.After(h.lastExit)) {
				code := sb.Exit.Code
				h.LastExitCode = &code
				h.lastExit = sb.Exit.At
			}
			if sb.Exit.Code != 0 && !sb.Exit.At.IsZero() && (h.lastSandbox == "" || sb.Exit.At.After(h.lastFailure)) {
				h.lastFailure = sb.Exit.At
				h.lastSandbox = sb.ID.String()
			}
		case compute_v1alpha.PENDING, compute_v1alpha.NOT_READY, compute_v1alpha.STOPPED:
		}
	}
	result := make([]serviceHealth, 0, len(byService))
	for _, h := range byService {
		result = append(result, *h)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Service < result[j].Service })
	return result
}

func renderServiceHealth(health []serviceHealth) string {
	var b strings.Builder
	for _, svc := range health {
		state := fmt.Sprintf("%d running, %d dead", svc.Running, svc.Dead)
		if svc.CrashStreak > 0 {
			state += fmt.Sprintf("; %d crashes in current streak (last within 10m)", svc.CrashStreak)
		}
		if svc.CrashLooping {
			state = infoRed.Render("crash-looping") + ", " + state
		}
		fmt.Fprintf(&b, "  %s: %s\n", svc.Service, state)
		if svc.LastExitCode != nil {
			fmt.Fprintf(&b, "    Last exit code: %d\n", *svc.LastExitCode)
		}
		if svc.LastFailureLog != "" {
			b.WriteString("    Last failure logs:\n")
			for _, line := range strings.Split(svc.LastFailureLog, "\n") {
				fmt.Fprintf(&b, "      %s\n", line)
			}
		}
	}
	return b.String()
}

func activeServicePools(pools []compute_v1alpha.SandboxPool, version entity.Id) []compute_v1alpha.SandboxPool {
	var active []compute_v1alpha.SandboxPool
	for _, pool := range pools {
		if version == "" || slices.Contains(pool.ReferencedByVersions, version) || len(pool.ReferencedByVersions) == 0 && pool.SandboxSpec.Version == version {
			active = append(active, pool)
		}
	}
	return active
}

func fetchServiceHealth(ctx *Context, app string) ([]serviceHealth, error) {
	cl, err := ctx.RPCClient("entities")
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	eac := entityserver_v1alpha.NewEntityAccessClient(cl)
	var appEntity core_v1alpha.App
	if err := entityserver.NewClient(ctx.Log, eac).Get(ctx, app, &appEntity); err != nil {
		return nil, fmt.Errorf("get app %q: %w", app, err)
	}
	var pools []compute_v1alpha.SandboxPool
	for cursor := ""; ; {
		page, err := eac.ListPage(ctx, entity.Ref(compute_v1alpha.SandboxPoolAppId, appEntity.ID), cursor, 200)
		if err != nil {
			return nil, err
		}
		for _, entry := range page.Values() {
			var pool compute_v1alpha.SandboxPool
			pool.Decode(entry.Entity())
			pools = append(pools, pool)
		}
		cursor = page.Cursor()
		if cursor == "" {
			break
		}
	}
	pools = activeServicePools(pools, appEntity.ActiveVersion)
	if len(pools) == 0 {
		return []serviceHealth{}, nil
	}
	kind, err := eac.LookupKind(ctx, "sandbox")
	if err != nil {
		return nil, err
	}
	var sandboxes []sandboxHealthRecord
	selected := make(map[string]bool, len(pools))
	for _, pool := range pools {
		selected[pool.ID.String()] = true
	}
	// Pool is a metadata label, not an indexed attribute. Page the kind index
	// rather than materializing every sandbox in the cluster in one RPC.
	for cursor := ""; ; {
		page, err := eac.ListPage(ctx, kind.Attr(), cursor, 200)
		if err != nil {
			return nil, err
		}
		for _, entry := range page.Values() {
			var meta core_v1alpha.Metadata
			meta.Decode(entry.Entity())
			pool, _ := meta.Labels.Get("pool")
			if !selected[pool] {
				continue
			}
			var sb compute_v1alpha.Sandbox
			sb.Decode(entry.Entity())
			sandboxes = append(sandboxes, sandboxHealthRecord{Sandbox: sb, Pool: pool, Updated: time.UnixMilli(entry.UpdatedAt())})
		}
		cursor = page.Cursor()
		if cursor == "" {
			break
		}
	}
	health := summarizeServiceHealth(pools, sandboxes, time.Now())
	for i := range health {
		if health[i].lastSandbox != "" && health[i].lastFailure.After(time.Now().Add(-failureWindow)) && health[i].CrashStreak > 0 {
			health[i].LastFailureLog = recentSandboxFailureLog(ctx, health[i].lastSandbox, health[i].lastFailure)
		}
	}
	return health, nil
}

func recentSandboxFailureLog(ctx *Context, sandbox string, exit time.Time) string {
	cl, err := ctx.RPCClient(rpcLogs)
	if err != nil {
		return ""
	}
	defer cl.Close()
	logs, err := app_v1alpha.NewLogsClient(cl).SandboxLogs(ctx, sandbox, standard.ToTimestamp(exit.Add(-time.Minute)), false)
	if err != nil {
		return ""
	}
	var lines []string
	for _, log := range logs.Logs() {
		if log.HasTimestamp() && standard.FromTimestamp(log.Timestamp()).After(exit.Add(time.Second)) {
			continue
		}
		if line := strings.TrimSpace(log.Line()); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	for i, line := range lines {
		if len(line) > 200 {
			lines[i] = line[:200] + "..."
		}
	}
	return strings.Join(lines, "\n")
}
