package runner_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

const (
	RunnerInviteClaimedAtId       = entity.Id("dev.miren.runner/runner_invite.claimed_at")
	RunnerInviteClaimedById       = entity.Id("dev.miren.runner/runner_invite.claimed_by")
	RunnerInviteCodeHashId        = entity.Id("dev.miren.runner/runner_invite.code_hash")
	RunnerInviteCreatedAtId       = entity.Id("dev.miren.runner/runner_invite.created_at")
	RunnerInviteEnrollmentCountId = entity.Id("dev.miren.runner/runner_invite.enrollment_count")
	RunnerInviteExpiresAtId       = entity.Id("dev.miren.runner/runner_invite.expires_at")
	RunnerInviteLabelsId          = entity.Id("dev.miren.runner/runner_invite.labels")
	RunnerInviteNameId            = entity.Id("dev.miren.runner/runner_invite.name")
	RunnerInviteReusableId        = entity.Id("dev.miren.runner/runner_invite.reusable")
	RunnerInviteStatusId          = entity.Id("dev.miren.runner/runner_invite.status")
	RunnerInviteStatusPendingId   = entity.Id("dev.miren.runner/status.pending")
	RunnerInviteStatusClaimedId   = entity.Id("dev.miren.runner/status.claimed")
	RunnerInviteStatusRevokedId   = entity.Id("dev.miren.runner/status.revoked")
	RunnerInviteStatusExpiredId   = entity.Id("dev.miren.runner/status.expired")
)

type RunnerInvite struct {
	ID              entity.Id          `json:"id"`
	ClaimedAt       time.Time          `cbor:"claimed_at,omitempty" json:"claimed_at"`
	ClaimedBy       string             `cbor:"claimed_by,omitempty" json:"claimed_by,omitempty"`
	CodeHash        string             `cbor:"code_hash,omitempty" json:"code_hash,omitempty"`
	CreatedAt       time.Time          `cbor:"created_at,omitempty" json:"created_at"`
	EnrollmentCount int64              `cbor:"enrollment_count,omitempty" json:"enrollment_count,omitempty"`
	ExpiresAt       time.Time          `cbor:"expires_at,omitempty" json:"expires_at"`
	Labels          types.Labels       `cbor:"labels,omitempty" json:"labels,omitempty"`
	Name            string             `cbor:"name,omitempty" json:"name,omitempty"`
	Reusable        bool               `cbor:"reusable,omitempty" json:"reusable,omitempty"`
	Status          RunnerInviteStatus `cbor:"status,omitempty" json:"status,omitempty"`
}

type RunnerInviteStatus string

const (
	PENDING RunnerInviteStatus = "status.pending"
	CLAIMED RunnerInviteStatus = "status.claimed"
	REVOKED RunnerInviteStatus = "status.revoked"
	EXPIRED RunnerInviteStatus = "status.expired"
)

var runner_invitestatusFromId = map[entity.Id]RunnerInviteStatus{RunnerInviteStatusPendingId: PENDING, RunnerInviteStatusClaimedId: CLAIMED, RunnerInviteStatusRevokedId: REVOKED, RunnerInviteStatusExpiredId: EXPIRED}
var runner_invitestatusToId = map[RunnerInviteStatus]entity.Id{PENDING: RunnerInviteStatusPendingId, CLAIMED: RunnerInviteStatusClaimedId, REVOKED: RunnerInviteStatusRevokedId, EXPIRED: RunnerInviteStatusExpiredId}

