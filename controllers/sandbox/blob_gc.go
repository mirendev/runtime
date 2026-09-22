package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// BlobGCResult contains information about blobs cleaned up during GC.
type BlobGCResult struct {
	DeletedBlobs  []string
	FailedBlobs   map[string]error
	TotalBlobs    int
	RetainedBlobs int
}

// ociManifest is a minimal OCI image manifest for extracting blob digests.
type ociManifest struct {
	Config ociDescriptor   `json:"config"`
	Layers []ociDescriptor `json:"layers"`
}

type ociDescriptor struct {
	Digest string `json:"digest"`
}

// RunBlobGC performs garbage collection of unreferenced registry blobs.
// It compares blob files on disk against digests referenced by non-archived
// artifacts or app versions and deletes any that are no longer needed.
func (w *ImageWatchdog) RunBlobGC(ctx context.Context) (*BlobGCResult, error) {
	result := &BlobGCResult{
		DeletedBlobs: []string{},
		FailedBlobs:  make(map[string]error),
	}

	blobsDir := filepath.Join(w.DataPath, "registry", "blobs")

	entries, err := os.ReadDir(blobsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("failed to read blobs directory: %w", err)
	}

	result.TotalBlobs = len(entries)

	referenced, err := w.collectReferencedBlobDigests(ctx)
	if err != nil {
		return result, fmt.Errorf("failed to collect referenced blob digests: %w", err)
	}

	now := time.Now()

	for _, entry := range entries {
		name := entry.Name()

		// Skip recently modified files to guard against concurrent uploads
		info, err := entry.Info()
		if err != nil {
			result.FailedBlobs[name] = err
			result.RetainedBlobs++
			continue
		}
		if now.Sub(info.ModTime()) < 1*time.Hour {
			result.RetainedBlobs++
			continue
		}

		if referenced[name] {
			result.RetainedBlobs++
			continue
		}

		// Blob is unreferenced and old enough - delete it
		blobPath := filepath.Join(blobsDir, name)
		if err := os.Remove(blobPath); err != nil {
			result.FailedBlobs[name] = err
		} else {
			result.DeletedBlobs = append(result.DeletedBlobs, name)
		}
	}

	return result, nil
}

// collectReferencedBlobDigests returns the set of blob digests referenced by
// non-archived artifacts and app versions. Artifacts with empty status (legacy)
// are treated as active to be safe.
func (w *ImageWatchdog) collectReferencedBlobDigests(ctx context.Context) (map[string]bool, error) {
	digests := make(map[string]bool)

	resp, err := w.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindArtifact))
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts: %w", err)
	}

	for _, e := range resp.Values() {
		var art core_v1alpha.Artifact
		art.Decode(e.Entity())

		// Only skip explicitly archived artifacts
		if art.Status == core_v1alpha.ARCHIVED {
			continue
		}

		if art.Manifest == "" {
			continue
		}

		var manifest ociManifest
		if err := json.Unmarshal([]byte(art.Manifest), &manifest); err != nil {
			w.Log.Warn("failed to parse artifact manifest, skipping blob cleanup for this artifact",
				"artifact", string(art.ID), "error", err)
			// Skip this artifact - its blobs may be deleted if not referenced by other artifacts.
			// This trades safety for availability: one bad manifest doesn't block all GC.
			continue
		}

		if manifest.Config.Digest != "" {
			digests[manifest.Config.Digest] = true
		}
		for _, layer := range manifest.Layers {
			if layer.Digest != "" {
				digests[layer.Digest] = true
			}
		}
	}

	versions, err := w.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindAppVersion))
	if err != nil {
		return nil, fmt.Errorf("failed to list app versions: %w", err)
	}
	for _, e := range versions.Values() {
		var version core_v1alpha.AppVersion
		version.Decode(e.Entity())
		if version.StaticArtifact != "" {
			digests[version.StaticArtifact] = true
		}
	}

	w.Log.Debug("collected referenced blob digests", "count", len(digests))
	return digests, nil
}
