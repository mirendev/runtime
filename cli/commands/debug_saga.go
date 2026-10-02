package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	saga_v1alpha "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/ui"
)

// sagaOutputPreview is how much of an action's output we show before
// truncating. Wide enough to recognize what came back, short enough that a
// twenty-action saga still fits on a screen. Use --full to see all of it.
const sagaOutputPreview = 100

// sagaIDNamespace is the kind prefix execution IDs carry. Listings print IDs
// without it, the way sandbox and app IDs are shown, and lookups put it back.
const sagaIDNamespace = "saga/"

// activeSagaStatuses are the non-terminal statuses. These are what `debug saga
// list` shows by default: a completed saga is history, but a saga sitting in
// one of these is either working or wedged, which is the thing you opened a
// debug command to find out.
var activeSagaStatuses = []saga.Status{
	saga.StatusPending,
	saga.StatusRunning,
	saga.StatusUndoing,
}

// sagaRecord pairs a decoded execution with the entity store's own timestamps.
// The saga schema does not persist Execution.CreatedAt/UpdatedAt, but the
// entity store records when it created and last wrote each entity, and for a
// saga those are the same events: it is written once per action. A running
// saga whose entity has not been updated in an hour is the stuck one.
type sagaRecord struct {
	exec      *saga.Execution
	createdAt time.Time
	updatedAt time.Time
}

// lastAction returns the most recently executed action name, or "" if the saga
// has not run anything yet.
func (r *sagaRecord) lastAction() string {
	if len(r.exec.ExecutionOrder) == 0 {
		return ""
	}
	return r.exec.ExecutionOrder[len(r.exec.ExecutionOrder)-1]
}

func DebugSagaList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
	Status     string `short:"s" long:"status" description:"Filter by status (pending, running, undoing, completed, failed)"`
	Definition string `short:"d" long:"definition" description:"Filter by saga definition name"`
	All        bool   `short:"A" long:"all" description:"Include completed and failed sagas"`
}) error {
	var wantStatus saga.Status
	if opts.Status != "" {
		var err error
		wantStatus, err = parseSagaStatus(opts.Status)
		if err != nil {
			return err
		}
	}

	eac, err := sagaClient(ctx)
	if err != nil {
		return err
	}

	// Pick the narrowest index the server can answer directly. definition_name
	// and status are both indexed; when both filters are given we query by
	// definition and narrow by status below, since a definition name is far
	// more selective than a status.
	var indexes []entity.Attr
	switch {
	case opts.Definition != "":
		indexes = append(indexes, entity.String(saga_v1alpha.SagaDefinitionNameId, opts.Definition))
	case wantStatus != "":
		attr, err := sagaStatusIndex(wantStatus)
		if err != nil {
			return err
		}
		indexes = append(indexes, attr)
	case opts.All:
		res, err := eac.LookupKind(ctx, "saga")
		if err != nil {
			return fmt.Errorf("looking up saga kind: %w", err)
		}
		indexes = append(indexes, res.Attr())
	default:
		for _, s := range activeSagaStatuses {
			attr, err := sagaStatusIndex(s)
			if err != nil {
				return err
			}
			indexes = append(indexes, attr)
		}
	}

	records, err := listSagas(ctx, eac, indexes)
	if err != nil {
		return err
	}

	// Filtering to active statuses applies when the user asked for neither a
	// specific status nor --all. Note that --definition does not opt out: a
	// definition whose sagas have all completed still lists empty by default.
	// The empty-state message below keys off this same variable rather than
	// restating the condition, since the two drifting apart is what silently
	// suppressed the --all hint in exactly the case that needed it.
	filterActive := !opts.All && wantStatus == ""

	// Apply whatever the index could not.
	records = slices.DeleteFunc(records, func(r *sagaRecord) bool {
		switch {
		case wantStatus != "":
			return r.exec.Status != wantStatus
		case filterActive:
			return !slices.Contains(activeSagaStatuses, r.exec.Status)
		default:
			return false
		}
	})

	// Most recent activity first.
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].updatedAt.After(records[j].updatedAt)
	})

	if opts.IsJSON() {
		items := make([]sagaListJSON, 0, len(records))
		for _, r := range records {
			items = append(items, newSagaListJSON(r))
		}
		return PrintJSON(items)
	}

	if len(records) == 0 {
		if filterActive {
			ctx.Printf("No active sagas found (use --all to include completed and failed)\n")
		} else {
			ctx.Printf("No sagas found\n")
		}
		return nil
	}

	headers := []string{"ID", "DEFINITION", "STATUS", "ACTIONS", "LAST ACTION", "CREATED", "UPDATED"}
	rows := make([]ui.Row, 0, len(records))

	for _, r := range records {
		lastAction := r.lastAction()
		if lastAction == "" {
			lastAction = "-"
		}

		rows = append(rows, ui.Row{
			ui.CleanEntityID(r.exec.ID),
			r.exec.DefinitionName,
			sagaStatusLabel(r.exec),
			fmt.Sprintf("%d", len(r.exec.ExecutionOrder)),
			lastAction,
			humanFriendlyTimestamp(r.createdAt),
			humanFriendlyTimestamp(r.updatedAt),
		})
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0, 2))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	if len(records) == 1 {
		ctx.Info("Total: 1 saga")
	} else {
		ctx.Info("Total: %d sagas", len(records))
	}

	return nil
}