func (o *RunnerInvite) Decode(e entity.AttrGetter) {
	o.ID = entity.MustGet(e, entity.DBId).Value.Id()
	if a, ok := e.Get(RunnerInviteClaimedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.ClaimedAt = a.Value.Time()
	}
	if a, ok := e.Get(RunnerInviteClaimedById); ok && a.Value.Kind() == entity.KindString {
		o.ClaimedBy = a.Value.String()
	}
	if a, ok := e.Get(RunnerInviteCodeHashId); ok && a.Value.Kind() == entity.KindString {
		o.CodeHash = a.Value.String()
	}
	if a, ok := e.Get(RunnerInviteCreatedAtId); ok && a.Value.Kind() == entity.KindTime {
		o.CreatedAt = a.Value.Time()
	}
	if a, ok := e.Get(RunnerInviteEnrollmentCountId); ok && a.Value.Kind() == entity.KindInt64 {
		o.EnrollmentCount = a.Value.Int64()
	}
	if a, ok := e.Get(RunnerInviteExpiresAtId); ok && a.Value.Kind() == entity.KindTime {
		o.ExpiresAt = a.Value.Time()
	}
	for _, a := range e.GetAll(RunnerInviteLabelsId) {
		if a.Value.Kind() == entity.KindLabel {
			o.Labels = append(o.Labels, a.Value.Label())
		}
	}
	if a, ok := e.Get(RunnerInviteNameId); ok && a.Value.Kind() == entity.KindString {
		o.Name = a.Value.String()
	}
	if a, ok := e.Get(RunnerInviteReusableId); ok && a.Value.Kind() == entity.KindBool {
		o.Reusable = a.Value.Bool()
	}
	if a, ok := e.Get(RunnerInviteStatusId); ok && a.Value.Kind() == entity.KindId {
		o.Status = runner_invitestatusFromId[a.Value.Id()]
	}
}

func (o *RunnerInvite) Is(e entity.AttrGetter) bool {
	return entity.Is(e, KindRunnerInvite)
}

func (o *RunnerInvite) ShortKind() string {
	return "runner_invite"
}

func (o *RunnerInvite) Kind() entity.Id {
	return KindRunnerInvite
}

func (o *RunnerInvite) EntityId() entity.Id {
	return o.ID
}

func (o *RunnerInvite) Encode() (attrs []entity.Attr) {
	if !entity.Empty(o.ClaimedAt) {
		attrs = append(attrs, entity.Time(RunnerInviteClaimedAtId, o.ClaimedAt))
	}
	if !entity.Empty(o.ClaimedBy) {
		attrs = append(attrs, entity.String(RunnerInviteClaimedById, o.ClaimedBy))
	}
	if !entity.Empty(o.CodeHash) {
		attrs = append(attrs, entity.String(RunnerInviteCodeHashId, o.CodeHash))
	}
	if !entity.Empty(o.CreatedAt) {
		attrs = append(attrs, entity.Time(RunnerInviteCreatedAtId, o.CreatedAt))
	}
	if !entity.Empty(o.EnrollmentCount) {
		attrs = append(attrs, entity.Int64(RunnerInviteEnrollmentCountId, o.EnrollmentCount))
	}
	if !entity.Empty(o.ExpiresAt) {
		attrs = append(attrs, entity.Time(RunnerInviteExpiresAtId, o.ExpiresAt))
	}
	for _, v := range o.Labels {
		attrs = append(attrs, entity.Label(RunnerInviteLabelsId, v.Key, v.Value))
	}
	if !entity.Empty(o.Name) {
		attrs = append(attrs, entity.String(RunnerInviteNameId, o.Name))
	}
	attrs = append(attrs, entity.Bool(RunnerInviteReusableId, o.Reusable))
	if a, ok := runner_invitestatusToId[o.Status]; ok {
		attrs = append(attrs, entity.Ref(RunnerInviteStatusId, a))
	}
	attrs = append(attrs, entity.Ref(entity.EntityKind, KindRunnerInvite))
	return
}

func (o *RunnerInvite) Empty() bool {
	if !entity.Empty(o.ClaimedAt) {
		return false
	}
	if !entity.Empty(o.ClaimedBy) {
		return false
	}
	if !entity.Empty(o.CodeHash) {
		return false
	}
	if !entity.Empty(o.CreatedAt) {
		return false
	}
	if !entity.Empty(o.EnrollmentCount) {
		return false
	}
	if !entity.Empty(o.ExpiresAt) {
		return false
	}
	if len(o.Labels) != 0 {
		return false
	}
	if !entity.Empty(o.Name) {
		return false
	}
	if !entity.Empty(o.Reusable) {
		return false
	}
	if o.Status != "" {
		return false
	}
	return true
}

