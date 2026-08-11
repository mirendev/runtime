// Command e2e_disk drives the real CloudUpdatesClient against a running cloud,
// exercising both update kinds end to end: an lbd log segment and a
// universal-mode image snapshot, then a restore.
//
// Development only.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/cloudauth"
)

func main() {
	cloudURL := flag.String("cloud", "http://localhost:8099", "cloud base URL")
	keyPath := flag.String("key", "", "service account private key PEM")
	workDir := flag.String("work", "", "scratch directory")
	flag.Parse()

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	keyData, err := os.ReadFile(*keyPath)
	check(err, "read key")
	keyPair, err := cloudauth.LoadKeyPairFromPEM(string(keyData))
	check(err, "parse key")
	authClient, err := cloudauth.NewAuthClient(*cloudURL, keyPair)
	check(err, "auth client")

	token, err := authClient.Authenticate(ctx)
	check(err, "authenticate")
	fmt.Printf("✓ authenticated (token %d chars)\n", len(token))

	volumeID := createVolume(ctx, *cloudURL, token)
	fmt.Printf("✓ created volume %s\n", volumeID)

	updates := diskio.NewCloudUpdatesClient(log, *cloudURL, authClient)

	// --- lbd_log: what the accelerator-mode uploader sends ---
	const label = "400000000000000100000001"
	diskPath := filepath.Join(*workDir, "vol")
	check(os.MkdirAll(filepath.Join(diskPath, "logs"), 0755), "mkdir logs")
	segPath := filepath.Join(diskPath, "logs", "disk."+label+".log")
	segData := bytes.Repeat([]byte("lbd segment payload "), 64)
	check(os.WriteFile(segPath, segData, 0644), "write segment")

	uploader := diskio.NewCloudSegmentUploaderWithClient(log, updates, nil)
	segID, err := uploader.UploadSegment(ctx, volumeID, segPath)
	check(err, "upload log segment")
	fmt.Printf("✓ uploaded lbd_log segment %s (label %s)\n", segID, label)

	// --- loop_image: what the image watcher sends ---
	imageData := bytes.Repeat([]byte("universal mode disk image "), 4096)
	check(os.WriteFile(filepath.Join(diskPath, "disk.img"), imageData, 0644), "write image")

	state := diskio.NewState()
	state.SetVolume("volume-1", &diskio.VolumeState{
		EntityId:   "volume-1",
		VolumeId:   volumeID,
		Name:       "e2e-disk",
		DiskPath:   diskPath,
		SizeBytes:  int64(len(imageData)),
		Filesystem: "ext4",
		Mode:       storage_v1alpha.VM_UNIVERSAL,
	})

	watcher := diskio.NewImageWatcher(log, state, updates, time.Hour)
	watcher.SnapshotOnce(ctx)

	// --- list both kinds back ---
	logs, err := updates.List(ctx, volumeID, diskio.ListOptions{Kind: diskio.KindLBDLog})
	check(err, "list lbd_log")
	images, err := updates.List(ctx, volumeID, diskio.ListOptions{Kind: diskio.KindLoopImage})
	check(err, "list loop_image")
	fmt.Printf("✓ listed %d lbd_log and %d loop_image updates\n", len(logs), len(images))

	if len(logs) != 1 || len(images) != 1 {
		fatal("expected one update of each kind, got %d and %d", len(logs), len(images))
	}
	if logs[0].OrderingKey != label {
		fatal("label round-trip broke: sent %q, got %q", label, logs[0].OrderingKey)
	}

	// --- download the segment and compare bytes ---
	rc, err := updates.Download(ctx, volumeID, logs[0].UpdateID)
	check(err, "download segment")
	got, err := io.ReadAll(rc)
	rc.Close()
	check(err, "read segment")
	if !bytes.Equal(got, segData) {
		fatal("segment bytes differ: sent %d, got %d", len(segData), len(got))
	}
	fmt.Printf("✓ downloaded lbd_log segment, %d bytes match\n", len(got))

	// --- restore: delete the local image, remount, confirm it comes back ---
	imagePath := filepath.Join(diskPath, "disk.img")
	check(os.Remove(imagePath), "remove image")

	mc := diskio.NewDiskMountController(log, *workDir, compute_v1alpha.NewNodeId("e2e-node"), diskio.NewState(), nil)
	mc.SetUpdatesClient(updates)
	check(mc.RestoreImageIfMissing(ctx, &diskio.VolumeState{
		VolumeId:   volumeID,
		Name:       "e2e-disk",
		DiskPath:   diskPath,
		Filesystem: "ext4",
	}, imagePath), "restore image")

	restored, err := os.ReadFile(imagePath)
	check(err, "read restored image")
	if !bytes.Equal(restored, imageData) {
		fatal("restored image differs: original %d bytes, restored %d", len(imageData), len(restored))
	}
	fmt.Printf("✓ restored loop_image, %d bytes match the original\n", len(restored))

	fmt.Println("\nAll checks passed against", *cloudURL)
}

func createVolume(ctx context.Context, cloudURL, token string) string {
	// Volume UUIDs are globally unique, so generate one per run
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		fatal("generate uuid: %v", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])

	body, _ := json.Marshal(map[string]any{
		"name":          fmt.Sprintf("e2e-disk-%d", time.Now().UnixNano()),
		"uuid":          uuid,
		"declared_size": 1 << 30,
		"data_format":   "raw",
		"segments":      []string{},
		"block_map":     map[string]string{"format": "none", "data": ""},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", cloudURL+"/api/v1/disk/volumes", bytes.NewReader(body))
	check(err, "create volume request")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	check(err, "create volume")
	defer resp.Body.Close()

	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fatal("create volume failed with %d: %s", resp.StatusCode, string(payload))
	}

	var created struct {
		VolumeID string `json:"volume_id"`
	}
	check(json.Unmarshal(payload, &created), "decode volume")
	return created.VolumeID
}

func check(err error, what string) {
	if err != nil {
		fatal("%s: %v", what, err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "✗ "+format+"\n", args...)
	os.Exit(1)
}
