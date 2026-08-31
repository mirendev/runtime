package diskio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"miren.dev/runtime/api/storage/storage_v1alpha"
)

const stateFileName = "diskio-state.json"

// State represents the persisted state of disk volumes and mounts
type State struct {
	mu      sync.RWMutex
	Volumes map[string]*VolumeState `json:"volumes"`
	Mounts  map[string]*MountState  `json:"mounts"`

	// Path to the state file (not persisted)
	path string
}

// VolumeState represents the state of a disk volume
type VolumeState struct {
	// EntityId is the ID of the disk_volume entity
	EntityId string `json:"entity_id"`

	// VolumeId is the local volume identifier. It names the mount point, so it
	// is deliberately not the cloud's id: repointing it would relocate a live
	// mount out from under whatever is using it.
	VolumeId string `json:"volume_id"`

	// CloudVolumeId is this volume's identifier in miren.cloud, empty until it
	// has been registered there. Every cloud call keys off it, so empty means
	// "not backed up yet" rather than "fall back to the local id", which the
	// cloud would only reject.
	CloudVolumeId string `json:"cloud_volume_id,omitempty"`

	// Name is the human-readable name (from parent disk)
	Name string `json:"name,omitempty"`

	// DiskPath is the path to the volume data directory
	DiskPath string `json:"disk_path"`

	// SizeBytes is the volume size
	SizeBytes int64 `json:"size_bytes"`

	// Filesystem type (ext4, xfs, btrfs)
	Filesystem string `json:"filesystem"`

	// RemoteOnly indicates if this uses only remote storage
	RemoteOnly bool `json:"remote_only"`

	// Mode is the disk I/O mode (universal or accelerator)
	Mode storage_v1alpha.DiskVolumeVolumeMode `json:"mode,omitempty"`

	// DevicePath is the loop device backing this volume (alwaysMount modes only)
	DevicePath string `json:"device_path,omitempty"`

	// MountPath is where the volume is mounted (alwaysMount modes only)
	MountPath string `json:"mount_path,omitempty"`

	// Mounted indicates if the volume is currently mounted (alwaysMount modes only)
	Mounted bool `json:"mounted,omitempty"`
}

// MountState represents the state of a disk mount
type MountState struct {
	// EntityId is the ID of the disk_mount entity
	EntityId string `json:"entity_id"`

	// VolumeId is the ID of the disk_volume entity
	VolumeId string `json:"volume_id"`

	// CloudVolumeId is the backing volume's miren.cloud identifier, captured
	// when the mount is created. Every cloud lease call keys off it, and
	// recording it here means lease release still works after the volume's
	// VolumeState is gone (e.g. orphan cleanup). Empty for mounts persisted
	// before this field existed, or volumes never registered with the cloud.
	CloudVolumeId string `json:"cloud_volume_id,omitempty"`

	// DevicePath is the path to the loop device node
	DevicePath string `json:"device_path"`

	// MountPath is where the volume is mounted
	MountPath string `json:"mount_path"`

	// Mounted indicates if the volume is currently mounted
	Mounted bool `json:"mounted"`

	// ReadOnly indicates if the mount is read-only
	ReadOnly bool `json:"read_only"`

	// Mode is the disk I/O mode used for this mount (universal or accelerator)
	Mode storage_v1alpha.DiskVolumeVolumeMode `json:"mode,omitempty"`

	// LeaseNonce is the volume lease nonce from remote Disk API
	LeaseNonce string `json:"lease_nonce,omitempty"`
}

// NewState creates a new empty state
func NewState() *State {
	return &State{
		Volumes: make(map[string]*VolumeState),
		Mounts:  make(map[string]*MountState),
	}
}

