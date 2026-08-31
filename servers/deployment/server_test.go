package deployment

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

// newTestDeploymentServer creates a DeploymentServer with test dependencies.
// The app client and lifecycle tracker share the same in-memory entity store.
func newTestDeploymentServer(t *testing.T, logger *slog.Logger, inmem *testutils.InMemEntityServer) (*DeploymentServer, error) {
	t.Helper()
	localClient := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
	ec := aes.NewClient(logger, inmem.EAC)
	ac := appclient.NewClient(logger, localClient)
	return NewDeploymentServer(logger, inmem.EAC, ec, ac, "", nil)
}

func TestCreateDeploymentRequiresClientUpgrade(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	if err != nil {
		t.Fatalf("create deployment server: %v", err)
	}
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	_, err = client.CreateDeployment(ctx, "test-app", "test-cluster", "pending-build", nil)
	if err == nil {
		t.Fatal("expected CreateDeployment to require a client upgrade")
	}
	if !strings.Contains(err.Error(), "run 'miren upgrade'") {
		t.Fatalf("expected actionable upgrade error, got %v", err)
	}
}

func TestToDeploymentInfo(t *testing.T) {
	logger := slog.Default()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	server, _ := newTestDeploymentServer(t, logger, inmem)

	tests := []struct {
		name       string
		deployment *core_v1alpha.Deployment
		checkFunc  func(t *testing.T, info *deployment_v1alpha.DeploymentInfo)
	}{
		{
			name: "deployment with dirty git state",
			deployment: &core_v1alpha.Deployment{
				ID:         "test-deployment-1",
				AppName:    "test-app",
				AppVersion: "v1.0.0",
				ClusterId:  "test-cluster",
				Status:     "active",
				GitInfo: core_v1alpha.GitInfo{
					Sha:             "e0bdb661891c2e4f5e7e6c5c5d5c5d5c5d5c5d5c",
					Branch:          "feature-branch",
					IsDirty:         true,
					WorkingTreeHash: "dirty-hash",
					Message:         "WIP: Adding feature",
					Author:          "Test User",
				},
				DeployedBy: core_v1alpha.DeployedBy{
					UserId:    "user-123",
					UserEmail: "test@example.com",
					Timestamp: time.Now().Format(time.RFC3339),
				},
			},
			checkFunc: func(t *testing.T, info *deployment_v1alpha.DeploymentInfo) {
				if !info.HasGitInfo() {
					t.Fatal("Expected git info")
				}

				gitInfo := info.GitInfo()
				if !gitInfo.IsDirty() {
					t.Error("Expected IsDirty = true")
				}

				if gitInfo.WorkingTreeHash() != "dirty-hash" {
					t.Errorf("Expected WorkingTreeHash = dirty-hash, got %s", gitInfo.WorkingTreeHash())
				}

				// Verify all git fields are preserved
				if gitInfo.Sha() != "e0bdb661891c2e4f5e7e6c5c5d5c5d5c5d5c5d5c" {
					t.Errorf("Expected SHA = e0bdb661891c2e4f5e7e6c5c5d5c5d5c5d5c5d5c, got %s", gitInfo.Sha())
				}
				if gitInfo.Branch() != "feature-branch" {
					t.Errorf("Expected Branch = feature-branch, got %s", gitInfo.Branch())
				}
				if gitInfo.CommitMessage() != "WIP: Adding feature" {
					t.Errorf("Expected CommitMessage = 'WIP: Adding feature', got %s", gitInfo.CommitMessage())
				}
				if gitInfo.CommitAuthorName() != "Test User" {
					t.Errorf("Expected CommitAuthorName = 'Test User', got %s", gitInfo.CommitAuthorName())
				}
			},
		},
		{
			name: "deployment with clean git state",
			deployment: &core_v1alpha.Deployment{
				ID:         "test-deployment-2",
				AppName:    "test-app",
				AppVersion: "v2.0.0",
				ClusterId:  "test-cluster",
				Status:     "active",
				GitInfo: core_v1alpha.GitInfo{
					Sha:     "abc123def456",
					Branch:  "main",
					IsDirty: false,
					Message: "Release v2.0.0",
					Author:  "Release Bot",
				},
				DeployedBy: core_v1alpha.DeployedBy{
					UserId:    "bot-456",
					UserEmail: "bot@example.com",
					Timestamp: time.Now().Format(time.RFC3339),
				},
			},
			checkFunc: func(t *testing.T, info *deployment_v1alpha.DeploymentInfo) {
				if !info.HasGitInfo() {
					t.Fatal("Expected git info")
				}

				gitInfo := info.GitInfo()
				if gitInfo.IsDirty() {
					t.Error("Expected IsDirty = false")
				}

				if gitInfo.WorkingTreeHash() != "" {
					t.Errorf("Expected empty WorkingTreeHash, got %s", gitInfo.WorkingTreeHash())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := server.toDeploymentInfo(tt.deployment)
			tt.checkFunc(t, info)
		})
	}
}

func TestListDeployments(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create deployment server
	logger := slog.Default()
	server, err := newTestDeploymentServer(t, logger, inmem)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	// Create RPC client
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	// Create test deployments directly in entity store
	testDeployments := []*core_v1alpha.Deployment{
		{
			AppName:    "app1",
			ClusterId:  "cluster1",
			AppVersion: "v1.0.0",
			Status:     "active",
		},
		{
			AppName:    "app1",
			ClusterId:  "cluster1",
			AppVersion: "v2.0.0",
			Status:     "inactive",
		},
		{
			AppName:    "app2",
			ClusterId:  "cluster1",
			AppVersion: "v1.0.0",
			Status:     "active",
		},
	}

	for i, d := range testDeployments {
		deploymentName := d.AppName + "-" + d.ClusterId + "-" + d.AppVersion
		id, err := inmem.Client.Create(ctx, deploymentName, d)
		if err != nil {
			t.Fatalf("Failed to create test deployment %d: %v", i, err)
		}
		d.ID = id
	}

	tests := []struct {
		name          string
		appName       string
		clusterId     string
		status        string
		limit         int32
		expectedCount int
	}{
		{
			name:          "list all deployments",
			expectedCount: 3,
		},
		{
			name:          "filter by app name",
			appName:       "app1",
			expectedCount: 2,
		},
		{
			name:          "filter by status",
			status:        "active",
			expectedCount: 2,
		},
		{
			name:          "filter by app and status",
			appName:       "app1",
			status:        "active",
			expectedCount: 1,
		},
		{
			name:          "with limit",
			limit:         2,
			expectedCount: 2,
		},
		{
			name:          "no matching deployments",
			appName:       "nonexistent",
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := client.ListDeployments(ctx, tt.appName, tt.clusterId, tt.status, tt.limit)
			if err != nil {
				t.Fatalf("ListDeployments failed: %v", err)
			}

			if !results.HasDeployments() {
				if tt.expectedCount > 0 {
					t.Fatalf("Expected deployments, got none")
				}
				return
			}

			deployments := results.Deployments()
			if len(deployments) != tt.expectedCount {
				t.Errorf("Expected %d deployments, got %d", tt.expectedCount, len(deployments))
			}
		})
	}
}