func DebugSagaShow(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
	Id   string `short:"i" long:"id" description:"Saga execution ID"`
	Full bool   `long:"full" description:"Print complete action outputs instead of truncating them (JSON always includes them)"`

	Args []string `rest:"true"`
}) error {
	id := opts.Id
	if id == "" {
		if len(opts.Args) == 0 {
			return fmt.Errorf("saga execution ID is required (pass as first positional arg or via --id)")
		}
		id = opts.Args[0]
	}

	eac, err := sagaClient(ctx)
	if err != nil {
		return err
	}

	res, err := getSaga(ctx, eac, id)
	if err != nil {
		return err
	}

	record, err := decodeSagaRecord(res)
	if err != nil {
		return err
	}

	// Nested sagas record their parent, and that attribute is indexed, so the
	// children of this execution are one query away. The ref value is hoisted
	// into its own variable because the guardrail in guardrails/refenum_test.go
	// flags entity.Id(…) sitting directly in an entity.Ref, which is how the
	// short-enum-value bug in MIR-1288 looks. This is a real execution ID, not
	// an enum value.
	parentID := entity.Id(record.exec.ID)
	children, err := listSagas(ctx, eac, []entity.Attr{
		entity.Ref(saga_v1alpha.SagaParentExecutionIdId, parentID),
	})
	if err != nil {
		return fmt.Errorf("listing child sagas: %w", err)
	}
	sort.SliceStable(children, func(i, j int) bool {
		return children[i].createdAt.Before(children[j].createdAt)
	})

	if opts.IsJSON() {
		return PrintJSON(newSagaShowJSON(record, children))
	}

	printSagaShow(ctx, record, children, opts.Full)
	return nil
}

