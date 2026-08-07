package run_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

const (
	RunAppId               = entity.Id("dev.miren.run/run.app")
	RunAttemptId           = entity.Id("dev.miren.run/run.attempt")
	RunAttemptRecordId     = entity.Id("dev.miren.run/run.attempt_record")
	RunCancelRequestedAtId = entity.Id("dev.miren.run/run.cancel_requested_at")
	RunCommandId           = entity.Id("dev.miren.run/run.command")
	RunEndedAtId           = entity.Id("dev.miren.run/run.ended_at")
	RunMaxAttemptsId       = entity.Id("dev.miren.run/run.max_attempts")
	RunResultId            = entity.Id("dev.miren.run/run.result")
	RunSandboxId           = entity.Id("dev.miren.run/run.sandbox")
	RunStartedAtId         = entity.Id("dev.miren.run/run.started_at")
	RunStatusId            = entity.Id("dev.miren.run/run.status")
	RunStatusPendingId     = entity.Id("dev.miren.run/status.pending")
	RunStatusRunningId     = entity.Id("dev.miren.run/status.running")
	RunStatusSucceededId   = entity.Id("dev.miren.run/status.succeeded")
	RunStatusFailedId      = entity.Id("dev.miren.run/status.failed")
	RunStatusTimedOutId    = entity.Id("dev.miren.run/status.timed_out")
	RunStatusCanceledId    = entity.Id("dev.miren.run/status.canceled")
	RunStatusSkippedId     = entity.Id("dev.miren.run/status.skipped")
	RunTaskId              = entity.Id("dev.miren.run/run.task")
	RunTickId              = entity.Id("dev.miren.run/run.tick")
	RunTimeoutId           = entity.Id("dev.miren.run/run.timeout")
	RunTriggerId           = entity.Id("dev.miren.run/run.trigger")
	RunTriggerDeployId     = entity.Id("dev.miren.run/trigger.deploy")
	RunTriggerScheduleId   = entity.Id("dev.miren.run/trigger.schedule")
	RunTriggerManualId     = entity.Id("dev.miren.run/trigger.manual")
	RunVersionId           = entity.Id("dev.miren.run/run.version")
)

type Run struct {
	ID                entity.Id       `json:"id"`
	App               entity.Id       `cbor:"app,omitempty" json:"app,omitempty"`
	Attempt           int64           `cbor:"attempt,omitempty" json:"attempt,omitempty"`
	AttemptRecord     []AttemptRecord `cbor:"attempt_record,omitempty" json:"attempt_record,omitempty"`
	CancelRequestedAt time.Time       `cbor:"cancel_requested_at,omitempty" json:"cancel_requested_at"`
	Command           string          `cbor:"command,omitempty" json:"command,omitempty"`
	EndedAt           time.Time       `cbor:"ended_at,omitempty" json:"ended_at"`
	MaxAttempts       int64           `cbor:"max_attempts,omitempty" json:"max_attempts,omitempty"`
	Result            Result          `cbor:"result,omitempty" json:"result"`
	Sandbox           entity.Id       `cbor:"sandbox,omitempty" json:"sandbox,omitempty"`
	StartedAt         time.Time       `cbor:"started_at,omitempty" json:"started_at"`
	Status            RunStatus       `cbor:"status,omitempty" json:"status,omitempty"`
	Task              string          `cbor:"task,omitempty" json:"task,omitempty"`
	Tick              string          `cbor:"tick,omitempty" json:"tick,omitempty"`
	Timeout           string          `cbor:"timeout,omitempty" json:"timeout,omitempty"`
	Trigger           RunTrigger      `cbor:"trigger,omitempty" json:"trigger,omitempty"`
	Version           entity.Id       `cbor:"version,omitempty" json:"version,omitempty"`
}

type RunStatus string

const (
	PENDING   RunStatus = "status.pending"
	RUNNING   RunStatus = "status.running"
	SUCCEEDED RunStatus = "status.succeeded"
	FAILED    RunStatus = "status.failed"
	TIMED_OUT RunStatus = "status.timed_out"
	CANCELED  RunStatus = "status.canceled"
	SKIPPED   RunStatus = "status.skipped"
)