// LoadState loads state from the data path.
func LoadState(dataPath string) (*State, error) {
	path := filepath.Join(dataPath, stateFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			state := NewState()
			state.path = path
			return state, nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	if state.Volumes == nil {
		state.Volumes = make(map[string]*VolumeState)
	}
	if state.Mounts == nil {
		state.Mounts = make(map[string]*MountState)
	}

	// Always use the new path going forward
	state.path = path

	// Backfill the cloud id onto mounts that predate MountState.CloudVolumeId.
	// Without this, a legacy mount that stays mounted across the upgrade never
	// records its cloud id, and once its VolumeState is gone (orphan cleanup)
	// the lease can no longer be released.
	//
	// The in-memory backfill is what the running process relies on; persistence
	// is best-effort. A failed rewrite must not discard the otherwise-valid
	// state we just loaded (which would drop active lease nonces from memory),
	// so we swallow the save error rather than fail the load. A later Save
	// captures the backfill, and because Save serializes volumes and mounts
	// together, no save can persist a volume deletion without also persisting
	// the mounts we just backfilled.
	changed := state.normalizeLegacyVolumeModes()
	if state.backfillMountCloudVolumeIds() {
		changed = true
	}
	if changed {
		_ = state.Save()
	}

	return &state, nil
}

// normalizeLegacyVolumeModes upgrades enum strings persisted before named
// enums switched Go-facing values from path-shaped names to semantic members.
func (s *State) normalizeLegacyVolumeModes() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for _, volume := range s.Volumes {
		normalized := storage_v1alpha.DiskVolumeVolumeMode(normalizeVolumeMode(string(volume.Mode)))
		if normalized != volume.Mode {
			volume.Mode = normalized
			changed = true
		}
	}
	for _, mount := range s.Mounts {
		normalized := storage_v1alpha.DiskVolumeVolumeMode(normalizeVolumeMode(string(mount.Mode)))
		if normalized != mount.Mode {
			mount.Mode = normalized
			changed = true
		}
	}
	return changed
}

func normalizeVolumeMode(mode string) string {
	return strings.TrimPrefix(strings.ToLower(mode), "volume_mode.")
}

// backfillMountCloudVolumeIds copies each volume's cloud id onto any mount that
// has none, keyed by the mount's local volume entity id. It never overwrites a
// cloud id already recorded on a mount. Returns true if any mount changed.
func (s *State) backfillMountCloudVolumeIds() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for _, m := range s.Mounts {
		if m.CloudVolumeId != "" {
			continue
		}
		if v := s.Volumes[m.VolumeId]; v != nil && v.CloudVolumeId != "" {
			m.CloudVolumeId = v.CloudVolumeId
			changed = true
		}
	}
	return changed
}

// Save persists the state to disk atomically.
// Callers that need to mutate and save atomically should use the
// combined methods (SetVolumeAndSave, SetMountAndSave, etc.) instead.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// saveLocked persists the state to disk. The caller must hold s.mu.
func (s *State) saveLocked() error {
	if s.path == "" {
		return fmt.Errorf("state path not set")
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temp file first
	dir := filepath.Dir(s.path)
	tempFile, err := os.CreateTemp(dir, "diskio-state-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, s.path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// SetPath sets the path for the state file
func (s *State) SetPath(dataPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = filepath.Join(dataPath, stateFileName)
}

// GetVolume returns a copy of a volume state by entity ID
func (s *State) GetVolume(entityId string) *VolumeState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := s.Volumes[entityId]
	if v == nil {
		return nil
	}
	// Return a copy to avoid data races
	copy := *v
	return &copy
}

// SetVolume sets a volume state
func (s *State) SetVolume(entityId string, volume *VolumeState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Volumes[entityId] = volume
}

// SetVolumeAndSave atomically sets a volume state and persists to disk.
func (s *State) SetVolumeAndSave(entityId string, volume *VolumeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Volumes[entityId] = volume
	return s.saveLocked()
}

// SetCloudVolumeId records a volume's miren.cloud identifier and persists it.
// It mutates the stored volume in place under the lock rather than taking a
// whole VolumeState, so it cannot clobber mount fields another path set while
// the caller was talking to the cloud.
func (s *State) SetCloudVolumeId(entityId, cloudVolumeId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	v := s.Volumes[entityId]
	if v == nil {
		return fmt.Errorf("volume %s not found in state", entityId)
	}
	v.CloudVolumeId = cloudVolumeId
	return s.saveLocked()
}

// DeleteVolume removes a volume state
func (s *State) DeleteVolume(entityId string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Volumes, entityId)
}

// DeleteVolumeAndSave atomically removes a volume state and persists to disk.
func (s *State) DeleteVolumeAndSave(entityId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Volumes, entityId)
	return s.saveLocked()
}

