package release

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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

	// The binary stays in place: a backup must never leave the path empty
	// while the server (and its shims) may exec it.
	if content, err := os.ReadFile(binaryPath); err != nil || string(content) != string(initialContent) {
		t.Errorf("Original should still be in place after backup: %s, %v", content, err)
	}

	// Install a new binary the way the installer does, by renaming over the
	// path. The backup is a hard link, so writing the path in place would
	// write the backup too; the installer never does that.
	newContent := []byte("new version")
	staged := filepath.Join(tmpDir, "staged")
	if err := os.WriteFile(staged, newContent, 0755); err != nil {
		t.Fatalf("Failed to create new binary: %v", err)
	}
	if err := os.Rename(staged, binaryPath); err != nil {
		t.Fatal(err)
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

// previous is where a bundled file's backup lands.
func previous(dir, name string) string {
	return filepath.Join(dir, bundleBackupDir, name)
}

func stageBundle(t *testing.T, files map[string]string) *DownloadedArtifact {
	t.Helper()
	stage := filepath.Join(t.TempDir(), "stage")
	downloaded := &DownloadedArtifact{
		Artifact:  Artifact{Type: ArtifactTypeBase, Version: "test"},
		Path:      filepath.Join(stage, "miren"),
		BundleDir: stage,
		Checksum:  "abc123",
	}
	for name, content := range files {
		writeFile(t, filepath.Join(stage, name), content)
		if name != "miren" {
			downloaded.Bundled = append(downloaded.Bundled, name)
		}
	}
	return downloaded
}

func TestBinaryInstaller_InstallsAndRollsBackBundle(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"miren", "containerd", "runc"} {
		writeFile(t, filepath.Join(dir, name), "old "+name)
	}
	// A host whose release dir predates some bundled file gets it added, and
	// rollback leaves it alone since there is nothing older to restore.
	installer := NewInstaller(InstallOptions{InstallPath: filepath.Join(dir, "miren"), BackupSuffix: ".old"})
	downloaded := stageBundle(t, map[string]string{
		"miren": "new miren", "containerd": "new containerd", "runc": "new runc", "ctr": "new ctr",
	})

	ctx := context.Background()
	if err := installer.Install(ctx, downloaded); err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, name := range []string{"miren", "containerd", "runc", "ctr"} {
		if got := readFile(t, filepath.Join(dir, name)); got != "new "+name {
			t.Errorf("%s = %q, want new", name, got)
		}
	}
	if got := readFile(t, filepath.Join(dir, "miren.old")); got != "old miren" {
		t.Errorf("miren.old = %q, want old", got)
	}
	for _, name := range []string{"containerd", "runc"} {
		if got := readFile(t, previous(dir, name)); got != "old "+name {
			t.Errorf(".previous/%s = %q, want old", name, got)
		}
	}
	if _, err := os.Stat(previous(dir, "ctr")); err == nil {
		t.Error(".previous/ctr should not exist: there was no previous ctr")
	}
	if _, err := os.Stat(downloaded.BundleDir); err == nil {
		t.Error("staging dir should be removed once everything moved out")
	}
	if !installer.HasBackup() {
		t.Fatal("HasBackup() should be true after install")
	}

	if err := installer.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	for _, name := range []string{"miren", "containerd", "runc"} {
		if got := readFile(t, filepath.Join(dir, name)); got != "old "+name {
			t.Errorf("after rollback %s = %q, want old", name, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "miren.old")); err == nil {
		t.Error("miren.old should be consumed by rollback")
	}
	if _, err := os.Stat(filepath.Join(dir, bundleBackupDir)); err == nil {
		t.Error(".previous should be consumed by rollback")
	}
	if got := readFile(t, filepath.Join(dir, "ctr")); got != "new ctr" {
		t.Errorf("ctr = %q, want the added file left in place", got)
	}
	if installer.HasBackup() {
		t.Error("HasBackup() should be false after rollback")
	}
}

func TestBinaryInstaller_ClearsItsOwnStaleBackups(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "miren"), "old miren")
	writeFile(t, filepath.Join(dir, "containerd"), "old containerd")
	installer := NewInstaller(InstallOptions{InstallPath: filepath.Join(dir, "miren"), BackupSuffix: ".old"})

	// A bundle install leaves .previous/containerd behind. A miren-only upgrade
	// after it must clear that: restoring it on the later rollback would
	// pair the previous miren with a containerd two upgrades back.
	if err := installer.Install(context.Background(), stageBundle(t, map[string]string{
		"miren": "mid miren", "containerd": "mid containerd",
	})); err != nil {
		t.Fatalf("bundle Install: %v", err)
	}
	if got := readFile(t, previous(dir, "containerd")); got != "old containerd" {
		t.Fatalf(".previous/containerd = %q, want old", got)
	}
	// Not this installer's: a stray backup and a dotfile another tool keeps.
	writeFile(t, filepath.Join(dir, "other.old"), "someone else's")
	writeFile(t, filepath.Join(dir, ".image-commit"), "abc")

	if err := installer.Install(context.Background(), stageBundle(t, map[string]string{"miren": "new miren"})); err != nil {
		t.Fatalf("miren-only Install: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, bundleBackupDir)); err == nil {
		t.Error("stale .previous should be cleared by the next install")
	}
	if got := readFile(t, filepath.Join(dir, "containerd")); got != "mid containerd" {
		t.Errorf("containerd = %q, want untouched", got)
	}
	for _, name := range []string{"other.old", ".image-commit"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s should be untouched: %v", name, err)
		}
	}

	// And the rollback of the miren-only install restores only miren.
	if err := installer.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "miren")); got != "mid miren" {
		t.Errorf("miren = %q, want mid", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "other")); err == nil {
		t.Error("other.old is not ours and must not be restored")
	}
}