var runstatusFromId = map[entity.Id]RunStatus{RunStatusPendingId: PENDING, RunStatusRunningId: RUNNING, RunStatusSucceededId: SUCCEEDED, RunStatusFailedId: FAILED, RunStatusTimedOutId: TIMED_OUT, RunStatusCanceledId: CANCELED, RunStatusSkippedId: SKIPPED}
var runstatusToId = map[RunStatus]entity.Id{PENDING: RunStatusPendingId, RUNNING: RunStatusRunningId, SUCCEEDED: RunStatusSucceededId, FAILED: RunStatusFailedId, TIMED_OUT: RunStatusTimedOutId, CANCELED: RunStatusCanceledId, SKIPPED: RunStatusSkippedId}

type RunTrigger string

const (
	DEPLOY   RunTrigger = "trigger.deploy"
	SCHEDULE RunTrigger = "trigger.schedule"
	MANUAL   RunTrigger = "trigger.manual"
)

var runtriggerFromId = map[entity.Id]RunTrigger{RunTriggerDeployId: DEPLOY, RunTriggerScheduleId: SCHEDULE, RunTriggerManualId: MANUAL}
var runtriggerToId = map[RunTrigger]entity.Id{DEPLOY: RunTriggerDeployId, SCHEDULE: RunTriggerScheduleId, MANUAL: RunTriggerManualId}

func (o *Run) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(RunAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(RunAttemptId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Attempt = a.Value.Int64()
	}
	for _, a := range e.GetAll(RunAttemptRecordId) {
		if a.Value.Kind() == entity.KindComponent {
			var v AttemptRecord
			v.Decode(a.Value.Component())
			o.AttemptRecord = append(o.AttemptRecord, v)
		}
	}
	if a, ok := e.Get(RunCancelRequestedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.CancelRequestedAt = a.Value.Time()
	}
	if a, ok := e.Get(RunCommandId); ok && a.Value.Kind() == entity.KindString {
		o.Command = a.Value.String()
	}
	if a, ok := e.Get(RunEndedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.EndedAt = a.Value.Time()
	}
	if a, ok := e.Get(RunMaxAttemptsId); ok && a.Value.Kind() == entity.KindInt64 {
		o.MaxAttempts = a.Value.Int64()
	}
	if a, ok := e.Get(RunResultId); ok && a.Value.Kind() == entity.KindComponent {
		o.Result.Decode(a.Value.Component())
	}
	if a, ok := e.Get(RunSandboxId); ok && a.Value.Kind() == entity.KindId {
		o.Sandbox = a.Value.Id()
	}
	if a, ok := e.Get(RunStartedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.StartedAt = a.Value.Time()
	}
	if a, ok := e.Get(RunStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = runstatusFromId[a.Value.Id()]
	}
	if a, ok := e.Get(RunTaskId); ok && a.Value.Kind() == entity.KindString {
		o.Task = a.Value.String()
	}
	if a, ok := e.Get(RunTickId); ok && a.Value.Kind() == entity.KindString {
		o.Tick = a.Value.String()
	}
	if a, ok := e.Get(RunTimeoutId); ok && a.Value.Kind() == entity.KindString {
		o.Timeout = a.Value.String()
	}
	if a, ok := e.Get(RunTriggerId); ok && a.Value.Kind() == entity.KindId {
		o.Trigger = runtriggerFromId[a.Value.Id()]
	}
	if a, ok := e.Get(RunVersionId); ok && a.Value.Kind() == entity.KindId {
		o.Version = a.Value.Id()
	}
}

func (o *Run) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindRun)
}

func (o *Run) ShortKind() string {
	return "run"
}

func (o *Run) Kind() entity.Id {
	return KindRun
}

func (o *Run) EntityId() entity.Id {
	return o.ID
}

