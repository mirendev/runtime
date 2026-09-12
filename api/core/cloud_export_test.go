package compute

import (
	"encoding/json"
	"os"
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
		ParentDeployment: "deployment/dep-0",
		Operation:        "build",
		Outcome:          "failed",
		Phase:            "building",
		StartedAt:        started,
		CompletedAt:      started.Add(time.Minute).Format(time.RFC3339),
		ClusterId:        "payload-cluster-must-not-win",
		ErrorMessage:     "token=super-secret",
		DeployedBy: core_v1alpha.DeployedBy{
			Subject:        "user-123",
			AuthMethod:     "oidc",
			OrganizationId: "org-123",
			UserEmail:      "private@example.com",
			UserName:       "Private Person",
		},
		GitInfo: core_v1alpha.GitInfo{
			Sha:               "abc123",
			Branch:            "main",
			Repository:        "https://example.com/acme/web.git",
			Author:            "Ada",
			CommitAuthorEmail: "author@example.com",
			Message:           "Fix the startup race",
			CommitTimestamp:   started.Add(-time.Hour).Format(time.RFC3339),
			IsDirty:           true,
			WorkingTreeHash:   "12345678",
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
	require.Equal(t, "org-123", entity.MustGet(actor, core_v1alpha.DeployedByOrganizationIdId).Value.String())
	var retained core_v1alpha.Deployment
	retained.Decode(filtered)
	require.Equal(t, deployment.GitInfo, retained.GitInfo, "source details survive without an app version")
	require.Empty(t, retained.Version)
	_, ok = actor.Get(core_v1alpha.DeployedByUserEmailId)
	require.False(t, ok)

	encoded, err := json.Marshal(filtered)
	require.NoError(t, err)
	fixture, err := os.ReadFile("testdata/cloud-deployment.json")
	require.NoError(t, err)
	require.JSONEq(t, string(fixture), string(encoded), "fixture is shared with cloud's ingestion tests")
	for _, excluded := range []string{
		string(core_v1alpha.DeploymentClusterIdId),
		string(core_v1alpha.DeploymentErrorMessageId),
		string(core_v1alpha.DeployedByUserEmailId),
		string(core_v1alpha.DeployedByUserNameId),
		"super-secret",
		"private@example.com",
		"password",
	} {
		require.NotContains(t, string(encoded), excluded)
	}
}

func TestCloudExportPreservesResolvedImageDigest(t *testing.T) {
	version := &core_v1alpha.AppVersion{
		ID: "app_version/ver-1", App: "app/web", Version: "v1",
		ManifestDigest: "sha256:0123456789abcdef",
		AdminToken:     "private-admin-token",
		Manifest:       "private-manifest",
		Source:         core_v1alpha.Source{Kind: "image", Value: "example.com/acme/web:latest"},
	}
	filtered, _, err := core_v1alpha.CloudExportContract.Filter(entity.New(
		entity.Ref(entity.DBId, version.ID), version.Encode(),
		entity.Int64(entity.Revision, 42),
		entity.Time(entity.CreatedAt, time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)),
		entity.Time(entity.UpdatedAt, time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)),
	))
	require.NoError(t, err)
	var retained core_v1alpha.AppVersion
	retained.Decode(filtered)
	require.Equal(t, version.ManifestDigest, retained.ManifestDigest)
	require.Equal(t, version.Source, retained.Source)
	require.Empty(t, retained.AdminToken)
	require.Empty(t, retained.Manifest)
}