func TestBinaryInstaller_RollbackResumesAfterPartialRestore(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"miren", "containerd", "runc"} {
		writeFile(t, filepath.Join(dir, name), "old "+name)
	}
	installer := NewInstaller(InstallOptions{InstallPath: filepath.Join(dir, "miren"), BackupSuffix: ".old"})
	if err := installer.Install(context.Background(), stageBundle(t, map[string]string{
		"miren": "new miren", "containerd": "new containerd", "runc": "new runc",
	})); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// A rollback that died after putting containerd back.
	if err := os.Rename(previous(dir, "containerd"), filepath.Join(dir, "containerd")); err != nil {
		t.Fatal(err)
	}

	if err := installer.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	for _, name := range []string{"miren", "containerd", "runc"} {
		if got := readFile(t, filepath.Join(dir, name)); got != "old "+name {
			t.Errorf("%s = %q, want old", name, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, bundleBackupDir)); err == nil {
		t.Error("rollback consumes the backup directory")
	}
}

func TestBinaryInstaller_UndoesPartialInstall(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "miren"), "old miren")
	writeFile(t, filepath.Join(dir, "containerd"), "old containerd")

	installer := NewInstaller(InstallOptions{InstallPath: filepath.Join(dir, "miren"), BackupSuffix: ".old"})
	downloaded := stageBundle(t, map[string]string{
		"miren": "new miren", "containerd": "new containerd", "ctr": "new ctr",
	})
	// Bundled order is containerd, ctr, then a file that is not there.
	downloaded.Bundled = append(downloaded.Bundled, "runc")
	slices.Sort(downloaded.Bundled)

	err := installer.Install(context.Background(), downloaded)
	if err == nil {
		t.Fatal("Install should fail on the missing staged file")
	}
	if got := readFile(t, filepath.Join(dir, "miren")); got != "old miren" {
		t.Errorf("miren = %q, want untouched (miren installs last)", got)
	}
	if got := readFile(t, filepath.Join(dir, "containerd")); got != "old containerd" {
		t.Errorf("containerd = %q, want restored", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "ctr")); err == nil {
		t.Error("ctr was new and should be removed by undo")
	}
	if installer.HasBackup() {
		t.Error("a failed install must not leave a backup for rollback to find")
	}
	if _, err := os.Stat(filepath.Join(dir, bundleBackupDir)); err == nil {
		t.Error("a failed install must not leave a backup directory")
	}
}

func TestBinaryInstaller_ResumedInstallKeepsOriginalBackups(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"miren", "containerd", "runc"} {
		writeFile(t, filepath.Join(dir, name), "old "+name)
	}
	installer := NewInstaller(InstallOptions{InstallPath: filepath.Join(dir, "miren"), BackupSuffix: ".old"})

	// An install that died after the bundle, before miren:
	// the bundle is new, its backups are the originals, miren is untouched.
	if err := os.MkdirAll(filepath.Join(dir, bundleBackupDir), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"containerd", "runc"} {
		if err := os.Rename(filepath.Join(dir, name), previous(dir, name)); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, name), "new "+name)
	}
	if !installer.HasBackup() {
		t.Fatal("an interrupted install has a bundle to roll back")
	}

	// The executor downloads and installs again.
	if err := installer.Install(context.Background(), stageBundle(t, map[string]string{
		"miren": "new miren", "containerd": "new containerd", "runc": "new runc",
	})); err != nil {
		t.Fatalf("resumed Install: %v", err)
	}
	for _, name := range []string{"containerd", "runc"} {
		if got := readFile(t, previous(dir, name)); got != "old "+name {
			t.Errorf(".previous/%s = %q, want the original kept", name, got)
		}
	}
	if got := readFile(t, filepath.Join(dir, "miren.old")); got != "old miren" {
		t.Errorf("miren.old = %q, want old", got)
	}

	if err := installer.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	for _, name := range []string{"miren", "containerd", "runc"} {
		if got := readFile(t, filepath.Join(dir, name)); got != "old "+name {
			t.Errorf("after rollback %s = %q, want old", name, got)
		}
	}
}