func (o *Run) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(RunAppId, o.App))
	}
	if !entity.Empty(o.Attempt) {
		attrs = append(attrs, entity.Int64(RunAttemptId, o.Attempt))
	}
	for _, v := range o.AttemptRecord {
		attrs = append(attrs, entity.Component(RunAttemptRecordId, v.Encode()))
	}
	if !entity.Empty(o.CancelRequestedAt) {
		attrs = append(attrs, entity.Time(RunCancelRequestedAtId, o.CancelRequestedAt))
	}
	if !entity.Empty(o.Command) {
		attrs = append(attrs, entity.String(RunCommandId, o.Command))
	}
	if !entity.Empty(o.EndedAt) {
		attrs = append(attrs, entity.Time(RunEndedAtId, o.EndedAt))
	}
	if !entity.Empty(o.MaxAttempts) {
		attrs = append(attrs, entity.Int64(RunMaxAttemptsId, o.MaxAttempts))
	}
	if !o.Result.Empty() {
		attrs = append(attrs, entity.Component(RunResultId, o.Result.Encode()))
	}
	if !entity.Empty(o.Sandbox) {
		attrs = append(attrs, entity.Ref(RunSandboxId, o.Sandbox))
	}
	if !entity.Empty(o.StartedAt) {
		attrs = append(attrs, entity.Time(RunStartedAtId, o.StartedAt))
	}
	if a, ok := runstatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(RunStatusId, a))
	}
	if !entity.Empty(o.Task) {
		attrs = append(attrs, entity.String(RunTaskId, o.Task))
	}
	if !entity.Empty(o.Tick) {
		attrs = append(attrs, entity.String(RunTickId, o.Tick))
	}
	if !entity.Empty(o.Timeout) {
		attrs = append(attrs, entity.String(RunTimeoutId, o.Timeout))
	}
	if a, ok := runtriggerToId[o.Trigger]; ok {
		attrs = append(attrs, entity.Ref(RunTriggerId, a))
	}
	if !entity.Empty(o.Version) {
		attrs = append(attrs, entity.Ref(RunVersionId, o.Version))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindRun))
	return
}

func (o *Run) Empty() bool {
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.Attempt) {
		return false
	}
	if len(o.AttemptRecord) != 0 {
		return false
	}
	if !entity.Empty(o.CancelRequestedAt) {
		return false
	}
	if !entity.Empty(o.Command) {
		return false
	}
	if !entity.Empty(o.EndedAt) {
		return false
	}
	if !entity.Empty(o.MaxAttempts) {
		return false
	}
	if !o.Result.Empty() {
		return false
	}
	if !entity.Empty(o.Sandbox) {
		return false
	}
	if !entity.Empty(o.StartedAt) {
		return false
	}
	if o.Status != "" {
		return false
	}
	if !entity.Empty(o.Task) {
		return false
	}
	if !entity.Empty(o.Tick) {
		return false
	}
	if !entity.Empty(o.Timeout) {
		return false
	}
	if o.Trigger != "" {
		return false
	}
	if !entity.Empty(o.Version) {
		return false
	}
	return true
}