// DebugSagaAbandon gives up a saga execution the server refused to resume.
// It is deliberately narrow: only a blocked execution qualifies, because
// anything else is still going to be driven or compensated on its own.
func DebugSagaAbandon(ctx *Context, opts struct {
	ConfigCentric
	Id    string `short:"i" long:"id" description:"Saga execution ID"`
	Force bool   `short:"f" long:"force" description:"Skip confirmation prompt"`

	Args []string `rest:"true"`
}) error {
	id := opts.Id
	if id == "" {
		if len(opts.Args) == 0 {
			return fmt.Errorf("saga execution ID is required (pass as first positional arg or via --id)")
		}
		id = opts.Args[0]
	}

	eac, err := sagaClient(ctx)
	if err != nil {
		return err
	}

	res, err := getSaga(ctx, eac, id)
	if err != nil {
		return err
	}

	record, err := decodeSagaRecord(res)
	if err != nil {
		return err
	}
	exec := record.exec
	blockedBecause := exec.BlockedReason

	// Same hoisting as show, for the same guardrail.
	parentID := entity.Id(exec.ID)
	children, err := listSagas(ctx, eac, []entity.Attr{
		entity.Ref(saga_v1alpha.SagaParentExecutionIdId, parentID),
	})
	if err != nil {
		return fmt.Errorf("listing child sagas: %w", err)
	}
	if err := sagaAbandonBlockers(children); err != nil {
		return err
	}

	left, err := saga.Abandon(exec, time.Now())
	if err != nil {
		if errors.Is(err, saga.ErrNotBlocked) {
			return fmt.Errorf("%w\n\nAbandon is only for executions the server refused to resume. "+
				"This one will be finished or compensated by the server on its own", err)
		}
		return err
	}

	ctx.Printf("Saga %s (%s) is blocked:\n  %s\n\n", ui.CleanEntityID(exec.ID), exec.DefinitionName, blockedBecause)
	if len(left) == 0 {
		ctx.Printf("It has no completed work left to compensate.\n\n")
	} else {
		ctx.Printf("Abandoning it marks it failed WITHOUT undoing these actions, which may leave their work behind:\n")
		for _, name := range left {
			ctx.Printf("  - %s\n", name)
		}
		ctx.Printf("\nIf a release that can still resume this execution is available, running it instead lets the saga finish or compensate normally.\n\n")
	}

	if !opts.Force {
		confirmed, err := ui.Confirm(
			ui.WithMessage("Abandon this saga execution?"),
			ui.WithDefault(false),
		)
		if err != nil {
			return err
		}
		if !confirmed {
			ctx.Printf("Aborted\n")
			return nil
		}
	}

	// Conditional on the revision we read, so a server that resumed the
	// execution while we were asking does not have its progress overwritten.
	storage := saga.NewEACStorage(eac, nil)
	if err := storage.SaveAtRevision(ctx, exec, res.Revision()); err != nil {
		return fmt.Errorf("abandoning saga %s (it may have changed since it was read; check `miren debug saga show`): %w",
			ui.CleanEntityID(exec.ID), err)
	}

	ctx.Printf("Abandoned saga %s.\n", ui.CleanEntityID(exec.ID))
	return nil
}

// sagaAbandonBlockers refuses to abandon a parent while any of its children
// is still in flight. A parent blocked by a nested refusal never recorded the
// action that ran the child, so its own leftover list would miss everything
// the child built, and the child would sit blocked with nothing left to ever
// resolve it. Abandoning the child first shows what it leaves behind, and its
// parent then unwinds on its own the next time it is driven.
func sagaAbandonBlockers(children []*sagaRecord) error {
	var inFlight []string
	for _, c := range children {
		// A completed child still holds its parent when it is blocked: its
		// undo was refused, so its work is uncompensated.
		finished := c.exec.Status == saga.StatusFailed ||
			(c.exec.Status == saga.StatusCompleted && c.exec.BlockedReason == "")
		if finished {
			continue
		}
		inFlight = append(inFlight, fmt.Sprintf("%s (%s)", ui.CleanEntityID(c.exec.ID), sagaStatusLabel(c.exec)))
	}
	if len(inFlight) == 0 {
		return nil
	}
	return fmt.Errorf("this saga has child sagas still in flight or blocked: %s\n\n"+
		"Abandon a blocked child first, which lists the work it leaves behind; "+
		"this saga then unwinds on its own the next time it is driven",
		strings.Join(inFlight, ", "))
}

