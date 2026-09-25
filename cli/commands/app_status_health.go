package commands

import (
	"fmt"
	"strings"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
)

type serviceHealth struct {
	Service         string `json:"service"`
	Running         int    `json:"running"`
	Dead            int    `json:"dead"`
	CrashStreak     int    `json:"crash_streak"`
	CrashLooping    bool   `json:"crash_looping"`
	CooldownSeconds int32  `json:"cooldown_seconds,omitempty"`
	LastExitCode    *int64 `json:"last_exit_code,omitempty"`
	LastFailureLog  string `json:"last_failure_log,omitempty"`
}

func renderServiceHealth(health []serviceHealth) string {
	var b strings.Builder
	for _, svc := range health {
		state := fmt.Sprintf("%d running, %d dead", svc.Running, svc.Dead)
		if svc.CrashLooping {
			state += fmt.Sprintf("; %d crashes in current streak, retry in %s", svc.CrashStreak, formatDuration(time.Duration(svc.CooldownSeconds)*time.Second))
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

func fetchServiceHealth(ctx *Context, app string) ([]serviceHealth, error) {
	cl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	res, err := app_v1alpha.NewAppStatusClient(cl).AppInfo(ctx, app)
	if err != nil {
		return nil, err
	}
	if res.Status().Health() == "unknown" && res.Status().ActiveVersion() != "" {
		return nil, fmt.Errorf("service health unavailable for %s", app)
	}
	var health []serviceHealth
	for _, svc := range res.Status().Services() {
		h := serviceHealth{Service: svc.Service(), Running: int(svc.Running()), Dead: int(svc.Dead()), CrashStreak: int(svc.CrashCount()), CrashLooping: svc.Health() == "crashed", CooldownSeconds: svc.CooldownSeconds()}
		if svc.HasLastExitCode() {
			code := svc.LastExitCode()
			h.LastExitCode = &code
		}
		if svc.HasLastFailureAt() && svc.LastFailureSandbox() != "" && svc.Health() == "crashed" {
			h.LastFailureLog = recentSandboxFailureLog(ctx, svc.LastFailureSandbox(), standard.FromTimestamp(svc.LastFailureAt()))
		}
		health = append(health, h)
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
