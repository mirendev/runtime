package runner_v1alpha

import (
	"time"

	entity "miren.dev/runtime/pkg/entity"
	schema "miren.dev/runtime/pkg/entity/schema"
	types "miren.dev/runtime/pkg/entity/types"
)

type InviteStatus string

const (
	InviteStatusPending InviteStatus = "pending"
	InviteStatusClaimed InviteStatus = "claimed"
	InviteStatusRevoked InviteStatus = "revoked"
	InviteStatusExpired InviteStatus = "expired"
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
	ID              entity.Id    `json:"id"`
	ClaimedAt       time.Time    `cbor:"claimed_at,omitempty" json:"claimed_at"`
	ClaimedBy       string       `cbor:"claimed_by,omitempty" json:"claimed_by,omitempty"`
	CodeHash        string       `cbor:"code_hash,omitempty" json:"code_hash,omitempty"`
	CreatedAt       time.Time    `cbor:"created_at,omitempty" json:"created_at"`
	EnrollmentCount int64        `cbor:"enrollment_count,omitempty" json:"enrollment_count,omitempty"`
	ExpiresAt       time.Time    `cbor:"expires_at,omitempty" json:"expires_at"`
	Labels          types.Labels `cbor:"labels,omitempty" json:"labels,omitempty"`
	Name            string       `cbor:"name,omitempty" json:"name,omitempty"`
	Reusable        bool         `cbor:"reusable,omitempty" json:"reusable,omitempty"`
	Status          InviteStatus `cbor:"status,omitempty" json:"status,omitempty"`
}

type RunnerInviteStatus = InviteStatus

const (
	PENDING InviteStatus = InviteStatusPending
	CLAIMED InviteStatus = InviteStatusClaimed
	REVOKED InviteStatus = InviteStatusRevoked
	EXPIRED InviteStatus = InviteStatusExpired
)

var RunnerInviteStatusFromId = map[entity.Id]InviteStatus{RunnerInviteStatusPendingId: InviteStatusPending, RunnerInviteStatusClaimedId: InviteStatusClaimed, RunnerInviteStatusRevokedId: InviteStatusRevoked, RunnerInviteStatusExpiredId: InviteStatusExpired}
var RunnerInviteStatusToId = map[InviteStatus]entity.Id{InviteStatusPending: RunnerInviteStatusPendingId, InviteStatusClaimed: RunnerInviteStatusClaimedId, InviteStatusRevoked: RunnerInviteStatusRevokedId, InviteStatusExpired: RunnerInviteStatusExpiredId}

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
		o.Status = RunnerInviteStatusFromId[a.Value.Id()]
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
	if a, ok := RunnerInviteStatusToId[o.Status]; ok {
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
	schema.RegisterEncodedSchema("dev.miren.runner", "v1alpha", []byte("\x1f\x8b\b\x00\x00\x00\x00\x00\x00\xff\x94\x94\xc1\x92\x9c \x10\x86\xdf$9\xa4RI.N\xe5\x89(\x94\x16;Bc5h\xe9=\xa7\xbcE*S\xd9'\xdc=o\x8183\xea\xee8{\xb1h\xfc\xf8\xf8[)Ί\xa4\x85N\xc1PXd\xa0\x82{\"`h\x91\x94\xff;~پ8\xc5\x17y,\x90\x06\f\xf0?)\xc6O;tE\xcdƗZ9+\x91v\x1b\xd65\x82Q\xfeϿ\x12\xd5\xf8㾪\xa8\x8cD\vJȐ\xb6\xfeuS\x87\xa9\x03\x15\xd0\u0087D\xe5\xb4\x16\x95S\x12\xd5>0\x92N\xaa\xefG*\xa7@4\xd27Ʉ\xd7r+:\xcc\xc4 \xc3ms\xd7z\xdd\xdc\xe9@\x04\xc4\xce\x18\v\x14D\xe5z\x9au\xddn6J+\xa4\xf0P8\x18;d\xf0\x97p7\xf5%\xdc9\x8a\xbe\x1e\x88\x8c,\xc1xe%M\xcfIU癨\x814N\x81\xf6\ap\xed\x89K\xd5\xf5\xb1\xfd\xd8\xdf\x0e\x963\xf4^\x96f^\xdd\\\xaa\xd4K\xe9\x9cyz\xa4\x17\x1fd\xe8\xbd\x02\xea\xed\x1by\xe3t1\x93\"\x93\xa9\xdf<N[E\xa6\x8d\x0f1HӃ?\xeb|\x12\xc7\xcf;\u07fcn9\xbaz\xfe\x03w\xc0\f\xe8\x0eH!\xe9\xf7\xc1\fh\x86\xc1\xb5\xf7\x8c\x190)\xb0\x05[\x02\xfbߋ\x7fI\xbeh\x96\x806\xd1@\x95\x8bT\xc5P\xeb\x01أ#=\xfc\x94\xa6k\xa4\xe9\x18\xad\xe4Iě®>\xf1\x16m}\xe38\x88\xf9\x92Z\xa3\x0f]Y\xaf\x00\x00\x00\xff\xff\x01\x00\x00\xff\xff\b\x8b\x83G\xf6\x04\x00\x00"))
}
