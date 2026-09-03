package compute

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

func TestCloudExportFiltersDeploymentCustodyFields(t *testing.T) {
	started := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	deployment := &core_v1alpha.Deployment{
		ID:               "deployment/dep-1",
		App:              "app/web",
		AppName:          "web",
		Version:          "app_version/ver-1",
		ParentDeployment: "deployment/dep-0",
		Operation:        "build",
		Outcome:          "failed",
		Phase:            "building",
		StartedAt:        started,
		CompletedAt:      started.Add(time.Minute).Format(time.RFC3339),
		ClusterId:        "payload-cluster-must-not-win",
		ErrorMessage:     "token=super-secret",
		DeployedBy: core_v1alpha.DeployedBy{
			Subject:    "user-123",
			AuthMethod: "oidc",
			UserEmail:  "private@example.com",
			UserName:   "Private Person",
		},
		GitInfo: core_v1alpha.GitInfo{
			Repository:        "https://user:password@example.com/private.git?token=secret",
			CommitAuthorEmail: "author@example.com",
		},
	}
	source := entity.New(
		deployment.Encode,
		entity.Ref(entity.DBId, deployment.ID),
		entity.String(entity.DBShortId, "dep-1"),
		entity.Int64(entity.Revision, 42),
		entity.Time(entity.CreatedAt, started),
		entity.Time(entity.UpdatedAt, started.Add(time.Minute)),
	)

	marker, ok := source.Get(core_v1alpha.CloudExportContract.MarkerID())
	require.True(t, ok)
	require.True(t, marker.Value.Bool())

	filtered, _, err := core_v1alpha.CloudExportContract.Filter(source)
	require.NoError(t, err)
	require.Equal(t, "web", entity.MustGet(filtered, core_v1alpha.DeploymentAppNameId).Value.String())
	require.Equal(t, "dep-1", entity.MustGet(filtered, entity.DBShortId).Value.String())
	actor := entity.MustGet(filtered, core_v1alpha.DeploymentDeployedById).Value.Component()
	require.Equal(t, "user-123", entity.MustGet(actor, core_v1alpha.DeployedBySubjectId).Value.String())
	_, ok = actor.Get(core_v1alpha.DeployedByUserEmailId)
	require.False(t, ok)

	encoded, err := json.Marshal(filtered)
	require.NoError(t, err)
	for _, excluded := range []string{
		string(core_v1alpha.DeploymentClusterIdId),
		string(core_v1alpha.DeploymentErrorMessageId),
		string(core_v1alpha.DeploymentGitInfoId),
		string(core_v1alpha.DeployedByUserEmailId),
		string(core_v1alpha.DeployedByUserNameId),
		"super-secret",
		"private@example.com",
		"password",
	} {
		require.NotContains(t, string(encoded), excluded)
	}
}
