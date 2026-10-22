package commands

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/ui"
)

const defaultExecService = "web"

// sandboxCandidate contains only the selection details used in CLI notices.
type sandboxCandidate struct {
	ID      entity.Id
	Brief   string
	Service string
}

func selectAppSandbox(ctx *Context, appName, service string) (sandboxCandidate, int, error) {
	client, err := ctx.RPCClient("dev.miren.runtime/sandboxes")
	if err == nil {
		defer client.Close()
		err = requireRPCMethod(ctx, client, "selectForApp")
	}
	if err != nil {
		return sandboxCandidate{}, 0, err
	}
	result, err := compute_v1alpha.NewSandboxesClient(client).SelectForApp(ctx, appName, service)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return sandboxCandidate{}, 0, unknownAppError(appName, err, ctx.Verbose())
		}
		return sandboxCandidate{}, 0, err
	}
	switch result.Failure() {
	case "unknown_service":
		return sandboxCandidate{}, 0, unknownServiceError(appName, service, result.Services())
	case "missing_default":
		return sandboxCandidate{}, 0, noDefaultServiceError(appName, result.Services())
	case "no_running":
		return sandboxCandidate{}, 0, noRunningSandboxError(appName, service)
	case "":
	default:
		return sandboxCandidate{}, 0, fmt.Errorf("unexpected sandbox selection failure: %s", result.Failure())
	}
	chosen := result.Selection()
	if chosen == nil || chosen.Id() == "" {
		return sandboxCandidate{}, 0, fmt.Errorf("sandbox selection returned no sandbox")
	}
	return sandboxCandidate{ID: entity.Id(chosen.Id()), Brief: chosen.ShortId(), Service: chosen.Service()}, int(chosen.CandidateCount()), nil
}

func unknownAppError(appName string, cause error, verbose bool) error {
	return &ui.Diagnostic{
		Summary: fmt.Sprintf("no app named %q", appName),
		Actions: []ui.Action{
			{Command: "miren app list", Note: "see the apps on this cluster"},
		},
		Cause:     cause,
		ShowCause: verbose,
	}
}

func unknownServiceError(appName, service string, known []string) error {
	d := &ui.Diagnostic{
		Summary: fmt.Sprintf("app %q has no service named %q", appName, service),
		Actions: []ui.Action{
			{Command: "miren sandbox list --app " + appName, Note: "see what's running for it"},
		},
	}

	slices.Sort(known)
	if len(known) > 0 {
		d.Detail = fmt.Sprintf("Its services are: %s.", strings.Join(known, ", "))
	} else {
		d.Detail = "It has no sandbox pools at all, so nothing has been deployed for it yet."
	}

	return d
}

// noDefaultServiceError reports that exec's default service has nothing
// running, when the app has other services that do. Handing over one of those
// instead would be a silent substitution of a background worker for the process
// serving traffic, so we say what happened and let the caller choose.
func noDefaultServiceError(appName string, running []string) error {
	d := &ui.Diagnostic{
		Summary: fmt.Sprintf("no running %s instance for app %q", defaultExecService, appName),
		Detail: fmt.Sprintf("Without --service, exec goes to the %s service, and none of "+
			"its sandboxes are up. This is usually a deploy in progress, in which "+
			"case trying again shortly will find one.", defaultExecService),
		Actions: []ui.Action{
			{Command: "miren sandbox list --all --app " + appName, Note: "see this app's sandboxes and their state"},
		},
	}

	if len(running) > 0 {
		d.Detail += fmt.Sprintf(" Still running: %s.", strings.Join(running, ", "))
		d.Actions = append([]ui.Action{{
			Command: fmt.Sprintf("miren sandbox exec -a %s --service %s", appName, running[0]),
			Note:    "exec into that service instead",
		}}, d.Actions...)
	}

	return d
}

func noRunningSandboxError(appName, service string) error {
	target := fmt.Sprintf("app %q", appName)
	if service != "" {
		target = fmt.Sprintf("service %q of app %q", service, appName)
	}

	return &ui.Diagnostic{
		Summary: fmt.Sprintf("no running sandbox for %s", target),
		// The distinction matters for what to try next: a sandbox that exists
		// but is still starting will fix itself, and one that never starts is a
		// different problem than an app that was never deployed.
		//
		// The listing caveat earns its place because the action below sends the
		// reader straight to a command that can contradict this message: `app
		// run` consoles and task runs get a sandbox without a pool, and the
		// listing resolves an app for those while exec won't target them.
		Detail: "Nothing is running and placed on a node right now. A sandbox that is " +
			"still starting up doesn't count, since exec needs one that's already up. " +
			"The listing below can still show a sandbox here: consoles from `miren app " +
			"run` and task runs aren't part of a service, so exec won't pick one.",
		Actions: []ui.Action{
			{Command: "miren sandbox list --all --app " + appName, Note: "see this app's sandboxes and their state"},
			{Command: "miren app run -a " + appName, Note: "run a command in a fresh ephemeral sandbox instead"},
		},
	}
}