func (o *Run) InitSchema(sb *schema.SchemaBuilder) {
	sb.Ref("app", "dev.miren.run/run.app", schema.Doc("The app this run belongs to"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Int64("attempt", "dev.miren.run/run.attempt", schema.Doc("1-based, incremented in place on retry. Retries deliberately reuse this\nentity rather than creating a second one: for a scheduled run the entity\nid is derived from the tick, and that derivation is the dedup guarantee.\n"))
	sb.Component("attempt_record", "dev.miren.run/run.attempt_record", schema.Doc("History of attempts made for this run"), schema.Many)
	(&AttemptRecord{}).InitSchema(sb.Builder("run.attempt_record"))
	sb.Time("cancel_requested_at", "dev.miren.run/run.cancel_requested_at", schema.Doc("Set by `miren app runs cancel`. Cancellation is a request rather than a\nstatus write because the controller owns every status transition; a\nsecond writer would race it.\n"))
	sb.String("command", "dev.miren.run/run.command", schema.Doc("The resolved command, after any invoke override"))
	sb.Time("ended_at", "dev.miren.run/run.ended_at", schema.Doc("When the run reached a terminal status"))
	sb.Int64("max_attempts", "dev.miren.run/run.max_attempts", schema.Doc("Total attempts allowed, resolved from the task's retries at creation"))
	sb.Component("result", "dev.miren.run/run.result", schema.Doc("The exit reported for the current attempt"))
	(&Result{}).InitSchema(sb.Builder("run.result"))
	sb.Ref("sandbox", "dev.miren.run/run.sandbox", schema.Doc("The sandbox executing the current attempt, once one has been created.\nEarlier attempts' sandboxes are recorded in attempt_record rather than\nbeing overwritten here.\n"))
	sb.Time("started_at", "dev.miren.run/run.started_at", schema.Doc("When the current attempt entered running"))
	sb.Singleton("dev.miren.run/status.pending")
	sb.Singleton("dev.miren.run/status.running")
	sb.Singleton("dev.miren.run/status.succeeded")
	sb.Singleton("dev.miren.run/status.failed")
	sb.Singleton("dev.miren.run/status.timed_out")
	sb.Singleton("dev.miren.run/status.canceled")
	sb.Singleton("dev.miren.run/status.skipped")
	sb.Ref("status", "dev.miren.run/run.status", schema.Doc("Indexed because the deadline sweep lists non-terminal runs on a short\ninterval; scanning every run of every app to find them would not hold up.\n\n\"skipped\" is a scheduled run that never executed because its predecessor\nwas still going. It exists so a skipped tick is visible rather than\nlooking like a gap, since \"my job stopped running\" and \"my job got\nslower than its interval\" are otherwise indistinguishable from outside.\n"), schema.Indexed, schema.Choices(RunStatusPendingId, RunStatusRunningId, RunStatusSucceededId, RunStatusFailedId, RunStatusTimedOutId, RunStatusCanceledId, RunStatusSkippedId))
	sb.String("task", "dev.miren.run/run.task", schema.Doc("Task name from app.toml"))
	sb.String("tick", "dev.miren.run/run.tick", schema.Doc("Scheduled runs only: the RFC 3339 instant this run claims. Every replica\nderives the same ticks from the stored calendar expression, so creating\nthe run is a create-if-absent on an id derived from this, and etcd picks\nthe single winner.\n"))
	sb.String("timeout", "dev.miren.run/run.timeout", schema.Doc("Go duration bounding the run; empty means the platform default, \"0\" unbounded"))
	sb.Singleton("dev.miren.run/trigger.deploy")
	sb.Singleton("dev.miren.run/trigger.schedule")
	sb.Singleton("dev.miren.run/trigger.manual")
	sb.Ref("trigger", "dev.miren.run/run.trigger", schema.Doc("What started this run"), schema.Choices(RunTriggerDeployId, RunTriggerScheduleId, RunTriggerManualId))
	sb.Ref("version", "dev.miren.run/run.version", schema.Doc("The app version whose image and config this run executes"))
}

const (
	AttemptRecordAttemptId   = entity.Id("dev.miren.run/attempt_record.attempt")
	AttemptRecordEndedAtId   = entity.Id("dev.miren.run/attempt_record.ended_at")
	AttemptRecordExitCodeId  = entity.Id("dev.miren.run/attempt_record.exit_code")
	AttemptRecordSandboxId   = entity.Id("dev.miren.run/attempt_record.sandbox")
	AttemptRecordStartedAtId = entity.Id("dev.miren.run/attempt_record.started_at")
	AttemptRecordStatusId    = entity.Id("dev.miren.run/attempt_record.status")
)

type AttemptRecord struct {
	Attempt   int64     `cbor:"attempt" json:"attempt"`
	EndedAt   time.Time `cbor:"ended_at,omitempty" json:"ended_at"`
	ExitCode  int64     `cbor:"exit_code" json:"exit_code"`
	Sandbox   entity.Id `cbor:"sandbox,omitempty" json:"sandbox,omitempty"`
	StartedAt time.Time `cbor:"started_at,omitempty" json:"started_at"`
	Status    string    `cbor:"status,omitempty" json:"status,omitempty"`
}

func (o *AttemptRecord) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(AttemptRecordAttemptId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Attempt = a.Value.Int64()
	}
	if a, ok := e.Get(AttemptRecordEndedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.EndedAt = a.Value.Time()
	}
	if a, ok := e.Get(AttemptRecordExitCodeId); ok && a.Value.Kind() == entity.KindInt64 {
		o.ExitCode = a.Value.Int64()
	}
	if a, ok := e.Get(AttemptRecordSandboxId); ok && a.Value.Kind() == entity.KindId {
		o.Sandbox = a.Value.Id()
	}
	if a, ok := e.Get(AttemptRecordStartedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.StartedAt = a.Value.Time()
	}
	if a, ok := e.Get(AttemptRecordStatusId); ok && a.Value.Kind() == entity.KindString {
		o.Status = a.Value.String()
	}
}

func (o *AttemptRecord) Encode() (attrs []entity.Attr) {
	attrs = append(attrs, entity.Int64(AttemptRecordAttemptId, o.Attempt))
	if !entity.Empty(o.EndedAt) {
		attrs = append(attrs, entity.Time(AttemptRecordEndedAtId, o.EndedAt))
	}
	attrs = append(attrs, entity.Int64(AttemptRecordExitCodeId, o.ExitCode))
	if !entity.Empty(o.Sandbox) {
		attrs = append(attrs, entity.Ref(AttemptRecordSandboxId, o.Sandbox))
	}
	if !entity.Empty(o.StartedAt) {
		attrs = append(attrs, entity.Time(AttemptRecordStartedAtId, o.StartedAt))
	}
	if !entity.Empty(o.Status) {
		attrs = append(attrs, entity.String(AttemptRecordStatusId, o.Status))
	}
	return
}

func (o *AttemptRecord) Empty() bool {
	if !entity.Empty(o.Attempt) {
		return false
	}
	if !entity.Empty(o.EndedAt) {
		return false
	}
	if !entity.Empty(o.ExitCode) {
		return false
	}
	if !entity.Empty(o.Sandbox) {
		return false
	}
	if !entity.Empty(o.StartedAt) {
		return false
	}
	if !entity.Empty(o.Status) {
		return false
	}
	return true
}

func (o *AttemptRecord) InitSchema(sb *schema.SchemaBuilder) {
	sb.Int64("attempt", "dev.miren.run/attempt_record.attempt", schema.Required)
	sb.Time("ended_at", "dev.miren.run/attempt_record.ended_at")
	sb.Int64("exit_code", "dev.miren.run/attempt_record.exit_code", schema.Required)
	sb.Ref("sandbox", "dev.miren.run/attempt_record.sandbox")
	sb.Time("started_at", "dev.miren.run/attempt_record.started_at")
	sb.String("status", "dev.miren.run/attempt_record.status")
}

const (
	ResultAtId         = entity.Id("dev.miren.run/result.at")
	ResultCodeId       = entity.Id("dev.miren.run/result.code")
	ResultSourceId     = entity.Id("dev.miren.run/result.source")
	ResultSourceTaskId = entity.Id("dev.miren.run/source.task")
	ResultSourceExecId = entity.Id("dev.miren.run/source.exec")
)

type Result struct {
	At     time.Time    `cbor:"at,omitempty" json:"at"`
	Code   int64        `cbor:"code" json:"code"`
	Source ResultSource `cbor:"source,omitempty" json:"source,omitempty"`
}

type ResultSource string

const (
	TASK ResultSource = "source.task"
	EXEC ResultSource = "source.exec"
)

var ResultsourceFromId = map[entity.Id]ResultSource{ResultSourceTaskId: TASK, ResultSourceExecId: EXEC}
var ResultsourceToId = map[ResultSource]entity.Id{TASK: ResultSourceTaskId, EXEC: ResultSourceExecId}

func (o *Result) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ResultAtId); ok && a.Value.Kind() == entity.KindTime {
		o.At = a.Value.Time()
	}
	if a, ok := e.Get(ResultCodeId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Code = a.Value.Int64()
	}
	if a, ok := e.Get(ResultSourceId); ok && a.Value.Kind() == entity.KindId {
		o.Source = ResultsourceFromId[a.Value.Id()]
	}
}

