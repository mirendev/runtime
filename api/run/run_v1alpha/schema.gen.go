package run_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
)

type ExitSource string

const (
	ExitSourceTask ExitSource = "task"
	ExitSourceExec ExitSource = "exec"
)

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusTimedOut  RunStatus = "timed_out"
	RunStatusCanceled  RunStatus = "canceled"
	RunStatusSkipped   RunStatus = "skipped"
)

type Trigger string

const (
	TriggerDeploy   Trigger = "deploy"
	TriggerSchedule Trigger = "schedule"
	TriggerManual   Trigger = "manual"
)

const (
	RunAppId               = entity.Id("dev.miren.run/run.app")
	RunAttemptId           = entity.Id("dev.miren.run/run.attempt")
	RunAttemptRecordId     = entity.Id("dev.miren.run/run.attempt_record")
	RunCancelRequestedAtId = entity.Id("dev.miren.run/run.cancel_requested_at")
	RunCommandId           = entity.Id("dev.miren.run/run.command")
	RunEndedAtId           = entity.Id("dev.miren.run/run.ended_at")
	RunMaxAttemptsId       = entity.Id("dev.miren.run/run.max_attempts")
	RunQueuedAtId          = entity.Id("dev.miren.run/run.queued_at")
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
	RunTtyId               = entity.Id("dev.miren.run/run.tty")
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
	QueuedAt          time.Time       `cbor:"queued_at,omitempty" json:"queued_at"`
	Result            Result          `cbor:"result,omitempty" json:"result"`
	Sandbox           entity.Id       `cbor:"sandbox,omitempty" json:"sandbox,omitempty"`
	StartedAt         time.Time       `cbor:"started_at,omitempty" json:"started_at"`
	Status            RunStatus       `cbor:"status,omitempty" json:"status,omitempty"`
	Task              string          `cbor:"task,omitempty" json:"task,omitempty"`
	Tick              string          `cbor:"tick,omitempty" json:"tick,omitempty"`
	Timeout           string          `cbor:"timeout,omitempty" json:"timeout,omitempty"`
	Trigger           Trigger         `cbor:"trigger,omitempty" json:"trigger,omitempty"`
	Tty               bool            `cbor:"tty,omitempty" json:"tty,omitempty"`
	Version           entity.Id       `cbor:"version,omitempty" json:"version,omitempty"`
}

const (
	PENDING   RunStatus = RunStatusPending
	RUNNING   RunStatus = RunStatusRunning
	SUCCEEDED RunStatus = RunStatusSucceeded
	FAILED    RunStatus = RunStatusFailed
	TIMED_OUT RunStatus = RunStatusTimedOut
	CANCELED  RunStatus = RunStatusCanceled
	SKIPPED   RunStatus = RunStatusSkipped
)

var RunStatusFromId = map[entity.Id]RunStatus{RunStatusPendingId: RunStatusPending, RunStatusRunningId: RunStatusRunning, RunStatusSucceededId: RunStatusSucceeded, RunStatusFailedId: RunStatusFailed, RunStatusTimedOutId: RunStatusTimedOut, RunStatusCanceledId: RunStatusCanceled, RunStatusSkippedId: RunStatusSkipped}
var RunStatusToId = map[RunStatus]entity.Id{RunStatusPending: RunStatusPendingId, RunStatusRunning: RunStatusRunningId, RunStatusSucceeded: RunStatusSucceededId, RunStatusFailed: RunStatusFailedId, RunStatusTimedOut: RunStatusTimedOutId, RunStatusCanceled: RunStatusCanceledId, RunStatusSkipped: RunStatusSkippedId}

type RunTrigger = Trigger

const (
	DEPLOY   Trigger = TriggerDeploy
	SCHEDULE Trigger = TriggerSchedule
	MANUAL   Trigger = TriggerManual
)