func TestListDeploymentsResolvesShortIDsForMixedVersionRefs(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	legacyVersionID, err := inmem.Client.Create(ctx, "mixed-app-v1", &core_v1alpha.AppVersion{Version: "mixed-app-v1"})
	if err != nil {
		t.Fatalf("Failed to create legacy app version: %v", err)
	}
	canonicalVersionID, err := inmem.Client.Create(ctx, "mixed-app-v2", &core_v1alpha.AppVersion{Version: "mixed-app-v2"})
	if err != nil {
		t.Fatalf("Failed to create canonical app version: %v", err)
	}

	legacyShortID := inmem.GetEntity(legacyVersionID).ShortId()
	canonicalShortID := inmem.GetEntity(canonicalVersionID).ShortId()
	if legacyShortID == "" || canonicalShortID == "" {
		t.Fatal("Expected app versions to have short IDs")
	}

	for name, deployment := range map[string]*core_v1alpha.Deployment{
		"mixed-dep-v1": {
			AppName:    "mixed-app",
			ClusterId:  "cluster1",
			AppVersion: "mixed-app-v1",
			Status:     "succeeded",
			DeployedBy: core_v1alpha.DeployedBy{Timestamp: time.Now().Add(-time.Hour).Format(time.RFC3339)},
		},
		"mixed-dep-v2": {
			AppName:    "mixed-app",
			ClusterId:  "cluster1",
			AppVersion: string(canonicalVersionID),
			Status:     "active",
			DeployedBy: core_v1alpha.DeployedBy{Timestamp: time.Now().Format(time.RFC3339)},
		},
	} {
		if _, err := inmem.Client.Create(ctx, name, deployment); err != nil {
			t.Fatalf("Failed to create deployment %s: %v", name, err)
		}
	}

	result, err := client.ListDeployments(ctx, "mixed-app", "cluster1", "", 20)
	if err != nil {
		t.Fatalf("ListDeployments failed: %v", err)
	}

	wantShortIDs := map[string]string{
		"mixed-app-v1":             legacyShortID,
		string(canonicalVersionID): canonicalShortID,
	}
	if len(result.Deployments()) != len(wantShortIDs) {
		t.Fatalf("Expected %d deployments, got %d", len(wantShortIDs), len(result.Deployments()))
	}
	for _, deployment := range result.Deployments() {
		want, ok := wantShortIDs[deployment.AppVersionId()]
		if !ok {
			t.Fatalf("Unexpected app version %q", deployment.AppVersionId())
		}
		if !deployment.HasAppVersionShortId() || deployment.AppVersionShortId() != want {
			t.Errorf("Version %q short ID = %q, want %q", deployment.AppVersionId(), deployment.AppVersionShortId(), want)
		}
	}
}