func printSagaShow(ctx *Context, r *sagaRecord, children []*sagaRecord, full bool) {
	exec := r.exec

	ctx.Printf("ID:         %s\n", ui.CleanEntityID(exec.ID))
	ctx.Printf("Definition: %s (v%d)\n", exec.DefinitionName, exec.DefinitionVersion)
	ctx.Printf("Status:     %s\n", sagaStatusLabel(exec))
	if exec.RecoveryScope != "" {
		ctx.Printf("Scope:      %s\n", exec.RecoveryScope)
	}
	if exec.ParentExecutionID != "" {
		ctx.Printf("Parent:     %s\n", ui.CleanEntityID(exec.ParentExecutionID))
	}
	ctx.Printf("Created:    %s\n", sagaTimestamp(r.createdAt))
	ctx.Printf("Updated:    %s\n", sagaTimestamp(r.updatedAt))
	if exec.Error != "" {
		ctx.Printf("Error:      %s\n", exec.Error)
	}
	if exec.BlockedReason != "" {
		// Last in the header and set apart, because it changes what every
		// other line means: nothing is driving this execution, and nothing
		// will until someone acts.
		ctx.Printf("\nBLOCKED: the server refused to resume this execution.\n")
		ctx.Printf("  %s\n", exec.BlockedReason)
		if exec.BlockedOn != "" {
			ctx.Printf("  Waiting on child saga %s; nothing here runs until it can be resumed or is abandoned.\n",
				ui.CleanEntityID(exec.BlockedOn))
		}
	}

	if len(exec.InitialInputs) > 0 {
		ctx.Printf("\nInitial inputs:\n")
		for _, key := range slices.Sorted(maps.Keys(exec.InitialInputs)) {
			ctx.Printf("  %s: %s\n", key, formatSagaValue(exec.InitialInputs[key], full, 2))
		}
	}

	order := sagaActionOrder(exec)
	if len(order) == 0 {
		ctx.Printf("\nActions: none executed yet\n")
	} else {
		ctx.Printf("\nActions (%d executed):\n", len(order))
		for i, name := range order {
			result := exec.ExecutedActions[name]
			ctx.Printf("  %d. %-30s %s\n", i+1, name, sagaActionSummary(result))
			if result == nil {
				continue
			}
			if result.Error != "" {
				ctx.Printf("     error: %s\n", result.Error)
			}
			if len(result.Output) > 0 {
				ctx.Printf("     output: %s\n", formatSagaJSON(result.Output, full, 5))
			}
		}
	}

	if len(children) > 0 {
		ctx.Printf("\nChild sagas (%d):\n", len(children))
		for _, c := range children {
			ctx.Printf("  %s  %s  %s\n", ui.CleanEntityID(c.exec.ID), c.exec.DefinitionName, sagaStatusLabel(c.exec))
		}
	}
}

// sagaStatusLabel is the status as a person should read it. A blocked
// execution keeps its recorded status, so the status alone would make a saga
// nothing will ever drive look like one that is in progress.
func sagaStatusLabel(exec *saga.Execution) string {
	if exec.BlockedReason != "" {
		return string(exec.Status) + " (blocked)"
	}
	return string(exec.Status)
}

// sagaActionOrder returns the action names to display, in execution order. The
// executor appends to ExecutionOrder every time it records a result, so the two
// should agree, but this is a debug command: if a record somehow holds a result
// that never made it into the order, show it rather than hide it.
func sagaActionOrder(exec *saga.Execution) []string {
	order := slices.Clone(exec.ExecutionOrder)
	for _, name := range slices.Sorted(maps.Keys(exec.ExecutedActions)) {
		if !slices.Contains(order, name) {
			order = append(order, name)
		}
	}
	return order
}

// sagaActionSummary describes what happened to a single action in one line.
func sagaActionSummary(result *saga.ActionResult) string {
	if result == nil {
		return "no result recorded"
	}

	var summary string
	switch {
	case result.Error != "":
		summary = fmt.Sprintf("failed %s", humanFriendlyTimestamp(result.ExecutedAt))
	default:
		summary = fmt.Sprintf("executed %s", humanFriendlyTimestamp(result.ExecutedAt))
	}

	if result.UndoneAt != nil {
		summary += fmt.Sprintf(", undone %s", humanFriendlyTimestamp(*result.UndoneAt))
	}

	return summary
}

func sagaTimestamp(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return "-"
	}
	return fmt.Sprintf("%s (%s)", t.Format(time.RFC3339), humanFriendlyTimestamp(t))
}

// formatSagaJSON renders raw JSON for display: compacted onto one line and
// truncated by default, pretty-printed and indented when full is set.
func formatSagaJSON(raw []byte, full bool, indent int) string {
	if len(raw) == 0 {
		return "-"
	}

	if full {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, strings.Repeat(" ", indent), "  "); err != nil {
			return string(raw)
		}
		return pretty.String()
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return truncateSagaValue(string(raw))
	}
	return truncateSagaValue(compact.String())
}