var RunTriggerFromId = map[entity.Id]Trigger{RunTriggerDeployId: TriggerDeploy, RunTriggerScheduleId: TriggerSchedule, RunTriggerManualId: TriggerManual}
var RunTriggerToId = map[Trigger]entity.Id{TriggerDeploy: RunTriggerDeployId, TriggerSchedule: RunTriggerScheduleId, TriggerManual: RunTriggerManualId}

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
	if a, ok := e.Get(RunQueuedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.QueuedAt = a.Value.Time()
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
		o.Status = RunStatusFromId[a.Value.Id()]
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
		o.Trigger = RunTriggerFromId[a.Value.Id()]
	}
	if a, ok := e.Get(RunTtyId); ok && a.Value.Kind() == entity.KindBool {
		o.Tty = a.Value.Bool()
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
	if !entity.Empty(o.QueuedAt) {
		attrs = append(attrs, entity.Time(RunQueuedAtId, o.QueuedAt))
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
	if a, ok := RunStatusToId[o.Status]; ok {
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
	if a, ok := RunTriggerToId[o.Trigger]; ok {
		attrs = append(attrs, entity.Ref(RunTriggerId, a))
	}
	attrs = append(attrs, entity.Bool(RunTtyId, o.Tty))
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
	if !entity.Empty(o.QueuedAt) {
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
	if !entity.Empty(o.Tty) {
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
	sb.String("command", "dev.miren.run/run.command", schema.Doc("The resolved command line, as handed to \"sh -c\". An invoke override\narrives as argv and is shell-quoted into this, so a quoted argument\nsurvives: the container's process args are always built by the shell,\nand a list would have to be quoted into a string here regardless.\n"))
	sb.Time("ended_at", "dev.miren.run/run.ended_at", schema.Doc("When the run reached a terminal status"))
	sb.Int64("max_attempts", "dev.miren.run/run.max_attempts", schema.Doc("Total attempts allowed, resolved from the task's retries at creation"))
	sb.Time("queued_at", "dev.miren.run/run.queued_at", schema.Doc("When the run was first seen by the controller. The start deadline is\nmeasured from here rather than from started_at, which is only written\nonce a run is already running -- so a run held back by admission would\notherwise queue with no bound at all.\n"))
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
	sb.Bool("tty", "dev.miren.run/run.tty", schema.Doc("Allocate a pseudo-terminal for the run. Set when a person is invoking\ninteractively from a real terminal, and not for deploy- or\nschedule-triggered runs: a pty merges stdout and stderr and rewrites\nnewlines, which is right for a console session and wrong for a log a\nmachine reads. containerd fixes this at task creation, so like stdin it\ncannot be decided later.\n"))
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
	At     time.Time  `cbor:"at,omitempty" json:"at"`
	Code   int64      `cbor:"code" json:"code"`
	Source ExitSource `cbor:"source,omitempty" json:"source,omitempty"`
}

type ResultSource = ExitSource

const (
	TASK ExitSource = ExitSourceTask
	EXEC ExitSource = ExitSourceExec
)

var ResultSourceFromId = map[entity.Id]ExitSource{ResultSourceTaskId: ExitSourceTask, ResultSourceExecId: ExitSourceExec}
var ResultSourceToId = map[ExitSource]entity.Id{ExitSourceTask: ResultSourceTaskId, ExitSourceExec: ResultSourceExecId}

func (o *Result) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ResultAtId); ok && a.Value.Kind() == entity.KindTime {
		o.At = a.Value.Time()
	}
	if a, ok := e.Get(ResultCodeId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Code = a.Value.Int64()
	}
	if a, ok := e.Get(ResultSourceId); ok && a.Value.Kind() == entity.KindId {
		o.Source = ResultSourceFromId[a.Value.Id()]
	}
}

func (o *Result) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.At) {
		attrs = append(attrs, entity.Time(ResultAtId, o.At))
	}
	attrs = append(attrs, entity.Int64(ResultCodeId, o.Code))
	if a, ok := ResultSourceToId[o.Source]; ok {
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
	schema.RegisterEncodedSchema("dev.miren.run", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8cW˒\xdb*\x10\xfd\x8d\xb9s\xefM\xa5\xf2\xdc)\x95/Rah\xc9\xc4\x02d\x1e\x8e\xbdͤ\x92}>af\x12\x7fa\xb2N\xf1\x92\x8c\x00\xd9\x1b\x17M\xf79\xea'\xe0g\xc2\x11\x03F\xe0\xd00*\x817\xd2p\xd8QN\xd4\xe3!\xd9\xfc`7\xed\xe2\x97C\xecS\xe5\x84\xfa\xd3\x11\xc1\x10\xe5)c\xd7Q\x18\x88\xfa\xf1\xb4\xa1\xc4d\xc8\x06\x8d\xa3#\xc5v\xa1O#l(\xb1\xa6\xc7\x7f\n\xb6Z\x03\x1b\xb5\xb3\xef\xa3`1\x98r\xfdӂ^VA\xad\x04,$!\f\xf1\xd3o\xc7\xc0\x17\x1aKD\xb1`\xa3\xe0\xc0\xf5\xbc\xf2A/\xa8\x9b\x9c\xfa\x86$|s\x91\xbdI\x9dLYփt\xf8\xb7\xabx\xe0\x04H\x8b<\xc1v\x92,\x03є\x81\xa3x\xb7Nq\xa4\xbał\x80㠳\x98\xb8\xb1\x1e\x86B\x9cl\xc4ч\x11\x85\xcb\xfa\xbe_\x87k$\xf5\x1cǧ\v9\x8d\xe4\xf55\x1am\x94\xa3\xe8\xc2\xda\xc2;\xa5%\xe5}\x7f\x00\xa9\xa8\xe0\xfd\xe1#\x1a\xc6-\x1aFI\x19\x92\xa7\xd6V\x92\x05*\xcfTJ\xbcm\x01\x8c8\x86\xa1\x95\xb07\xa0.\x1cV%E\xeay\xa1\xc1\xb1`\fq\xe2\x93\x16\x85\v\x87\x1d\xee>\xc7\xddT\xf3\x179\x8e\xa1c\x1b\xc2\xf4I\x1a\x92\x9d\xa4\xdc\xff\xe6\xf0\xbd\x013\x7f\x97\xce\xe2\xf4\xe1g\x8b\xbcˑ\x12\x94\x19<\xac\v\xeb+\xe3w\x97\x8f\x9f\a\xde0v\x0f6\x80\xcf\v/\x1c\xb8\t\xbeo\xaeVǛOCA\x92y8\x97\x12\xe4\x11J\x18\x89\x81\x007lY\x01\xbb\xe7g-\x18\xf96\xf5k獵\xd8ٟ\xf6\x80\x06\x03\xea\x91\xc0\x11\xf0\xd27\x8fh\xac\x8ah\xa4v\x15\xbdU\r\x8e\x8c\x01ۀT_\x9c\xb5\xa3dn\x1f8\x16\x84\xf2\x1eK\xe8Vg#Ԭ\xd6\xc6Wg\xff\xbf\x02憁?W\xba)L\xb6\xcb\xf1\xff\x85\x1cK\xc3\xdb\xcaI\x90\xa5\xf8\xdcu\x88\x0e\x90\x95\xd3c\x1a\xaf\xecG\xe06Q\xcb@\x82Q\xd0\xf6\xd2p^\xb7\n\xda^\xed\xe88B\x96\x94`\x15\xb4[\x7f\x9e\x00Y\x06\x18̢\x9a*\x831\x00\x81l\xde#_\xd4S\x9bQ\xd2\n\xa3+\x96\x93>i\x9a\xef\xfd\"\xba\xf9\x8b!s3\xf1\xe4t\x8c1o3\xdb\x0e\x87\xbc\xa2\xae1ݜ\xb9\xd5\xe2\b,\x01(\x8e\x00\xbbZ\x9e\x99\x85&\xb5^\n\x13\xee\xd9(\\\xe0\xce5\x9c\xa4}\x0f\xd2w\xdb}\xa1ۢ\x81'\x0eB\xb1מ:\x02\xe3 N\xcb\xd2\aP\xe3\xb5\x1dCܠ\xa1f\xe4\xb5[\x85\xb7@\xcc\x00\xcbbF\xb3\xa8O\x8a\xf9\x10\x1c\x98\xd0\xe1[\xe5:\x15^oZ\x9f\xfc\xeb\xcd.\\\x8c\x1b!\x86Z\xcaÑ\xe23\x13\x85p.\xac\x9e7X\x1a\xbe\x1c\xc8\xf8&m\xd5 \xc2%\x91\xf7\x85S\xdepC|u.\xbf*\xe3\x1b\x84\xf7\x86\xca\xf9p\xda]n\xa4\xd7F~9\a\x8a\xda3\xb7\x06\x90\xc6\xe7Ɇ\x9e\x00\xf2{\xd8\x03\xea#\xb3\x9a\xdam$XZ\xed\xd4VH\xdd\xfa\xff\x03\u058b\xca\x7f\x82\x89`\xb5@\x7f\x01\x00\x00\xff\xff\x01\x00\x00\xff\xff\x00gb\x0fl\f\x00\x00"))
}