func TestSetEnvVarsRecordsCanonicalVersionID(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	versionID, err := inmem.Client.Create(ctx, "env-app-v1", &core_v1alpha.AppVersion{Version: "env-app-v1"})
	if err != nil {
		t.Fatalf("Failed to create app version: %v", err)
	}
	if _, err := inmem.Client.Create(ctx, "env-app", &core_v1alpha.App{ActiveVersion: versionID}); err != nil {
		t.Fatalf("Failed to create app: %v", err)
	}

	envVar := &deployment_v1alpha.EnvironmentVariable{}
	envVar.SetKey("GREETING")
	envVar.SetValue("hello")
	result, err := client.SetEnvVars(ctx, "env-app", "cluster1", []*deployment_v1alpha.EnvironmentVariable{envVar}, "")
	if err != nil {
		t.Fatalf("SetEnvVars failed: %v", err)
	}
	if result.HasError() && result.Error() != "" {
		t.Fatalf("SetEnvVars returned error: %s", result.Error())
	}

	wantVersionID := "app_version/" + result.VersionId()
	if got := result.Deployment().AppVersionId(); got != wantVersionID {
		t.Errorf("Deployment app version = %q, want canonical ID %q", got, wantVersionID)
	}
	if !result.Deployment().HasAppVersionShortId() || result.Deployment().AppVersionShortId() == "" {
		t.Error("Expected env deployment to include an app-version short ID")
	}
}

func TestGetDeploymentById(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create deployment server
	logger := slog.Default()
	server, err := newTestDeploymentServer(t, logger, inmem)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	// Create RPC client
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	// Create test deployment
	testDeployment := &core_v1alpha.Deployment{
		AppName:    "test-app",
		ClusterId:  "cluster1",
		AppVersion: "v1.0.0",
		Status:     "active",
		GitInfo: core_v1alpha.GitInfo{
			Sha:             "abc123def456",
			Branch:          "main",
			IsDirty:         true,
			WorkingTreeHash: "dirty123",
			Message:         "Test commit",
			Author:          "Test User",
		},
	}

	deploymentId, err := inmem.Client.Create(ctx, "test-deployment", testDeployment)
	if err != nil {
		t.Fatalf("Failed to create test deployment: %v", err)
	}

	tests := []struct {
		name          string
		deploymentId  string
		expectError   bool
		expectedError string
		verifyFunc    func(t *testing.T, info *deployment_v1alpha.DeploymentInfo)
	}{
		{
			name:         "get existing deployment",
			deploymentId: deploymentId.String(),
			expectError:  false,
			verifyFunc: func(t *testing.T, info *deployment_v1alpha.DeploymentInfo) {
				if info.Id() != deploymentId.String() {
					t.Errorf("Expected ID %s, got %s", deploymentId.String(), info.Id())
				}
				if info.AppName() != "test-app" {
					t.Errorf("Expected app name test-app, got %s", info.AppName())
				}
				if !info.HasGitInfo() {
					t.Fatal("Expected git info")
				}
				git := info.GitInfo()
				if !git.IsDirty() {
					t.Error("Expected IsDirty = true")
				}
				if git.WorkingTreeHash() != "dirty123" {
					t.Errorf("Expected WorkingTreeHash = dirty123, got %s", git.WorkingTreeHash())
				}
			},
		},
		{
			name:          "get non-existent deployment",
			deploymentId:  "nonexistent",
			expectError:   true,
			expectedError: "not found",
		},
		{
			name:          "empty deployment id",
			deploymentId:  "",
			expectError:   true,
			expectedError: "deployment_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.GetDeploymentById(ctx, tt.deploymentId)

			if tt.expectError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}
				if !containsError(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.expectedError, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("GetDeploymentById failed: %v", err)
				}

				if !result.HasDeployment() {
					t.Fatal("Expected deployment in results")
				}

				if tt.verifyFunc != nil {
					tt.verifyFunc(t, result.Deployment())
				}
			}
		})
	}
}

func containsError(actual, expected string) bool {
	return actual == expected ||
		(expected != "" && actual != "" &&
			(actual == expected ||
				containsString(actual, expected)))
}

func containsString(str, substr string) bool {
	return len(substr) > 0 && len(str) >= len(substr) &&
		(str == substr || indexString(str, substr) >= 0)
}

