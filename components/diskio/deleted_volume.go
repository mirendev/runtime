package diskio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
)

const (
	deletedVolumesDir = "deleted-volumes"
	metadataFilename  = "metadata.json"
	defaultRetainDays = 7
)

// DeletedVolumeMetadata stores information about a soft-deleted disk volume
// so it can be restored via the undelete command.
type DeletedVolumeMetadata struct {
	DiskID     string         `json:"disk_id"`
	DiskName   string         `json:"disk_name"`
	SizeGb     int64          `json:"size_gb"`
	Filesystem string         `json:"filesystem"`
	VolumeID   string         `json:"volume_id"`
	VolumeMode string         `json:"volume_mode"`
	CreatedBy  string         `json:"created_by,omitempty"`
	NodeID     compute.NodeId `json:"node_id"`
	DeletedAt  time.Time      `json:"deleted_at"`
}

// DeletedVolumesPath returns the path to the deleted-volumes directory.
func DeletedVolumesPath(dataPath string) string {
	return filepath.Join(dataPath, deletedVolumesDir)
}

// SaveDeletedVolumeMetadata writes metadata to a JSON file inside the given directory.
func SaveDeletedVolumeMetadata(dir string, meta *DeletedVolumeMetadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling deleted volume metadata: %w", err)
	}

	path := filepath.Join(dir, metadataFilename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing deleted volume metadata to %s: %w", path, err)
	}

	return nil
}

// LoadDeletedVolumeMetadata reads metadata from a JSON file inside the given directory.
func LoadDeletedVolumeMetadata(dir string) (*DeletedVolumeMetadata, error) {
	path := filepath.Join(dir, metadataFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading deleted volume metadata from %s: %w", path, err)
	}

	var meta DeletedVolumeMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshaling deleted volume metadata from %s: %w", path, err)
	}

	return &meta, nil
}

// DeletedVolumeEntry combines metadata with its directory path.
type DeletedVolumeEntry struct {
	Metadata *DeletedVolumeMetadata
	Path     string
}

// ListDeletedVolumes scans the deleted-volumes directory and returns all entries
// with valid metadata.
func ListDeletedVolumes(dataPath string) ([]DeletedVolumeEntry, error) {
	dir := DeletedVolumesPath(dataPath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading deleted volumes directory: %w", err)
	}

	var result []DeletedVolumeEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		entryPath := filepath.Join(dir, entry.Name())
		meta, err := LoadDeletedVolumeMetadata(entryPath)
		if err != nil {
			continue
		}

		result = append(result, DeletedVolumeEntry{
			Metadata: meta,
			Path:     entryPath,
		})
	}

	return result, nil
}