// formatSagaValue renders an already-decoded value (an initial input) the same
// way formatSagaJSON renders raw action output.
func formatSagaValue(v any, full bool, indent int) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return formatSagaJSON(raw, full, indent)
}

func truncateSagaValue(s string) string {
	if len(s) <= sagaOutputPreview {
		return s
	}
	return s[:sagaOutputPreview] + "… (--full for the rest)"
}

// parseSagaStatus maps a user-supplied status name onto a saga.Status.
func parseSagaStatus(s string) (saga.Status, error) {
	known := []saga.Status{
		saga.StatusPending,
		saga.StatusRunning,
		saga.StatusUndoing,
		saga.StatusCompleted,
		saga.StatusFailed,
	}

	normalized := strings.ToLower(strings.TrimSpace(s))
	for _, k := range known {
		if string(k) == normalized {
			return k, nil
		}
	}

	names := make([]string, len(known))
	for i, k := range known {
		names[i] = string(k)
	}
	return "", fmt.Errorf("unknown saga status %q (expected one of: %s)", s, strings.Join(names, ", "))
}

// sagaStatusIndex builds the index attribute for querying sagas by status.
func sagaStatusIndex(s saga.Status) (entity.Attr, error) {
	attr, ok := saga.StatusIndexAttr(s)
	if !ok {
		return attr, fmt.Errorf("no index for saga status %q", s)
	}
	return attr, nil
}

// sagaIDCandidates expands what the user typed into the IDs worth trying, in
// order. Execution IDs are kind-namespaced (saga/sg-…) but listings print them
// without the namespace, so the bare name a user copies off the table has to
// resolve too. The unqualified form is also what sagas minted before IDs grew
// their namespace look like, so it stays a valid thing to type.
func sagaIDCandidates(id string) []string {
	if strings.Contains(id, "/") {
		return []string{id}
	}
	return []string{sagaIDNamespace + id, id}
}

// getSaga resolves an execution by any of the forms its ID can take.
func getSaga(ctx *Context, eac *entityserver_v1alpha.EntityAccessClient, id string) (*entityserver_v1alpha.Entity, error) {
	for _, candidate := range sagaIDCandidates(id) {
		res, err := eac.Get(ctx, candidate)
		if err == nil {
			return res.Entity(), nil
		}
		if !errors.Is(err, cond.ErrNotFound{}) {
			return nil, fmt.Errorf("getting saga %s: %w", candidate, err)
		}
	}

	return nil, fmt.Errorf("saga %s not found", id)
}

func sagaClient(ctx *Context) (*entityserver_v1alpha.EntityAccessClient, error) {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return nil, err
	}
	return entityserver_v1alpha.NewEntityAccessClient(client), nil
}

// listSagas queries each index and returns the decoded executions, deduplicated
// by ID. A saga can appear under more than one index at once (a stale pending
// entry lingering after the transition to running), so the caller would
// otherwise see the same execution twice.
func listSagas(ctx *Context, eac *entityserver_v1alpha.EntityAccessClient, indexes []entity.Attr) ([]*sagaRecord, error) {
	var records []*sagaRecord
	seen := make(map[string]struct{})

	for _, index := range indexes {
		res, err := eac.List(ctx, index)
		if err != nil {
			return nil, fmt.Errorf("listing sagas: %w", err)
		}

		for _, e := range res.Values() {
			if _, dup := seen[e.Id()]; dup {
				continue
			}
			seen[e.Id()] = struct{}{}

			record, err := decodeSagaRecord(e)
			if err != nil {
				// One unreadable record should not blank the whole listing.
				ctx.Warn("skipping saga %s: %s", e.Id(), err)
				continue
			}
			records = append(records, record)
		}
	}

	return records, nil
}

func decodeSagaRecord(e *entityserver_v1alpha.Entity) (*sagaRecord, error) {
	ent := e.Entity()

	sagaEntity, ok := entity.As[saga_v1alpha.Saga](ent)
	if !ok {
		return nil, fmt.Errorf("entity %s is not a saga", e.Id())
	}

	exec, err := saga.ExecutionFromEntity(sagaEntity)
	if err != nil {
		return nil, fmt.Errorf("decoding saga %s: %w", e.Id(), err)
	}

	return &sagaRecord{
		exec:      exec,
		createdAt: time.UnixMilli(e.CreatedAt()),
		updatedAt: time.UnixMilli(e.UpdatedAt()),
	}, nil
}