func indexString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestCancelDeployment(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create deployment server
	logger := slog.Default()
	server, err := newTestDeploymentServer(t, logger, inmem)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	// Create RPC client
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	t.Run("empty deployment id returns error", func(t *testing.T) {
		result, err := client.CancelDeployment(ctx, "", "")
		if err != nil {
			t.Fatalf("Unexpected RPC error: %v", err)
		}
		if !result.HasError() || result.Error() == "" {
			t.Fatal("Expected error for empty deployment ID")
		}
		if result.Error() != "deployment_id is required" {
			t.Errorf("Expected 'deployment_id is required', got '%s'", result.Error())
		}
	})

	t.Run("non-existent deployment returns error", func(t *testing.T) {
		result, err := client.CancelDeployment(ctx, "nonexistent-deployment-id", "")
		if err != nil {
			t.Fatalf("Unexpected RPC error: %v", err)
		}
		if !result.HasError() || result.Error() == "" {
			t.Fatal("Expected error for non-existent deployment")
		}
		if result.Error() != "deployment not found" {
			t.Errorf("Expected 'deployment not found', got '%s'", result.Error())
		}
	})

	t.Run("cancel deployment not in progress returns error", func(t *testing.T) {
		// Create a completed (active) deployment
		deployment := &core_v1alpha.Deployment{
			AppName:     "test-app-completed",
			ClusterId:   "test-cluster",
			AppVersion:  "v1.0.0",
			Status:      "active",
			CompletedAt: time.Now().Format(time.RFC3339),
		}
		deploymentId, err := inmem.Client.Create(ctx, "completed-deployment", deployment)
		if err != nil {
			t.Fatalf("Failed to create test deployment: %v", err)
		}

		result, err := client.CancelDeployment(ctx, string(deploymentId), "")
		if err != nil {
			t.Fatalf("Unexpected RPC error: %v", err)
		}
		if !result.HasError() || result.Error() == "" {
			t.Fatal("Expected error for completed deployment")
		}
		if !containsString(result.Error(), "deployment is not in progress") {
			t.Errorf("Expected error containing 'deployment is not in progress', got '%s'", result.Error())
		}
	})

	t.Run("cancel in-progress deployment with no owner succeeds", func(t *testing.T) {
		// Create an in-progress deployment with no owner (unregistered cluster scenario)
		deployment := &core_v1alpha.Deployment{
			AppName:    "test-app-no-owner",
			ClusterId:  "test-cluster",
			AppVersion: "v1.0.0",
			Status:     "in_progress",
			Phase:      "building",
			DeployedBy: core_v1alpha.DeployedBy{
				// No UserId set - simulates unregistered cluster
				UserEmail: "",
				Timestamp: time.Now().Format(time.RFC3339),
			},
		}
		deploymentId, err := inmem.Client.Create(ctx, "no-owner-deployment", deployment)
		if err != nil {
			t.Fatalf("Failed to create test deployment: %v", err)
		}

		// Cancel without providing caller ID (should succeed for unregistered cluster)
		result, err := client.CancelDeployment(ctx, string(deploymentId), "")
		if err != nil {
			t.Fatalf("Unexpected RPC error: %v", err)
		}
		if result.HasError() && result.Error() != "" {
			t.Fatalf("Expected success, got error: %s", result.Error())
		}
		if !result.Success() {
			t.Error("Expected Success() to be true")
		}

		// Verify deployment is now cancelled
		getResult, err := client.GetDeploymentById(ctx, string(deploymentId))
		if err != nil {
			t.Fatalf("Failed to get deployment: %v", err)
		}
		if getResult.Deployment().Status() != "cancelled" {
			t.Errorf("Expected status 'cancelled', got '%s'", getResult.Deployment().Status())
		}
	})

	t.Run("cancel owned deployment as different user succeeds", func(t *testing.T) {
		// Any user with cluster access can cancel any deployment (permissive model)
		ownerId := "user-123"
		ownerEmail := "owner@example.com"
		differentUserId := "user-other"

		deployment := &core_v1alpha.Deployment{
			AppName:    "test-app-owned",
			ClusterId:  "test-cluster",
			AppVersion: "v1.0.0",
			Status:     "in_progress",
			Phase:      "building",
			DeployedBy: core_v1alpha.DeployedBy{
				UserId:    ownerId,
				UserEmail: ownerEmail,
				Timestamp: time.Now().Format(time.RFC3339),
			},
		}
		deploymentId, err := inmem.Client.Create(ctx, "owned-deployment", deployment)
		if err != nil {
			t.Fatalf("Failed to create test deployment: %v", err)
		}

		// Cancel as different user (should succeed - permissive model)
		result, err := client.CancelDeployment(ctx, string(deploymentId), differentUserId)
		if err != nil {
			t.Fatalf("Unexpected RPC error: %v", err)
		}
		if result.HasError() && result.Error() != "" {
			t.Fatalf("Expected success, got error: %s", result.Error())
		}
		if !result.Success() {
			t.Error("Expected Success() to be true")
		}

		// Verify deployment is now cancelled
		getResult, err := client.GetDeploymentById(ctx, string(deploymentId))
		if err != nil {
			t.Fatalf("Failed to get deployment: %v", err)
		}
		if getResult.Deployment().Status() != "cancelled" {
			t.Errorf("Expected status 'cancelled', got '%s'", getResult.Deployment().Status())
		}
	})

	t.Run("cancel sets correct fields on deployment", func(t *testing.T) {
		// Create an in-progress deployment
		deployment := &core_v1alpha.Deployment{
			AppName:    "test-app-fields",
			ClusterId:  "test-cluster",
			AppVersion: "v1.0.0",
			Status:     "in_progress",
			Phase:      "activating",
		}
		deploymentId, err := inmem.Client.Create(ctx, "fields-deployment", deployment)
		if err != nil {
			t.Fatalf("Failed to create test deployment: %v", err)
		}

		result, err := client.CancelDeployment(ctx, string(deploymentId), "")
		if err != nil {
			t.Fatalf("Unexpected RPC error: %v", err)
		}
		if result.HasError() && result.Error() != "" {
			t.Fatalf("Expected success, got error: %s", result.Error())
		}

		// Verify all fields are set correctly
		getResult, err := client.GetDeploymentById(ctx, string(deploymentId))
		if err != nil {
			t.Fatalf("Failed to get deployment: %v", err)
		}

		dep := getResult.Deployment()
		if dep.Status() != "cancelled" {
			t.Errorf("Expected status 'cancelled', got '%s'", dep.Status())
		}
		if !dep.HasCompletedAt() {
			t.Error("Expected CompletedAt to be set")
		}

		// Verify CompletedAt is recent (within last minute)
		completedAt := standard.FromTimestamp(dep.CompletedAt())
		if time.Since(completedAt) > time.Minute {
			t.Errorf("CompletedAt (%v) should be recent", completedAt)
		}
	})
}