func (o *RunnerInvite) InitSchema(sb *schema.SchemaBuilder) {
	sb.Time("claimed_at", "dev.miren.runner/runner_invite.claimed_at", schema.Doc("When the invite was claimed"))
	sb.String("claimed_by", "dev.miren.runner/runner_invite.claimed_by", schema.Doc("Runner ID that claimed this invite"))
	sb.String("code_hash", "dev.miren.runner/runner_invite.code_hash", schema.Doc("SHA-256 hash of the join code (code itself is not stored)"), schema.Indexed)
	sb.Time("created_at", "dev.miren.runner/runner_invite.created_at", schema.Doc("When the invite was created"))
	sb.Int64("enrollment_count", "dev.miren.runner/runner_invite.enrollment_count", schema.Doc("Number of runners that have joined using this invite"))
	sb.Time("expires_at", "dev.miren.runner/runner_invite.expires_at", schema.Doc("When the invite expires"))
	sb.Label("labels", "dev.miren.runner/runner_invite.labels", schema.Doc("Labels to apply to the runner when it joins"), schema.Many)
	sb.String("name", "dev.miren.runner/runner_invite.name", schema.Doc("Human-readable name for audit and management"), schema.Indexed)
	sb.Bool("reusable", "dev.miren.runner/runner_invite.reusable", schema.Doc("Whether this invite can be used multiple times"))
	sb.Singleton("dev.miren.runner/status.pending")
	sb.Singleton("dev.miren.runner/status.claimed")
	sb.Singleton("dev.miren.runner/status.revoked")
	sb.Singleton("dev.miren.runner/status.expired")
	sb.Ref("status", "dev.miren.runner/runner_invite.status", schema.Doc("Status of the invite"), schema.Indexed, schema.Choices(RunnerInviteStatusPendingId, RunnerInviteStatusClaimedId, RunnerInviteStatusRevokedId, RunnerInviteStatusExpiredId))
}

var (
	KindRunnerInvite = entity.Id("dev.miren.runner/kind.runner_invite")
	Schema           = entity.Id("dev.miren.runner/schema.v1alpha")
)

func init() {
	schema.Register("dev.miren.runner", "v1alpha", func(sb *schema.SchemaBuilder) {
		(&RunnerInvite{}).InitSchema(sb)
	})
	schema.RegisterEncodedSchema("dev.miren.runner", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x94\x94\xdfN\xeb0\f\xc6\xdf\xe4\x9c\v\x84\x80\x9bM<Q\xe5.nj\x9a8Q\x92V\xed+\xf0\x16\x88\x897\x84k\x94?lk+\xd6qS\xd9\xc9\xe7_\xfcEn\x8e\x82A\xa3\x158\xec49\xe4\x9d\xeb\x99\xd1aG,\xfc\xdbx\xb7\xdc\xd8Ǎ\x12W\xc4\x03\x05\xfcH\x88\xf1\xdfJ:Se\xe2W#\x8c\x06\xe2ՁMC\xa8\x84\x7f}\xafI\x8cO\xd7Q\xbb\x83\x02\xd2(*\b\xe9藋<L\x16E \x8d\x7f\x02\xd5\xd3\x1cTO\t\xd4\xf8\xe0\x88eB=n\xa1\x8c\xc0\xaa\x05\xdf&\x12\x9d\xd3%h\xb3'\x87\x10.͝\xf3\xb9\xb9\xfd\x06\b\xd9\x19\xa54r\xa8\x0e\xa6猳\xab\xd5\b=\x10\x87\x9b\x9a\xc3ђC\x7fj\xee\"?5w\x8c\xa0\xfb\r\x90\x82\x1a\x95\x17\x1ax\xfaL\xa8\xa6\xacD\f\xa685\xb4\x1e\xc09'\x96\x8a\xf3gy\xd9\x0f\x1b\xe5\x0e{\x0f\xb5\xca\xd5\xed)K^jc\xd4M^|\x80\xd0\xfb\xec\xa2\xc4\t\x80\xdc\xeb.~\xaa\x01T\x8f\xfe(\xcb|\x8d\xffW\xc4\\\xf73\x902\xdf\xeb\x15a\x11H\x8b,\x88\xe5\xef\xc2\"\x90\x0e\a\xd3]#\x16\x81\x1c\xd0y2,\x87gP\xb6\x05e\x1dipS\x15\xff_=3\xbe\x94v\xbe5.T\xf9\xe9\x98KozH\xbe\x01\x00\x00\xff\xff\x01\x00\x00\xff\xff;\x8d\xf7\x10\x8c\x04\x00\x00"))
}