func TestBinaryInstaller_RollbackOfInterruptedInstallRestoresBundle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "miren"), "old miren")
	writeFile(t, filepath.Join(dir, "runc"), "new runc")
	writeFile(t, previous(dir, "runc"), "old runc")
	installer := NewInstaller(InstallOptions{InstallPath: filepath.Join(dir, "miren"), BackupSuffix: ".old"})

	if err := installer.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "runc")); got != "old runc" {
		t.Errorf("runc = %q, want old", got)
	}
	if got := readFile(t, filepath.Join(dir, "miren")); got != "old miren" {
		t.Errorf("miren = %q, want untouched", got)
	}
	if installer.HasBackup() {
		t.Error("nothing left to roll back")
	}
}

func TestBinaryInstaller_SwapKeepsPathResolvable(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "runc"), "old runc")
	installer := NewInstaller(InstallOptions{InstallPath: filepath.Join(dir, "miren"), BackupSuffix: ".old"}).(*binaryInstaller)
	// Stage on what is, in production, usually another filesystem.
	src := filepath.Join(t.TempDir(), "runc")
	writeFile(t, src, "new runc")

	if err := installer.installFile(src, filepath.Join(dir, "runc"), previous(dir, "runc"), false); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "runc")); got != "new runc" {
		t.Errorf("runc = %q", got)
	}
	if got := readFile(t, previous(dir, "runc")); got != "old runc" {
		t.Errorf(".previous/runc = %q", got)
	}
	// Nothing staged is left lying around.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "runc" && e.Name() != bundleBackupDir {
			t.Errorf("unexpected file %s", e.Name())
		}
	}
}

func TestBinaryInstaller_EmptyBackupTreeIsNoBackup(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "miren"), "miren")
	// A rollback that consumed .previous/bin/ctr and died before removing
	// the tree leaves an empty directory behind.
	if err := os.MkdirAll(previous(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	installer := NewInstaller(InstallOptions{InstallPath: filepath.Join(dir, "miren"), BackupSuffix: ".old"})

	if installer.HasBackup() {
		t.Error("an empty backup tree is not a backup")
	}
	if err := installer.Rollback(context.Background()); err == nil {
		t.Error("Rollback should refuse when there is nothing to restore")
	}

	// And an install after it is a fresh one, not a resume: the empty tree
	// is swept and miren is backed up as usual.
	if err := installer.Install(context.Background(), stageBundle(t, map[string]string{"miren": "new miren"})); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "miren.old")); got != "miren" {
		t.Errorf("miren.old = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, bundleBackupDir)); err == nil {
		t.Error("the empty tree should be swept by a fresh install")
	}
}

func TestBinaryInstaller_UndoRemovesNestedBackupDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "miren"), "old miren")
	writeFile(t, filepath.Join(dir, "bin", "ctr"), "old ctr")
	installer := NewInstaller(InstallOptions{InstallPath: filepath.Join(dir, "miren"), BackupSuffix: ".old"})
	downloaded := stageBundle(t, map[string]string{"miren": "new miren", "bin/ctr": "new ctr"})
	downloaded.Path = filepath.Join(t.TempDir(), "missing")

	if err := installer.Install(context.Background(), downloaded); err == nil {
		t.Fatal("Install should fail on the missing miren")
	}
	if got := readFile(t, filepath.Join(dir, "bin", "ctr")); got != "old ctr" {
		t.Errorf("bin/ctr = %q, want restored", got)
	}
	if _, err := os.Stat(filepath.Join(dir, bundleBackupDir)); err == nil {
		t.Error("undo should remove the backup tree, nested directories included")
	}
	if installer.HasBackup() {
		t.Error("a failed install leaves nothing to roll back")
	}
}