func (o *Result) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.At) {
		attrs = append(attrs, entity.Time(ResultAtId, o.At))
	}
	attrs = append(attrs, entity.Int64(ResultCodeId, o.Code))
	if a, ok := ResultsourceToId[o.Source]; ok {
		attrs = append(attrs, entity.Ref(ResultSourceId, a))
	}
	return
}

func (o *Result) Empty() bool {
	if !entity.Empty(o.At) {
		return false
	}
	if !entity.Empty(o.Code) {
		return false
	}
	if o.Source != "" {
		return false
	}
	return true
}

func (o *Result) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("at", "dev.miren.run/result.at", schema.Doc("When the exit was observed. Always set, or a zero-code result encodes to nothing."))
	sb.Int64("code", "dev.miren.run/result.code", schema.Doc("Exit code. Required so a legitimate 0 survives encoding."), schema.Required)
	sb.Singleton("dev.miren.run/source.task")
	sb.Singleton("dev.miren.run/source.exec")
	sb.Ref("source", "dev.miren.run/result.source", schema.Doc("Which reporter observed the exit: \"task\" is the container's own primary process, \"exec\" is a process run for an attached client. Recorded because the two have different failure modes -- a task exit is observed whether or not anyone is watching, while an exec exit is only seen while a client is connected."), schema.Choices(ResultSourceTaskId, ResultSourceExecId))
}

