package addon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
)

// WaitCeiling leaves room above providers' five-minute readiness timeouts for
// their failure to surface, while staying below the thirty-minute deploy lock.
const WaitCeiling = 10 * time.Minute

// CloneCopyTimeout applies to both physical backups and logical dump/restore.
const CloneCopyTimeout = 30 * time.Minute

// Preview deploys do not hold the normal deployment lock. Allow a full copy,
// pool/service readiness, and exec overhead rather than the primary's budget.
const CloneWaitCeiling = CloneCopyTimeout + 3*WaitCeiling

// Teardown may wait for an in-flight clone and then resume its unfinished saga
// before undoing it. Both passes need room for copying and rollback.
const CloneCleanupWaitCeiling = 2 * CloneWaitCeiling

// ExpectedAddon is one addon the deploy declared and therefore requires to
// be active before the version can serve.
type ExpectedAddon struct {
	Name    string
	Variant string
}

type waitStatus interface {
	SendMessage(string)
	SendError(string, ...any)
}

// WaitForAssociations blocks until each expected addon has an active association
// on the app (or exactly versionID for a preview). With nothing expected it
// returns at once without touching the store. Status may be nil.
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
func WaitForAssociations(
	ctx context.Context,
	eac *entityserver_v1alpha.EntityAccessClient,
	log *slog.Logger,
	appName string,
	appID entity.Id,
	versionID entity.Id,
	expected []ExpectedAddon,
	ceiling time.Duration,
	status waitStatus,
) error {
	if len(expected) == 0 {
		return nil
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	index := entity.Ref(addon_v1alpha.AddonAssociationAppId, appID)
	if versionID != "" {
		index = entity.Ref(addon_v1alpha.AddonAssociationAppVersionId, versionID)
	}
	w := indexwatch.New(eac, index, indexwatch.Options{
		Logger:     log.With("component", "addon-wait", "app", appName),
		BufferSize: 16,
	})
	if err := w.Start(watchCtx); err != nil {
		return fmt.Errorf("watching addon associations for app %q: %w", appName, err)
	}
	defer w.Stop()

	deadline := time.NewTimer(ceiling)
	defer deadline.Stop()

	state := newAddonWaitState(appName, versionID, expected, status)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-deadline.C:
			return fmt.Errorf("addons did not become ready within %s: %s",
				ceiling, strings.Join(state.waiting(), ", "))

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
	appName   string
	versionID entity.Id
	status    waitStatus
	expected  []ExpectedAddon

	// current is the latest association seen per expected addon name. An
	// expected addon absent from this map has no visible record yet.
	current map[string]*addon_v1alpha.AddonAssociation

	// announced tracks which addons have had a "provisioning" line sent, so
	// each one gets that line once and a "ready" line once.
	announced map[string]bool
	// ready tracks which addons have had their "ready" line sent.
	ready map[string]bool
}

func newAddonWaitState(appName string, versionID entity.Id, expected []ExpectedAddon, status waitStatus) *addonWaitState {
	return &addonWaitState{
		appName:   appName,
		versionID: versionID,
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
	if assoc.AppVersion != s.versionID {
		return
	}
	name := NameFromRef(assoc.Addon)
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
	if s.status == nil {
		return
	}
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
			if s.versionID != "" {
				err := fmt.Errorf("addon %q could not be cloned for this preview: %s", e.Name, assoc.ErrorMessage)
				if s.status != nil {
					s.status.SendError("%s", err)
				}
				return err
			}
			const format = "addon %q failed to provision: %s. Run 'miren addon destroy %s -a %s' and redeploy"
			if s.status != nil {
				s.status.SendError(format, e.Name, assoc.ErrorMessage, e.Name, s.appName)
			}
			return fmt.Errorf(format, e.Name, assoc.ErrorMessage, e.Name, s.appName)
		case "deprovisioning":
			if s.versionID != "" {
				err := fmt.Errorf("addon %q clone is being removed from this preview; redeploy the preview once cleanup finishes", e.Name)
				if s.status != nil {
					s.status.SendError("%s", err)
				}
				return err
			}
			const format = "addon %q is being removed from app %q; redeploy once it is gone"
			if s.status != nil {
				s.status.SendError(format, e.Name, s.appName)
			}
			return fmt.Errorf(format, e.Name, s.appName)
		}
	}
	return nil
}
