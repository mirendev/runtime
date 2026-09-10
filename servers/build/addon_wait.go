package build

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
)

// addonWaitCeiling bounds the wait as a whole. It sits above the providers'
// own pool-ready timeouts (5 minutes) so a provisioning failure surfaces as
// the association's error, with its message, before this deadline turns it
// into a bare timeout. It must stay well under the deploy lock TTL (30
// minutes). A variable rather than a constant so tests can shrink it.
var addonWaitCeiling = 10 * time.Minute

// expectedAddon is one addon the deploy declared and therefore requires to
// be active before the version can serve.
type expectedAddon struct {
	Name    string
	Variant string
}

// expectedAddons lists the addons app.toml declares, sorted by name so the
// deploy's messages come out in a stable order. The key in app.toml is the
// addon name, optionally with a ":variant" suffix that CreateInstance
// splits the same way.
func expectedAddons(ac *appconfig.AppConfig) []expectedAddon {
	if ac == nil {
		return nil
	}
	out := make([]expectedAddon, 0, len(ac.Addons))
	for key, cfg := range ac.Addons {
		name, suffix, _ := strings.Cut(key, ":")
		variant := suffix
		if cfg != nil && cfg.Variant != "" {
			variant = cfg.Variant
		}
		out = append(out, expectedAddon{Name: name, Variant: variant})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// awaitAddons blocks until each expected addon has an active association on
// the app. With nothing expected it returns at once without touching the
// store.
//
// Creating an association only records the request; the addon controller
// provisions the backing service on its own clock and flips the association
// to active once the database is listening and reachable. Nothing downstream
// of the deploy can use the addon before that: ResolveRuntimeConfig injects
// variables only from active associations, so a deploy task run earlier
// would have no DATABASE_URL, and a version activated earlier would sit at
// 0/0 instances until the launcher's gate released it, long after the CLI
// gave up waiting for health.
//
// The wait is driven by what the deploy declared, not by whatever records
// happen to exist. An expected addon whose association is not visible yet is
// simply not active yet, so a slow index never lets the deploy through
// early. It watches the app's association index rather than polling it, so
// the deploy moves on the moment the controller flips the status.
//
// An expected addon in error fails the deploy: error is terminal in the
// controller and nothing in a redeploy heals it, so the user has to act, and
// the message says how. One being deprovisioned fails too: it was destroyed
// out from under this deploy and will not come back on its own.
func (b *Builder) awaitAddons(
	ctx context.Context,
	appName string,
	appID entity.Id,
	expected []expectedAddon,
	status StatusSender,
) error {
	if len(expected) == 0 {
		return nil
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	w := indexwatch.New(b.EAS, entity.Ref(addon_v1alpha.AddonAssociationAppId, appID), indexwatch.Options{
		Logger:     b.Log.With("component", "addon-wait", "app", appName),
		BufferSize: 16,
	})
	if err := w.Start(watchCtx); err != nil {
		return fmt.Errorf("watching addon associations for app %q: %w", appName, err)
	}
	defer w.Stop()

	deadline := time.NewTimer(addonWaitCeiling)
	defer deadline.Stop()

	state := newAddonWaitState(appName, expected, status)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-deadline.C:
			return fmt.Errorf("addons did not become ready within %s: %s",
				addonWaitCeiling, strings.Join(state.waiting(), ", "))

		case ev, ok := <-w.Updates():
			if !ok {
				// The watcher only closes Updates when its context ends,
				// which is our context.
				if err := ctx.Err(); err != nil {
					return err
				}
				return fmt.Errorf("addon association watch for app %q ended unexpectedly", appName)
			}

			state.apply(ev)

			if err := state.failed(); err != nil {
				return err
			}
			if len(state.waiting()) == 0 {
				return nil
			}
		}
	}
}

// addonWaitState is the deploy's view of the expected addons, kept current
// from watch events. Associations for addons the deploy did not declare are
// ignored.
type addonWaitState struct {
	appName  string
	status   StatusSender
	expected []expectedAddon

	// current is the latest association seen per expected addon name. An
	// expected addon absent from this map has no visible record yet.
	current map[string]*addon_v1alpha.AddonAssociation

	// announced tracks which addons have had a "provisioning" line sent, so
	// each one gets that line once and a "ready" line once.
	announced map[string]bool
	// ready tracks which addons have had their "ready" line sent.
	ready map[string]bool
}

func newAddonWaitState(appName string, expected []expectedAddon, status StatusSender) *addonWaitState {
	return &addonWaitState{
		appName:   appName,
		status:    status,
		expected:  expected,
		current:   make(map[string]*addon_v1alpha.AddonAssociation, len(expected)),
		announced: make(map[string]bool, len(expected)),
		ready:     make(map[string]bool, len(expected)),
	}
}

// apply folds one watch event into the state and then emits progress for
// any expected addon whose situation changed.
func (s *addonWaitState) apply(ev indexwatch.Event) {
	switch ev.Type {
	case indexwatch.EventSync:
		// A snapshot is the complete current set: anything not in it is gone.
		s.current = make(map[string]*addon_v1alpha.AddonAssociation, len(s.expected))
		for _, en := range ev.Entities {
			s.observe(en)
		}
	case indexwatch.EventAdded, indexwatch.EventUpdated:
		s.observe(ev.Entity)
	case indexwatch.EventDeleted:
		for name, assoc := range s.current {
			if assoc.ID == ev.Id {
				delete(s.current, name)
			}
		}
	}
	s.announce()
}

func (s *addonWaitState) observe(en *entity.Entity) {
	if en == nil {
		return
	}
	var assoc addon_v1alpha.AddonAssociation
	assoc.Decode(en)
	name := addon.NameFromRef(assoc.Addon)
	if !s.expects(name) {
		return
	}
	s.current[name] = &assoc
}

func (s *addonWaitState) expects(name string) bool {
	for _, e := range s.expected {
		if e.Name == name {
			return true
		}
	}
	return false
}

// announce sends one "provisioning" line per expected addon the first time
// it is seen not yet active, and one "ready" line when it gets there. An
// addon that was active from the first snapshot is not news and gets neither.
func (s *addonWaitState) announce() {
	for _, e := range s.expected {
		assoc := s.current[e.Name]
		active := assoc != nil && assoc.Status == "active"

		switch {
		case !active && !s.announced[e.Name]:
			s.announced[e.Name] = true
			variant := e.Variant
			if assoc != nil && assoc.Variant != "" {
				variant = assoc.Variant
			}
			if variant != "" {
				s.status.SendMessage(fmt.Sprintf("Provisioning addon %s (%s)...", e.Name, variant))
			} else {
				s.status.SendMessage(fmt.Sprintf("Provisioning addon %s...", e.Name))
			}
		case active && s.announced[e.Name] && !s.ready[e.Name]:
			s.ready[e.Name] = true
			s.status.SendMessage(fmt.Sprintf("Addon %s ready", e.Name))
		}
	}
}

// waiting returns the names of expected addons that do not yet have an
// active association, in declaration order.
func (s *addonWaitState) waiting() []string {
	var names []string
	for _, e := range s.expected {
		assoc := s.current[e.Name]
		if assoc == nil || assoc.Status != "active" {
			names = append(names, e.Name)
		}
	}
	return names
}

// failed returns the deploy-ending error for the first expected addon that
// can no longer become active, or nil.
func (s *addonWaitState) failed() error {
	for _, e := range s.expected {
		assoc := s.current[e.Name]
		if assoc == nil {
			continue
		}
		switch assoc.Status {
		case "error":
			const format = "addon %q failed to provision: %s. Run 'miren addon destroy %s -a %s' and redeploy"
			s.status.SendError(format, e.Name, assoc.ErrorMessage, e.Name, s.appName)
			return fmt.Errorf(format, e.Name, assoc.ErrorMessage, e.Name, s.appName)
		case "deprovisioning":
			const format = "addon %q is being removed from app %q; redeploy once it is gone"
			s.status.SendError(format, e.Name, s.appName)
			return fmt.Errorf(format, e.Name, s.appName)
		}
	}
	return nil
}
