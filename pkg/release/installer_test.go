package release

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryInstaller_BackupAndRollback(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "miren")
	backupPath := binaryPath + ".old"

	// Create initial binary
	initialContent := []byte("initial version")
	if err := os.WriteFile(binaryPath, initialContent, 0755); err != nil {
		t.Fatalf("Failed to create initial binary: %v", err)
	}

	// Create installer
	opts := InstallOptions{
		InstallPath:  binaryPath,
		BackupSuffix: ".old",
	}
	installer := NewInstaller(opts)

	// Test backup
	ctx := context.Background()
	if err := installer.Backup(ctx); err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	// Verify backup exists
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("Backup file not created: %v", err)
	}

	// Verify original is removed
	if _, err := os.Stat(binaryPath); err == nil {
		t.Error("Original file should be removed after backup")
	}

	// Create new binary
	newContent := []byte("new version")
	if err := os.WriteFile(binaryPath, newContent, 0755); err != nil {
		t.Fatalf("Failed to create new binary: %v", err)
	}

	// Test rollback
	if err := installer.Rollback(ctx); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify rolled back content
	content, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("Failed to read rolled back binary: %v", err)
	}
	if string(content) != string(initialContent) {
		t.Errorf("Rollback content mismatch: got %s, want %s", content, initialContent)
	}

	// Verify backup is removed after rollback
	if _, err := os.Stat(backupPath); err == nil {
		t.Error("Backup file should be removed after rollback")
	}
}

func TestBinaryInstaller_Install(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "miren")
	newBinaryPath := filepath.Join(tmpDir, "miren.new")

	// Create new binary to install
	newContent := []byte("new binary content")
	if err := os.WriteFile(newBinaryPath, newContent, 0755); err != nil {
		t.Fatalf("Failed to create new binary: %v", err)
	}

	// Create installer
	opts := InstallOptions{
		InstallPath:  binaryPath,
		BackupSuffix: ".old",
	}
	installer := NewInstaller(opts)

	// Create downloaded artifact
	downloaded := &DownloadedArtifact{
		Artifact: Artifact{
			Type:    ArtifactTypeBase,
			Version: "test",
		},
		Path:     newBinaryPath,
		Checksum: "abc123",
		Size:     int64(len(newContent)),
	}

	// Test install
	ctx := context.Background()
	if err := installer.Install(ctx, downloaded); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify installed binary
	content, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("Failed to read installed binary: %v", err)
	}
	if string(content) != string(newContent) {
		t.Errorf("Installed content mismatch: got %s, want %s", content, newContent)
	}

	// Verify permissions
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("Failed to stat installed binary: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("Incorrect permissions: got %v, want 0755", info.Mode().Perm())
	}

	// Verify checksum file
	checksumPath := binaryPath + ".sha256"
	checksumContent, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Logf("Warning: checksum file not created (non-fatal): %v", err)
	} else if string(checksumContent) != "abc123" {
		t.Errorf("Checksum mismatch: got %s, want abc123", checksumContent)
	}
}

func TestBinaryInstaller_HasBackup(t *testing.T) {
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "miren")
	backupPath := binaryPath + ".old"

	opts := InstallOptions{
		InstallPath:  binaryPath,
		BackupSuffix: ".old",
	}
	installer := NewInstaller(opts)

	// Test no backup exists
	if installer.HasBackup() {
		t.Error("HasBackup() should return false when no backup exists")
	}

	// Create backup file
	if err := os.WriteFile(backupPath, []byte("backup"), 0755); err != nil {
		t.Fatalf("Failed to create backup file: %v", err)
	}

	// Test backup exists
	if !installer.HasBackup() {
		t.Error("HasBackup() should return true when backup exists")
	}
}
