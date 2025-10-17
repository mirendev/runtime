package deployment

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

func TestCreateDeploymentWithGitInfo(t *testing.T) {
	ctx := context.Background()
	
	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create deployment server
	logger := slog.Default()
	server, err := NewDeploymentServer(logger, inmem.EAC)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	// Create RPC client
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	tests := []struct {
		name           string
		gitInfo        *deployment_v1alpha.GitInfo
		expectedDirty  bool
		expectedHash   string
	}{
		{
			name: "clean git state",
			gitInfo: func() *deployment_v1alpha.GitInfo {
				gi := &deployment_v1alpha.GitInfo{}
				gi.SetSha("e0bdb661891c2e4f5e7e6c5c5d5c5d5c5d5c5d5c")
				gi.SetBranch("main")
				gi.SetIsDirty(false)
				gi.SetCommitMessage("Initial commit")
				gi.SetCommitAuthorName("Test User")
				return gi
			}(),
			expectedDirty: false,
			expectedHash:  "",
		},
		{
			name: "dirty git state",
			gitInfo: func() *deployment_v1alpha.GitInfo {
				gi := &deployment_v1alpha.GitInfo{}
				gi.SetSha("e0bdb661891c2e4f5e7e6c5c5d5c5d5c5d5c5d5c")
				gi.SetBranch("feature-branch")
				gi.SetIsDirty(true)
				gi.SetWorkingTreeHash("abc12345")
				gi.SetCommitMessage("Work in progress")
				gi.SetCommitAuthorName("Test User")
				return gi
			}(),
			expectedDirty: true,
			expectedHash:  "abc12345",
		},
		{
			name:          "no git info",
			gitInfo:       nil,
			expectedDirty: false,
			expectedHash:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create deployment
			results, err := client.CreateDeployment(ctx, "test-app", "test-cluster", "v1.0.0", tt.gitInfo)
			if err != nil {
				t.Fatalf("CreateDeployment failed: %v", err)
			}

			// Verify the deployment was created with correct git info
			if !results.HasDeployment() {
				t.Fatal("Expected deployment in results")
			}

			deploymentInfo := results.Deployment()
			
			if tt.gitInfo == nil {
				if deploymentInfo.HasGitInfo() {
					t.Error("Expected no git info, but got some")
				}
			} else {
				if !deploymentInfo.HasGitInfo() {
					t.Fatal("Expected git info, but got none")
				}

				gitInfo := deploymentInfo.GitInfo()
				
				// Check IsDirty flag
				if gitInfo.IsDirty() != tt.expectedDirty {
					t.Errorf("Expected IsDirty = %v, got %v", tt.expectedDirty, gitInfo.IsDirty())
				}

				// Check WorkingTreeHash
				if gitInfo.WorkingTreeHash() != tt.expectedHash {
					t.Errorf("Expected WorkingTreeHash = %s, got %s", tt.expectedHash, gitInfo.WorkingTreeHash())
				}
			}
		})
	}
}

func TestToDeploymentInfo(t *testing.T) {
	logger := slog.Default()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	
	server, _ := NewDeploymentServer(logger, inmem.EAC)

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

func TestCreateDeploymentErrorCases(t *testing.T) {
	ctx := context.Background()
	
	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create deployment server
	logger := slog.Default()
	server, err := NewDeploymentServer(logger, inmem.EAC)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	// Create RPC client
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	tests := []struct {
		name          string
		appName       string
		clusterId     string
		appVersionId  string
		expectedError string
	}{
		{
			name:          "missing app name",
			appName:       "",
			clusterId:     "test-cluster",
			appVersionId:  "v1.0.0",
			expectedError: "app_name is required",
		},
		{
			name:          "missing cluster id",
			appName:       "test-app",
			clusterId:     "",
			appVersionId:  "v1.0.0",
			expectedError: "cluster_id is required",
		},
		{
			name:          "missing app version id",
			appName:       "test-app",
			clusterId:     "test-cluster",
			appVersionId:  "",
			expectedError: "app_version_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.CreateDeployment(ctx, tt.appName, tt.clusterId, tt.appVersionId, nil)
			
			if err == nil {
				t.Fatal("Expected error but got none")
			}
			
			if !containsError(err.Error(), tt.expectedError) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.expectedError, err.Error())
			}
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
	server, err := NewDeploymentServer(logger, inmem.EAC)
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

func TestGetDeploymentById(t *testing.T) {
	ctx := context.Background()
	
	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create deployment server
	logger := slog.Default()
	server, err := NewDeploymentServer(logger, inmem.EAC)
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

func TestUpdateDeploymentStatusToInProgress(t *testing.T) {
	ctx := context.Background()
	
	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create deployment server
	logger := slog.Default()
	server, err := NewDeploymentServer(logger, inmem.EAC)
	if err != nil {
		t.Fatalf("Failed to create deployment server: %v", err)
	}

	// First create a deployment directly in entity store for testing  
	testDeployment := &core_v1alpha.Deployment{
		AppName:    "test-app",
		ClusterId:  "test-cluster",
		AppVersion: "v1.0.0",
		Status:     "in_progress",
		Phase:      "preparing",
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
	if !containsString(err.Error(), "cannot update deployment in active state") {
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