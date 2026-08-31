package deploylifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func TestRecordCanonicalPresenceWinsOverLegacyFields(t *testing.T) {
	started := time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC)
	rec := &Record{Deployment: &core_v1alpha.Deployment{
		ID: "deployment/mixed", App: "app/web", Version: "app_version/v2",
		Outcome: "failed", StartedAt: started,
		AppName: "web", AppVersion: "app_version/v1", Status: "active", Phase: "building",
	}}

	assert.True(t, rec.Canonical())
	assert.Equal(t, StatusFailed, rec.Status())
	assert.Equal(t, "app_version/v2", rec.AppVersion())
	assert.Equal(t, PhaseBuilding, rec.Phase())
	assert.Equal(t, started, rec.StartedAt())
}

func TestCanonicalAttemptWithoutOutcomeIsInProgress(t *testing.T) {
	rec := &Record{Deployment: &core_v1alpha.Deployment{
		ID: "deployment/running", App: "app/web",
		StartedAt: time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC),
		Status:    "active", Phase: "building",
	}}

	assert.True(t, rec.Canonical())
	assert.Empty(t, rec.Deployment.Outcome)
	assert.Equal(t, StatusInProgress, rec.Status(), "absence of a terminal outcome wins over legacy serving status")
	assert.Equal(t, PhaseBuilding, rec.Phase())
}

func TestRecordDecodesLegacyServingStatusesAsSuccessfulAttempts(t *testing.T) {
	for _, legacy := range []string{"active", "succeeded", "rolled_back"} {
		rec := &Record{Deployment: &core_v1alpha.Deployment{
			ID: entity.Id("deployment/" + legacy), Status: legacy,
		}}
		assert.False(t, rec.Canonical())
		assert.Equal(t, StatusSucceeded, rec.Status())
	}
}

func TestUnknownCanonicalOutcomeRemainsTerminal(t *testing.T) {
	rec := &Record{Deployment: &core_v1alpha.Deployment{
		ID: "deployment/future", App: "app/web", Outcome: "superseded",
	}}

	assert.Equal(t, StatusInterrupted, rec.Status())
	assert.True(t, rec.Status().Terminal())
}

func TestNilRecordAccessorsReturnZeroValues(t *testing.T) {
	for _, rec := range []*Record{nil, {}} {
		assert.Empty(t, rec.ParentDeploymentID())
		assert.True(t, rec.StartedAt().IsZero())
		assert.True(t, rec.CompletedAt().IsZero())
	}
}