const (
	RunSlotAcquiredAtId = entity.Id("dev.miren.run/run_slot.acquired_at")
	RunSlotAppId        = entity.Id("dev.miren.run/run_slot.app")
	RunSlotRunId        = entity.Id("dev.miren.run/run_slot.run")
	RunSlotTaskId       = entity.Id("dev.miren.run/run_slot.task")
)

type RunSlot struct {
	ID         entity.Id `json:"id"`
	AcquiredAt time.Time `cbor:"acquired_at,omitempty" json:"acquired_at"`
	App        entity.Id `cbor:"app,omitempty" json:"app,omitempty"`
	Run        entity.Id `cbor:"run,omitempty" json:"run,omitempty"`
	Task       string    `cbor:"task,omitempty" json:"task,omitempty"`
}

func (o *RunSlot) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(RunSlotAcquiredAtId); ok && a.Value.Kind() == entity.KindTime {
		o.AcquiredAt = a.Value.Time()
	}
	if a, ok := e.Get(RunSlotAppId); ok && a.Value.Kind() == entity.KindId {
		o.App = a.Value.Id()
	}
	if a, ok := e.Get(RunSlotRunId); ok && a.Value.Kind() == entity.KindId {
		o.Run = a.Value.Id()
	}
	if a, ok := e.Get(RunSlotTaskId); ok && a.Value.Kind() == entity.KindString {
		o.Task = a.Value.String()
	}
}

func (o *RunSlot) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindRunSlot)
}

func (o *RunSlot) ShortKind() string {
	return "run_slot"
}

func (o *RunSlot) Kind() entity.Id {
	return KindRunSlot
}

func (o *RunSlot) EntityId() entity.Id {
	return o.ID
}

func (o *RunSlot) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.AcquiredAt) {
		attrs = append(attrs, entity.Time(RunSlotAcquiredAtId, o.AcquiredAt))
	}
	if !entity.Empty(o.App) {
		attrs = append(attrs, entity.Ref(RunSlotAppId, o.App))
	}
	if !entity.Empty(o.Run) {
		attrs = append(attrs, entity.Ref(RunSlotRunId, o.Run))
	}
	if !entity.Empty(o.Task) {
		attrs = append(attrs, entity.String(RunSlotTaskId, o.Task))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindRunSlot))
	return
}

func (o *RunSlot) Empty() bool {
	if !entity.Empty(o.AcquiredAt) {
		return false
	}
	if !entity.Empty(o.App) {
		return false
	}
	if !entity.Empty(o.Run) {
		return false
	}
	if !entity.Empty(o.Task) {
		return false
	}
	return true
}

func (o *RunSlot) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("acquired_at", "dev.miren.run/run_slot.acquired_at", schema.Doc("When the current holder took the slot"))
	sb.Ref("app", "dev.miren.run/run_slot.app", schema.Doc("The app whose task this slot guards"), schema.Indexed, schema.Tags("dev.miren.app_ref"))
	sb.Ref("run", "dev.miren.run/run_slot.run", schema.Doc("The run currently holding the slot. Validity is decided by reading this\nrun rather than by a lease: a crashed controller leaves the pointer\naimed at a terminal or missing run, which the next admission reclaims.\nA timeout would have to be tuned, and would deny service if set wrong.\n"))
	sb.String("task", "dev.miren.run/run_slot.task", schema.Doc("The task name this slot guards"))
}

