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
	ResultAtId   = entity.Id("dev.miren.run/result.at")
	ResultCodeId = entity.Id("dev.miren.run/result.code")
)

type Result struct {
	At   time.Time `cbor:"at,omitempty" json:"at"`
	Code int64     `cbor:"code" json:"code"`
}

func (o *Result) Decode(e entity.AttrGetter) {
	if a, ok := e.Get(ResultAtId); ok && a.Value.Kind() == entity.KindTime {
		o.At = a.Value.Time()
	}
	if a, ok := e.Get(ResultCodeId); ok && a.Value.Kind() == entity.KindInt64 {
		o.Code = a.Value.Int64()
	}
}

func (o *Result) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.At) {
		attrs = append(attrs, entity.Time(ResultAtId, o.At))
	}
	attrs = append(attrs, entity.Int64(ResultCodeId, o.Code))
	return
}

func (o *Result) Empty() bool {
	if !entity.Empty(o.At) {
		return false
	}
	if !entity.Empty(o.Code) {
		return false
	}
	return true
}

func (o *Result) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("at", "dev.miren.run/result.at", schema.Doc("When the exit was observed. Always set, or a zero-code result encodes to nothing."))
	sb.Int64("code", "dev.miren.run/result.code", schema.Doc("Exit code. Required so a legitimate 0 survives encoding."), schema.Required)
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
	schema.RegisterEncodedSchema("dev.miren.run", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x8cVɒ\xd3<\x10~\x8d\xf9\xff\x01\x8ab\xbd\x99\xe2\x89\\\x8a\xbam\x8bh\xf1h\tɕ\xa28\xf3\f\xcc\x00O\bgJ[bٖ\x93KJ\xed\xee\xefK\x7f\xbd\xa8\xf4\x04\x92\b\x14\x80\x87F0\x8d\xb2\xd1N\xe2\x9eI0?\x0e\xc5\xc7\x0f\xfe\xa3?\xfc\n\x88\x87\xd2yF\xfd\xed@\t\xc2d\xc9\xd8u\f9\x98\xef\x8f;\x06n\x81l\xc88\x06R\xea\x0f\xf64⎁\x0f=\xfe\xb7\x12k-\x8aц\xf8>\x1b\x1eC\x99\xb4?=\xe8e\x15\xd4j\xa4J\x03\b\"O\x7f\x02\x83\x9cy<\x11\xa3J\x8cJ\xa2\xb4\x97S\x14=\xa3n\x96\xd47\x14\xe1[P\xf6\xa6L\xb2d\xd9\x16\x19\xf0o7\xf1(\x01\xa1%\x91`8[\x9e\x01,\x13\x18(\xdemS\x1c\x99m\xa9\x02\f\x1c\xecb\x16il\xcb0D\xc2N\x1d\xa3\x8clL\xfb\xfb~\x1bn\x89\xb6\x17\x1d\x9f&v\xa9\xe4\xf55\x1a\xebL\xa0\xe8\xd2\xd9\xc3;c5\x93}\x7f@m\x98\x92\xfd\xe1#\xe1\xe3@\xf8\xa8\x99 \xfa\xd4\xfaN\x8aD\x15\x99\xd6\n\xefG\x80\x12I\x91\xb7\x1a\x1f\x1c\x9aI\xc2f\xcdQf\xbe2\xe0T\tA$Ģec\x92p\xc0\xfd\xbf\xc4\xdd\xd4\xf3\x17K\x9c \xc76ɌE\xe2ŗ\xdc\xee'\x0f\xbf[\xc25\x1a\xc7\xe3\x9fv\xe9|e\x89\xee\x96K\x14\x817,\xcf\x17\xaf\xe2\xf3,\x8b\x00n\x92\xf2\xdd\xd5\x1a\xc7\xf0\xf3hC1՛\xe3\x90\x04\xd6:wuܟ\xad`n\x98\xf1Z\xe9+\x83\r(\x9d\xd8\xfb\x9f\xf6@\xb8C\xf3\xbb\xeb\b\xe3\b\xc7\xfb\x92#b\x9a\xe8\xecG\x94\xc0d?O2\x05%o\xaf\x9d\x94\xf5\xa8\xe4\xed͞\x8d#.\x04\xa7\xa8\xe4\x1d\xe2z \x1c\x9f\xaf\x86e73\x8eRD\xc0\xc5\xf8f\xbe\xecg\xbeZ\xd0*g+\x91g\xbfo\xc7aYQK\xcc>\x0eE8Ͷn\r\xc0h\x06\xf8\xd3|MW\x86ħ\xa0\\\xbaڳ1\xc1=\xd5p\x9a\xf5=\xea\x84K\xc6j\xbb\x1f;\xc0\x91\xabӼ\xfa\t\xd4Do'\x88t\x84ׂ\xa2w0t@p\x1c\xe7\xf5\xcca\xd9_S\x9b\xb6)f\x9d\x8d\xb4\x12\x9b\xabF\xb5\x93\xf3y\xcd/\x90\xd6p\x95.\x93eK\x82\xf3\x86\x9b\xe4kH\xf9\xd5:\xbe!\xf4\xc11}\xd9\xcb\xfd\xf4Cy\xbd,\xaf\xe2DQ{\xd4\xd4\x00\xda\xc5:y\xe9\x05\xe0\xbe\x02\xa8O\xebfi\x87L0\x8fڛAi\xdb\xc6ןϢ\xf2\x02<\x13l6\xe8\x1f\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff\xfbw&\xb6Z\n\x00\x00"))
}
