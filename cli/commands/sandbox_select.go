package commands

import (
	"cmp"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/ui"
)

// defaultExecService is the service we exec into when the caller didn't name
// one. An app's other services exist to do work off the request path, so "the
// app" almost always means the thing serving traffic.
const defaultExecService = "web"

// sandboxCandidate is a running sandbox that could serve an exec, reduced to
// the fields the choice actually turns on. Narrowing works on these rather than
// on entities so it stays a pure function that can be tested without a server.
type sandboxCandidate struct {
	// ID is the full entity id, which is what the exec RPC takes.
	ID entity.Id
	// Brief is the short id, for telling the user where they landed.
	Brief   string
	Service string
	Version entity.Id
}

// selectAppSandbox picks one running sandbox belonging to app to exec into. It
// also returns how many sandboxes it was choosing between, so the caller can
// stay quiet when there was never a decision to report.
//
// service, when set, is a hard filter; when empty the choice falls back to a
// preference for the web service.
func selectAppSandbox(ctx *Context, appName, service string) (sandboxCandidate, int, error) {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return sandboxCandidate{}, 0, err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)

	appEnt, err := app.NewClient(ctx.Log, client).GetByName(ctx, appName)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return sandboxCandidate{}, 0, unknownAppError(appName, err, ctx.Verbose())
		}
		return sandboxCandidate{}, 0, err
	}

	// A sandbox reaches its app through its pool: the pool carries the indexed
	// app ref, and every pool-created sandbox is labeled with its pool's id.
	// Going this way rather than through sandbox.spec.version also excludes the
	// ephemeral sandboxes `miren app run` creates, which have no pool — those
	// belong to whoever is sitting in them.
	pools, err := ec.List(ctx, entity.Ref(compute_v1alpha.SandboxPoolAppId, appEnt.ID))
	if err != nil {
		return sandboxCandidate{}, 0, fmt.Errorf("failed to list sandbox pools for %q: %w", appName, err)
	}

	poolService := make(map[string]string)
	var known []string
	for pools.Next() {
		var pool compute_v1alpha.SandboxPool
		if err := pools.Read(&pool); err != nil {
			continue
		}

		if !slices.Contains(known, pool.Service) {
			known = append(known, pool.Service)
		}

		if service != "" && pool.Service != service {
			continue
		}

		poolService[pool.ID.String()] = pool.Service
	}

	if service != "" && len(poolService) == 0 {
		return sandboxCandidate{}, 0, unknownServiceError(appName, service, known)
	}

	sandboxes, err := ec.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return sandboxCandidate{}, 0, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	var candidates []sandboxCandidate
	for sandboxes.Next() {
		var sb compute_v1alpha.Sandbox
		if err := sandboxes.Read(&sb); err != nil {
			continue
		}

		md := sandboxes.Metadata()
		if md == nil {
			continue
		}

		pool, _ := md.Labels.Get("pool")
		svc, ours := poolService[pool]
		if !ours {
			continue
		}

		if sb.Status != compute_v1alpha.RUNNING {
			continue
		}

		// Exec is forwarded to whichever node the sandbox landed on, so one
		// that hasn't been placed yet has nowhere to forward to.
		var sch compute_v1alpha.Schedule
		sch.Decode(sandboxes.Entity())
		if entity.Empty(sch.Key.Node) {
			continue
		}

		candidates = append(candidates, sandboxCandidate{
			ID:      sb.ID,
			Brief:   ui.BriefId(sandboxes.Entity()),
			Service: svc,
			Version: sb.Spec.Version,
		})
	}

	choice := narrowSandboxCandidates(
		candidates, appEnt.ActiveVersion, service, slices.Contains(known, defaultExecService))

	switch {
	case choice.MissingDefault:
		return sandboxCandidate{}, 0, noDefaultServiceError(appName, choice.Running)
	case len(choice.Candidates) == 0:
		return sandboxCandidate{}, 0, noRunningSandboxError(appName, service)
	}

	return choice.Candidates[rand.IntN(len(choice.Candidates))], len(choice.Candidates), nil
}

// sandboxChoice is what narrowing produced: either a set to pick from, or the
// reason there isn't one.
type sandboxChoice struct {
	Candidates []sandboxCandidate

	// MissingDefault reports that the app has a web service but none of its
	// running sandboxes are web. That is not the same as having nothing to
	// offer, and it must not be answered by handing over a different service.
	MissingDefault bool
	// Running lists the services that do have something running, for telling
	// the caller what they could ask for instead.
	Running []string
}

// narrowSandboxCandidates decides which of the running sandboxes are the ones
// to land in.
//
// Service is a requirement and version is a preference, in that order, and the
// order is the whole design. Which service you get is the difference between a
// web process and a background worker, so substituting one for the other is
// never a kindness. Which version you get is the difference between newer and
// older code for the same job, where insisting is what hurts: a failed deploy
// leaves ActiveVersion pointing at a version that never came up while the old
// instances keep serving traffic, and refusing those would lock you out of a
// shell exactly when a deploy has just broken.
//
// So a rolling deploy that has taken web down to the old version still gives
// you old web rather than new worker, and an app whose web service has nothing
// running at all gets told so instead of quietly handed a worker.
//
// The result is sorted so that the only nondeterminism left is the caller's
// random pick.
func narrowSandboxCandidates(
	candidates []sandboxCandidate,
	activeVersion entity.Id,
	service string,
	appHasDefaultService bool,
) sandboxChoice {
	candidates = slices.Clone(candidates)

	// An explicit --service was already applied as a hard filter against the
	// pools, so this is only the default for a caller who didn't say. An app
	// with no web service at all is a different case: there's no substitution
	// to worry about, so every service stays in play.
	if service == "" && appHasDefaultService {
		web := keepSandboxes(candidates, func(c sandboxCandidate) bool {
			return c.Service == defaultExecService
		})

		if len(web) == 0 {
			return sandboxChoice{MissingDefault: true, Running: servicesOf(candidates)}
		}

		candidates = web
	}

	if !entity.Empty(activeVersion) {
		candidates = preferSandboxes(candidates, func(c sandboxCandidate) bool {
			return c.Version == activeVersion
		})
	}

	slices.SortFunc(candidates, func(a, b sandboxCandidate) int {
		return cmp.Compare(a.ID, b.ID)
	})

	return sandboxChoice{Candidates: candidates}
}

// servicesOf lists the distinct services present, sorted.
func servicesOf(candidates []sandboxCandidate) []string {
	var services []string
	for _, c := range candidates {
		if c.Service != "" && !slices.Contains(services, c.Service) {
			services = append(services, c.Service)
		}
	}

	slices.Sort(services)

	return services
}

// keepSandboxes is a plain filter: what doesn't match is dropped, even if that
// leaves nothing.
func keepSandboxes(candidates []sandboxCandidate, want func(sandboxCandidate) bool) []sandboxCandidate {
	kept := make([]sandboxCandidate, 0, len(candidates))
	for _, c := range candidates {
		if want(c) {
			kept = append(kept, c)
		}
	}

	return kept
}

// preferSandboxes keeps the candidates matching want, unless that would keep
// none, in which case the field is left as it was.
func preferSandboxes(candidates []sandboxCandidate, want func(sandboxCandidate) bool) []sandboxCandidate {
	preferred := make([]sandboxCandidate, 0, len(candidates))
	for _, c := range candidates {
		if want(c) {
			preferred = append(preferred, c)
		}
	}

	if len(preferred) == 0 {
		return candidates
	}

	return preferred
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