func TestUpdateDeploymentStatusToInProgress(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create deployment server
	logger := slog.Default()
	server, err := newTestDeploymentServer(t, logger, inmem)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	appID, err := inmem.Client.Create(ctx, "test-app", &core_v1alpha.App{})
	if err != nil {
		t.Fatal(err)
	}
	versionID, err := inmem.Client.Create(ctx, "test-app-v1", &core_v1alpha.AppVersion{App: appID, Version: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	// First create a deployment directly in entity store for testing.
	testDeployment := &core_v1alpha.Deployment{
		App:        appID,
		Version:    versionID,
		Operation:  string(deploylifecycle.OperationBuild),
		Phase:      string(deploylifecycle.PhasePreparing),
		StartedAt:  time.Now(),
		AppName:    "test-app",
		ClusterId:  "test-cluster",
		AppVersion: "v1.0.0",
		Status:     "in_progress",
		DeployedBy: core_v1alpha.DeployedBy{
			UserId:    "test-user",
			UserEmail: "test@example.com",
			Timestamp: time.Now().Format(time.RFC3339),
		},
	}

	// Create entity
	deploymentName := "test-deployment"
	deploymentId, err := inmem.Client.Create(ctx, deploymentName, testDeployment)
	if err != nil {
		t.Fatalf("Failed to create test deployment: %v", err)
	}
	testDeployment.ID = deploymentId
	_, err = server.tracker.Locks().Acquire(ctx, "test-app", string(deploymentId))
	if err != nil {
		t.Fatal(err)
	}

	// Create RPC client
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	// Test 1: Update to active status
	updateResult, err := client.UpdateDeploymentStatus(ctx, string(deploymentId), "active", "")
	if err != nil {
		t.Fatalf("Failed to update deployment status to active: %v", err)
	}
	if updateResult.Deployment().Status() != "active" {
		t.Errorf("Expected status 'active', got %s", updateResult.Deployment().Status())
	}
	if !updateResult.Deployment().HasCompletedAt() {
		t.Error("CompletedAt should be set for active deployment")
	}

	// Test 2: Try to update back to in_progress (should fail - completed deployments can't go back)
	_, err = client.UpdateDeploymentStatus(ctx, string(deploymentId), "in_progress", "")
	if err == nil {
		t.Error("Expected error when updating completed deployment back to in_progress")
	}
	if !containsString(err.Error(), "cannot transition deployment from succeeded to") {
		t.Errorf("Unexpected error message: %v", err)
	}

	// Test 3: Create another deployment and verify we can keep it in_progress
	testDeployment2 := &core_v1alpha.Deployment{
		AppName:    "test-app2",
		ClusterId:  "test-cluster",
		AppVersion: "v1.0.0",
		Status:     "in_progress",
		Phase:      "building",
		DeployedBy: core_v1alpha.DeployedBy{
			UserId:    "test-user",
			UserEmail: "test@example.com",
			Timestamp: time.Now().Format(time.RFC3339),
		},
	}

	deploymentName2 := "test-deployment2"
	deploymentId2, err := inmem.Client.Create(ctx, deploymentName2, testDeployment2)
	if err != nil {
		t.Fatalf("Failed to create test deployment 2: %v", err)
	}

	// Update to in_progress (should work since it's already in_progress)
	updateResult2, err := client.UpdateDeploymentStatus(ctx, string(deploymentId2), "in_progress", "")
	if err != nil {
		t.Fatalf("Failed to update deployment status to in_progress: %v", err)
	}
	if updateResult2.Deployment().Status() != "in_progress" {
		t.Errorf("Expected status 'in_progress', got %s", updateResult2.Deployment().Status())
	}

	// Verify CompletedAt is not set when status is in_progress
	if updateResult2.Deployment().HasCompletedAt() {
		t.Error("CompletedAt should not be set for in_progress deployment")
	}
}

// TestListDeploymentsIgnoresClusterID pins the MIR-1465 fix: the same physical
// cluster ends up with deployments filed under two different client-supplied
// identity strings (a manual deploy stamps the cluster name, a CI/OIDC deploy
// stamps the raw address), and neither history nor the active-deployment lookup
// may hide rows based on which string the query is scoped to. The store is
// per-cluster, so every deployment it holds is ours.
func TestListDeploymentsIgnoresClusterID(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := slog.Default()
	server, err := newTestDeploymentServer(t, logger, inmem)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	const clusterName = "garden"
	const clusterAddr = "34.122.229.118:8443"

	// An older manual deploy stamped with the cluster name, then a newer CI
	// deploy stamped with the raw address. Both target the same cluster.
	for _, d := range []*core_v1alpha.Deployment{
		{AppName: "myapp", ClusterId: clusterName, AppVersion: "v1-manual", Status: "succeeded",
			DeployedBy: core_v1alpha.DeployedBy{Timestamp: time.Now().Add(-1 * time.Hour).Format(time.RFC3339)}},
		{AppName: "myapp", ClusterId: clusterAddr, AppVersion: "v2-ci", Status: "active",
			DeployedBy: core_v1alpha.DeployedBy{Timestamp: time.Now().Format(time.RFC3339)}},
	} {
		name := d.AppName + "-" + d.AppVersion
		if _, err := inmem.Client.Create(ctx, name, d); err != nil {
			t.Fatalf("Failed to create deployment: %v", err)
		}
	}

	// History returns every deployment for the app regardless of the cluster_id
	// the caller passes — name, address, or empty.
	for _, clusterArg := range []string{clusterName, clusterAddr, ""} {
		result, err := client.ListDeployments(ctx, "myapp", clusterArg, "", 0)
		if err != nil {
			t.Fatalf("ListDeployments(cluster_id=%q) failed: %v", clusterArg, err)
		}
		if len(result.Deployments()) != 2 {
			t.Errorf("cluster_id=%q: expected 2 deployments, got %d", clusterArg, len(result.Deployments()))
		}
	}

	// The active deployment is the CI (address-stamped) one, and it must be
	// found even when the caller scopes the query by the cluster *name* — the
	// exact query that MIR-1465 caused to return the stale manual deploy.
	active, err := client.GetActiveDeployment(ctx, "myapp", clusterName)
	if err != nil {
		t.Fatalf("GetActiveDeployment(cluster_id=%q) failed: %v", clusterName, err)
	}
	if got := active.Deployment().AppVersionId(); got != "v2-ci" {
		t.Errorf("expected active app version %q (CI deploy), got %q", "v2-ci", got)
	}
}

func TestDeployVersion(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := slog.Default()
	server, err := newTestDeploymentServer(t, logger, inmem)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	t.Run("missing app_name returns error", func(t *testing.T) {
		_, err := client.DeployVersion(ctx, "", "cluster1", "myapp-v1", false, nil, "", "")
		if err == nil {
			t.Fatal("Expected error for empty app_name")
		}
	})

	t.Run("missing cluster_id returns error", func(t *testing.T) {
		_, err := client.DeployVersion(ctx, "myapp", "", "myapp-v1", false, nil, "", "")
		if err == nil {
			t.Fatal("Expected error for empty cluster_id")
		}
	})

	t.Run("missing app_version_id returns error", func(t *testing.T) {
		_, err := client.DeployVersion(ctx, "myapp", "cluster1", "", false, nil, "", "")
		if err == nil {
			t.Fatal("Expected error for empty app_version_id")
		}
	})

	t.Run("non-existent version returns error in results", func(t *testing.T) {
		result, err := client.DeployVersion(ctx, "myapp", "cluster1", "nonexistent-version", false, nil, "", "")
		if err != nil {
			t.Fatalf("Unexpected RPC error: %v", err)
		}
		if !result.HasError() || result.Error() == "" {
			t.Fatal("Expected error for non-existent version")
		}
		if !containsString(result.Error(), "not found") {
			t.Errorf("Expected 'not found' error, got: %s", result.Error())
		}
	})

	t.Run("deploy existing version creates active deployment", func(t *testing.T) {
		// Create an app entity
		app := &core_v1alpha.App{}
		_, err := inmem.Client.Create(ctx, "testapp", app)
		if err != nil {
			t.Fatalf("Failed to create app: %v", err)
		}

		// Create an app version entity
		appVersion := &core_v1alpha.AppVersion{
			Version: "testapp-v1abc",
		}
		versionId, err := inmem.Client.Create(ctx, "testapp-v1abc", appVersion)
		if err != nil {
			t.Fatalf("Failed to create app version: %v", err)
		}

		// Create a prior deployment record for this version (to serve as source)
		priorDep := &core_v1alpha.Deployment{
			AppName:    "testapp",
			ClusterId:  "cluster1",
			AppVersion: "testapp-v1abc",
			Status:     "succeeded",
			GitInfo: core_v1alpha.GitInfo{
				Sha:    "abc123",
				Branch: "main",
			},
			DeployedBy: core_v1alpha.DeployedBy{
				Timestamp: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
		}
		priorId, err := inmem.Client.Create(ctx, "prior-dep", priorDep)
		if err != nil {
			t.Fatalf("Failed to create prior deployment: %v", err)
		}

		// Deploy the version
		result, err := client.DeployVersion(ctx, "testapp", "cluster1", "testapp-v1abc", false, nil, "", "")
		if err != nil {
			t.Fatalf("DeployVersion failed: %v", err)
		}
		if result.HasError() && result.Error() != "" {
			t.Fatalf("DeployVersion returned error: %s", result.Error())
		}

		if !result.HasDeployment() || result.Deployment() == nil {
			t.Fatal("Expected deployment in results")
		}

		dep := result.Deployment()
		if dep.Status() != "active" {
			t.Errorf("Expected status 'active', got %s", dep.Status())
		}
		if dep.AppVersionId() != string(versionId) {
			t.Errorf("Expected canonical version %q, got %q", versionId, dep.AppVersionId())
		}
		if dep.SourceDeploymentId() != string(priorId) {
			t.Errorf("Expected source_deployment_id %s, got %s", priorId, dep.SourceDeploymentId())
		}

		// Verify git info was copied from source
		if !dep.HasGitInfo() || dep.GitInfo() == nil {
			t.Fatal("Expected git info copied from source")
		}
		if dep.GitInfo().Sha() != "abc123" {
			t.Errorf("Expected git SHA 'abc123', got %s", dep.GitInfo().Sha())
		}
	})

	t.Run("rollback marks previous as rolled_back", func(t *testing.T) {
		// Create app and version
		app := &core_v1alpha.App{}
		_, err := inmem.Client.Create(ctx, "rollback-app", app)
		if err != nil {
			t.Fatalf("Failed to create app: %v", err)
		}

		appVersion := &core_v1alpha.AppVersion{Version: "rollback-app-v1"}
		version1ID, err := inmem.Client.Create(ctx, "rollback-app-v1", appVersion)
		if err != nil {
			t.Fatalf("Failed to create app version: %v", err)
		}

		appVersion2 := &core_v1alpha.AppVersion{Version: "rollback-app-v2"}
		version2ID, err := inmem.Client.Create(ctx, "rollback-app-v2", appVersion2)
		if err != nil {
			t.Fatalf("Failed to create app version v2: %v", err)
		}

		// Create an active deployment for v2 (current)
		activeDep := &core_v1alpha.Deployment{
			AppName:    "rollback-app",
			ClusterId:  "cluster1",
			AppVersion: string(version2ID),
			Status:     "active",
			DeployedBy: core_v1alpha.DeployedBy{
				Timestamp: time.Now().Format(time.RFC3339),
			},
		}
		activeDepId, err := inmem.Client.Create(ctx, "active-dep-v2", activeDep)
		if err != nil {
			t.Fatalf("Failed to create active deployment: %v", err)
		}

		// Create a succeeded deployment for v1 (source for rollback)
		_, err = inmem.Client.Create(ctx, "succeeded-dep-v1", &core_v1alpha.Deployment{
			AppName:    "rollback-app",
			ClusterId:  "cluster1",
			AppVersion: string(version1ID),
			Status:     "succeeded",
			DeployedBy: core_v1alpha.DeployedBy{
				Timestamp: time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
		})
		if err != nil {
			t.Fatalf("Failed to create succeeded deployment: %v", err)
		}

		// Roll back to v1
		result, err := client.DeployVersion(ctx, "rollback-app", "cluster1", string(version1ID), true, nil, "", "")
		if err != nil {
			t.Fatalf("DeployVersion (rollback) failed: %v", err)
		}
		if result.HasError() && result.Error() != "" {
			t.Fatalf("DeployVersion returned error: %s", result.Error())
		}

		newDep := result.Deployment()
		if newDep.Status() != "active" {
			t.Errorf("Expected new deployment status 'active', got %s", newDep.Status())
		}
		if newDep.AppVersionId() != string(version1ID) {
			t.Errorf("Expected rollback to retain canonical version %q, got %q", version1ID, newDep.AppVersionId())
		}

		// Verify the previous active deployment was marked as rolled_back
		prevResult, err := client.GetDeploymentById(ctx, string(activeDepId))
		if err != nil {
			t.Fatalf("Failed to get previous deployment: %v", err)
		}
		if prevResult.Deployment().Status() != "rolled_back" {
			t.Errorf("Expected previous deployment status 'rolled_back', got %s", prevResult.Deployment().Status())
		}
	})

	t.Run("deploy version blocked by in-progress deployment", func(t *testing.T) {
		// Create app and version
		app := &core_v1alpha.App{}
		_, err := inmem.Client.Create(ctx, "locked-app", app)
		if err != nil {
			t.Fatalf("Failed to create app: %v", err)
		}

		appVersion := &core_v1alpha.AppVersion{Version: "locked-app-v1"}
		_, err = inmem.Client.Create(ctx, "locked-app-v1", appVersion)
		if err != nil {
			t.Fatalf("Failed to create app version: %v", err)
		}

		// Establish a blocking in-progress deployment through the server-owned
		// lifecycle. A bare in_progress record no longer blocks; the lock does.
		_, err = server.tracker.Begin(ctx, deploylifecycle.BeginParams{
			AppName: "locked-app", ClusterID: "cluster1", Operation: deploylifecycle.OperationBuild,
		})
		if err != nil {
			t.Fatalf("Failed to create blocking deployment: %v", err)
		}

		// Try to deploy — should be blocked
		result, err := client.DeployVersion(ctx, "locked-app", "cluster1", "locked-app-v1", false, nil, "", "")
		if err != nil {
			t.Fatalf("Unexpected RPC error: %v", err)
		}
		if !result.HasError() || result.Error() == "" {
			t.Fatal("Expected error for blocked deployment")
		}
		if !containsString(result.Error(), "blocked") {
			t.Errorf("Expected 'blocked' in error, got: %s", result.Error())
		}
		if !result.HasLockInfo() || result.LockInfo() == nil {
			t.Error("Expected lock info in response")
		}
	})
}

func TestDeployVersionEphemeral(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := slog.Default()
	server, err := newTestDeploymentServer(t, logger, inmem)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	// Create an app entity
	app := &core_v1alpha.App{}
	_, err = inmem.Client.Create(ctx, "ephapp", app)
	if err != nil {
		t.Fatalf("Failed to create app: %v", err)
	}

	// Create an app version entity
	appVersion := &core_v1alpha.AppVersion{
		Version: "ephapp-v1abc",
	}
	_, err = inmem.Client.Create(ctx, "ephapp-v1abc", appVersion)
	if err != nil {
		t.Fatalf("Failed to create app version: %v", err)
	}

	t.Run("ephemeral deploy sets label and TTL without activating", func(t *testing.T) {
		result, err := client.DeployVersion(ctx, "ephapp", "cluster1", "ephapp-v1abc", false, nil, "feat-preview", "48h")
		if err != nil {
			t.Fatalf("DeployVersion failed: %v", err)
		}
		if result.HasError() && result.Error() != "" {
			t.Fatalf("DeployVersion returned error: %s", result.Error())
		}

		if !result.HasDeployment() || result.Deployment() == nil {
			t.Fatal("Expected deployment in results")
		}

		dep := result.Deployment()
		if dep.Status() != "active" {
			t.Errorf("Expected deployment status 'active', got %q", dep.Status())
		}

		// Verify a new ephemeral version entity was created (clone, not mutation)
		ephVersionId := dep.AppVersionId()
		if ephVersionId == "ephapp-v1abc" {
			t.Fatal("Expected a new version entity, not the original")
		}

		var ephVersion core_v1alpha.AppVersion
		if err := inmem.Client.GetById(ctx, entity.Id(ephVersionId), &ephVersion); err != nil {
			t.Fatalf("Failed to get ephemeral version entity: %v", err)
		}

		if ephVersion.EphemeralLabel != "feat-preview" {
			t.Errorf("Expected ephemeral label 'feat-preview', got %q", ephVersion.EphemeralLabel)
		}
		if ephVersion.EphemeralTtl != "48h" {
			t.Errorf("Expected ephemeral TTL '48h', got %q", ephVersion.EphemeralTtl)
		}
		if ephVersion.EphemeralExpiresAt.IsZero() {
			t.Error("Expected non-zero ephemeral expiry")
		}

		// Verify the original version was NOT mutated
		var originalVersion core_v1alpha.AppVersion
		err = inmem.Client.Get(ctx, "ephapp-v1abc", &originalVersion)
		if err != nil {
			t.Fatalf("Failed to get original version: %v", err)
		}
		if originalVersion.EphemeralLabel != "" {
			t.Errorf("Original version should not have ephemeral label, got %q", originalVersion.EphemeralLabel)
		}

		// Verify the app's active version was NOT changed
		var appCheck core_v1alpha.App
		err = inmem.Client.Get(ctx, "ephapp", &appCheck)
		if err != nil {
			t.Fatalf("Failed to get app: %v", err)
		}
		if appCheck.ActiveVersion != "" {
			t.Errorf("Expected active version to remain empty for ephemeral deploy, got %q", appCheck.ActiveVersion)
		}
	})

	t.Run("ephemeral deploy with invalid TTL returns error", func(t *testing.T) {
		// Use a separate app to avoid deployment lock conflicts
		badApp := &core_v1alpha.App{}
		_, err := inmem.Client.Create(ctx, "ephapp-bad", badApp)
		if err != nil {
			t.Fatalf("Failed to create app: %v", err)
		}

		av2 := &core_v1alpha.AppVersion{Version: "ephapp-bad-v1"}
		_, err = inmem.Client.Create(ctx, "ephapp-bad-v1", av2)
		if err != nil {
			t.Fatalf("Failed to create version: %v", err)
		}

		result, err := client.DeployVersion(ctx, "ephapp-bad", "cluster1", "ephapp-bad-v1", false, nil, "bad-ttl", "notaduration")
		if err != nil {
			t.Fatalf("DeployVersion failed: %v", err)
		}
		if !result.HasError() || result.Error() == "" {
			t.Fatal("Expected error for invalid TTL")
		}
		if !containsString(result.Error(), "invalid ephemeral TTL") {
			t.Errorf("Expected TTL error, got: %s", result.Error())
		}
	})

	t.Run("ephemeral deploy defaults TTL to 24h", func(t *testing.T) {
		// Use a separate app to avoid deployment lock conflicts
		defaultApp := &core_v1alpha.App{}
		_, err := inmem.Client.Create(ctx, "ephapp-default", defaultApp)
		if err != nil {
			t.Fatalf("Failed to create app: %v", err)
		}

		av3 := &core_v1alpha.AppVersion{Version: "ephapp-default-v1"}
		_, err = inmem.Client.Create(ctx, "ephapp-default-v1", av3)
		if err != nil {
			t.Fatalf("Failed to create version: %v", err)
		}

		result, err := client.DeployVersion(ctx, "ephapp-default", "cluster1", "ephapp-default-v1", false, nil, "default-ttl", "")
		if err != nil {
			t.Fatalf("DeployVersion failed: %v", err)
		}
		if result.HasError() && result.Error() != "" {
			t.Fatalf("DeployVersion returned error: %s", result.Error())
		}

		dep := result.Deployment()
		ephVersionId := dep.AppVersionId()

		var ephVersion core_v1alpha.AppVersion
		if err := inmem.Client.GetById(ctx, entity.Id(ephVersionId), &ephVersion); err != nil {
			t.Fatalf("Failed to get ephemeral version: %v", err)
		}
		if ephVersion.EphemeralTtl != "24h" {
			t.Errorf("Expected default TTL '24h', got %q", ephVersion.EphemeralTtl)
		}
	})
}