// GetMount returns a copy of a mount state by entity ID
func (s *State) GetMount(entityId string) *MountState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.Mounts[entityId]
	if m == nil {
		return nil
	}
	// Return a copy to avoid data races
	copy := *m
	return &copy
}

// SetMount sets a mount state
func (s *State) SetMount(entityId string, mount *MountState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Mounts[entityId] = mount
}

// SetMountAndSave atomically sets a mount state and persists to disk.
func (s *State) SetMountAndSave(entityId string, mount *MountState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Mounts[entityId] = mount
	return s.saveLocked()
}

// DeleteMount removes a mount state
func (s *State) DeleteMount(entityId string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Mounts, entityId)
}

// DeleteMountAndSave atomically removes a mount state and persists to disk.
func (s *State) DeleteMountAndSave(entityId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Mounts, entityId)
	return s.saveLocked()
}

// SetMountFromVolume atomically reads the current volume state and, if the
// volume is mounted, creates a mount entry using the volume's live device and
// mount paths. This avoids a TOCTOU race where the volume controller could
// update mount fields between a GetVolume call and a SetMount call.
// Returns the volume's DevicePath and MountPath on success, or an error if the
// volume is not found or not mounted.
func (s *State) SetMountFromVolume(volumeId string, mount *MountState) (devicePath, mountPath string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v := s.Volumes[volumeId]
	if v == nil {
		return "", "", fmt.Errorf("volume %s not found in state", volumeId)
	}
	if !v.Mounted {
		return "", "", fmt.Errorf("volume %s not mounted by volume controller", volumeId)
	}

	mount.DevicePath = v.DevicePath
	mount.MountPath = v.MountPath
	s.Mounts[mount.EntityId] = mount
	return v.DevicePath, v.MountPath, nil
}

// LeaseNonceForCloudVolume returns the lease nonce held by a mount whose
// backing volume has the given cloud volume id, or "" if none is held.
//
// The uploader is handed a volume's cloud id, but mount state keys off the
// local disk_volume entity id, so the two cannot be compared directly. This
// resolves cloud id -> volume entity id -> mount under a single read lock.
func (s *State) LeaseNonceForCloudVolume(cloudVolumeId string) string {
	if cloudVolumeId == "" {
		return ""
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Prefer the cloud id recorded directly on the mount.
	for _, m := range s.Mounts {
		if m.CloudVolumeId == cloudVolumeId && m.LeaseNonce != "" {
			return m.LeaseNonce
		}
	}

	// Fall back to resolving through volume state for mounts persisted before
	// the cloud id was recorded on the mount.
	for _, v := range s.Volumes {
		if v.CloudVolumeId != cloudVolumeId {
			continue
		}
		for _, m := range s.Mounts {
			if m.VolumeId == v.EntityId && m.LeaseNonce != "" {
				return m.LeaseNonce
			}
		}
	}
	return ""
}

// GetVolumeByVolumeId returns a copy of a volume state by volume ID
func (s *State) GetVolumeByVolumeId(volumeId string) *VolumeState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.Volumes {
		if v.VolumeId == volumeId {
			// Return a copy to avoid data races
			copy := *v
			return &copy
		}
	}
	return nil
}

// ListVolumes returns copies of all volume states
func (s *State) ListVolumes() []*VolumeState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	volumes := make([]*VolumeState, 0, len(s.Volumes))
	for _, v := range s.Volumes {
		// Return copies to avoid data races
		copy := *v
		volumes = append(volumes, &copy)
	}
	return volumes
}

// ListMounts returns copies of all mount states
func (s *State) ListMounts() []*MountState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mounts := make([]*MountState, 0, len(s.Mounts))
	for _, m := range s.Mounts {
		// Return copies to avoid data races
		copy := *m
		mounts = append(mounts, &copy)
	}
	return mounts
}