var (
	KindRun     = entity.Id("dev.miren.run/kind.run")
	KindRunSlot = entity.Id("dev.miren.run/kind.run_slot")
	Schema      = entity.Id("dev.miren.run/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.run", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&Run{}).InitSchema(sb)
		(&RunSlot{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.run", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8cVɒ\xd3<\x10~\x8d\xf9\x7f\x96\xa2Xo\xa6x\"\x97\xa2n\xdb\"\x96\xe4\xd1\x12\x923\x14g\x9eaf\x80'\x843\xa5-\xb1%\xcb\xc9%\xa5\xf6\xd7\xdf\xe7^\x15?\x81 \x1c9\xe0\xa1\xe1L\xa1h\x94\x15\xb8g\x02\xf4\xc3a\xf1\xf0\xa3{\xe8\x0e\xbf<\xe3~\t\x9eY\x7f;\x90\x9c0\xb1T\xec:\x86#\xe8\x1f\x8f;\x06\xb6`6d\x9a\xbc(u\as\x9ap\xc7\xc0\xb9\x1e\xff[\xf15\x06\xf9d\xbc\x7f\x9f\fǡL\x98\x9f\x8e\xf4\xaaJj\x15R\xa9\x008\x11\xa7?^Ad\x88\x13bT\xf2I\n\x14\xe6r\nIg\xd2M)}C\x11\xbe\xfb\xcc\xde.\x83\\\xaal'\xe9\xf9\xef6\xf9(\x00\xa1%A`8[N\x01\f\xe3\xe8%\xdeoK\x1c\x99i\xa9\x04\xf4\x1a\xecb.\xc2\xd8NC\x13\x01;y\fi$c\xde\xdf\x0f\xdbtC\x94\xb9\xe4\xf1yf/3ysM\xc6X\xed%\xbaxv\xf4N\x1b\xc5D\xdf\x1fPi&E\x7f\xf8D\xc6i \xe3\xa4\x18'\xeaԺN\xf2(\x15\x94\xd6\n\xefF\x80\x12Aql\x15\xde[Գ\x80\xf5\x1a\xb0\x8c|e\xc0\xa9\xe4\x9c\b\bEK\xc6,`\xcf\xfb\xbf\xe4\xdd\xd4\xf3\x97%\x8f\x93c\x1b\xd3\fE\x1a\x17OR\xbb\x9f\x1c\xfd\xae\xa4+\xd4v\f/\xed\xe2\xf9\xca\x12ݕK\x14\x887,\xcfW\x97ŗ,\nOnb滫5\x0e\xee\xe7ц\xc5T\xfb4\x9f\xad2\xb4\xb4\x8ab\x9c\xa3p\xf6/Ba\xf9\xde\xfd\xb4\a2Z\xd4\x0f\x80G\xa4\xf9k\x03\xa3q\x10\x18\xa2\xf7\x15\xdcA\x9b#\x19\x8b\\\x9b\x9e\xab+\xf7|\x85sÞ\xd5\xda_Y\xae\xa2(\xbf\xbb\x8e\xb0\x11\x8b\xda\x06N\x13\xc0~B\x01L\xf4y\x90\xd1)\xa2\xbd\xb2BԽ\"\xda\xeb=\x9b&,\x12\x8e^\x11\x1d\u008a\"\x1c_\xac\xba%\x98iK)\"`\xb1BI/\xe1\xccU\vZiM\xc5\xf3\x8c\xbbv\x1cʊ\xba\t\b\x83\xe9O\xd9\xe6\xaf\x11\x18M\x04wʯ\x8a\x95!q!H\x1b\xff^\x921\xe3=\xd5x\x8a\xf5=\xaaȋ\xc6j\xbb\x1f;\xc0i\x94\xa7\xbc\xfa\x91\xd4\x04\xb4\xe3DX2֜\x02:h: \xd8\x11\xf3z&\xb7\x84ײ\x8d\xdb\x14\xa2NF\\\x89\xcdU\xa3ʊ|^\xd3WP\xabG\x19/\xb4\xb2%\x1e\xbc\xe16\xfb\xe6C~\xbd\xceo\b\xbd\xb7L]\xf6r?\x7f\xb0\xbc\xe2ʿ\x83(Q\xfb\xb0\xaa\x11\x94\rur\xa9/\b\xf9\x95\x98\b\xf5i\xdd,\xed\x90\x04r\xaf\xbd\x1e\xa42m\xf8\x02uQT\xbeB\xcf\x02\x9b\r\xfa\a\x00\x00\xff\xff\x01\x00\x00\xff\xff\x18\xc4m\xf4\xde\n\x00\x00"))
}