type sagaListJSON struct {
	ID                string `json:"id"`
	DefinitionName    string `json:"definition_name"`
	DefinitionVersion int    `json:"definition_version"`
	Status            string `json:"status"`
	ActionCount       int    `json:"action_count"`
	LastAction        string `json:"last_action,omitempty"`
	ParentExecutionID string `json:"parent_execution_id,omitempty"`
	RecoveryScope     string `json:"recovery_scope,omitempty"`
	Error             string `json:"error,omitempty"`
	BlockedReason     string `json:"blocked_reason,omitempty"`
	BlockedOn         string `json:"blocked_on,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// IDs stay fully qualified in JSON. Table output drops the namespace because a
// human is reading it, but a machine-readable ID should be the one the entity
// store actually holds.
func newSagaListJSON(r *sagaRecord) sagaListJSON {
	return sagaListJSON{
		ID:                r.exec.ID,
		DefinitionName:    r.exec.DefinitionName,
		DefinitionVersion: r.exec.DefinitionVersion,
		Status:            string(r.exec.Status),
		ActionCount:       len(r.exec.ExecutionOrder),
		LastAction:        r.lastAction(),
		ParentExecutionID: r.exec.ParentExecutionID,
		RecoveryScope:     r.exec.RecoveryScope,
		Error:             r.exec.Error,
		BlockedReason:     r.exec.BlockedReason,
		BlockedOn:         r.exec.BlockedOn,
		CreatedAt:         sagaJSONTime(r.createdAt),
		UpdatedAt:         sagaJSONTime(r.updatedAt),
	}
}

type sagaActionJSON struct {
	Name       string          `json:"name"`
	Order      int             `json:"order"`
	ExecutedAt string          `json:"executed_at,omitempty"`
	UndoneAt   string          `json:"undone_at,omitempty"`
	Error      string          `json:"error,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
}

type sagaChildJSON struct {
	ID             string `json:"id"`
	DefinitionName string `json:"definition_name"`
	Status         string `json:"status"`
	BlockedReason  string `json:"blocked_reason,omitempty"`
}

type sagaShowJSON struct {
	sagaListJSON
	InitialInputs map[string]any   `json:"initial_inputs,omitempty"`
	Actions       []sagaActionJSON `json:"actions"`
	Children      []sagaChildJSON  `json:"children,omitempty"`
}

// JSON carries the whole record. Truncating output would produce invalid JSON,
// and emitting it only under --full made the default a silently partial record
// with nothing marking the omission, while initial_inputs went out whole
// regardless. Size is the human reader's problem, so --full stays a text-mode
// concern and machine output is always complete.
func newSagaShowJSON(r *sagaRecord, children []*sagaRecord) sagaShowJSON {
	out := sagaShowJSON{
		sagaListJSON:  newSagaListJSON(r),
		InitialInputs: r.exec.InitialInputs,
		Actions:       []sagaActionJSON{},
	}

	for i, name := range sagaActionOrder(r.exec) {
		action := sagaActionJSON{Name: name, Order: i + 1}

		if result := r.exec.ExecutedActions[name]; result != nil {
			action.ExecutedAt = sagaJSONTime(result.ExecutedAt)
			if result.UndoneAt != nil {
				action.UndoneAt = sagaJSONTime(*result.UndoneAt)
			}
			action.Error = result.Error
			if len(result.Output) > 0 {
				action.Output = json.RawMessage(result.Output)
			}
		}

		out.Actions = append(out.Actions, action)
	}

	for _, c := range children {
		out.Children = append(out.Children, sagaChildJSON{
			ID:             c.exec.ID,
			DefinitionName: c.exec.DefinitionName,
			Status:         string(c.exec.Status),
			BlockedReason:  c.exec.BlockedReason,
		})
	}

	return out
}

func sagaJSONTime(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return ""
	}
	return t.Format(time.RFC3339)
}
