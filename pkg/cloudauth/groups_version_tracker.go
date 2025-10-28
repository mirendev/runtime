package cloudauth

import (
	"sync"
	"time"
)

// GroupsVersionTracker tracks the groups version for authenticated users
// to detect when permissions have changed and force token refresh
type GroupsVersionTracker struct {
	mu       sync.RWMutex
	versions map[string]*groupVersionEntry // userID -> version info
}

type groupVersionEntry struct {
	groupsVersion string
	lastChecked   time.Time
}

// NewGroupsVersionTracker creates a new groups version tracker
func NewGroupsVersionTracker() *GroupsVersionTracker {
	return &GroupsVersionTracker{
		versions: make(map[string]*groupVersionEntry),
	}
}

// CheckAndUpdate checks if the groups version has changed for a user
// Returns true if the version is different (or new), false if unchanged
func (t *GroupsVersionTracker) CheckAndUpdate(userID, groupsVersion string) bool {
	if userID == "" || groupsVersion == "" {
		return false // Can't track without IDs
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	entry, exists := t.versions[userID]
	if !exists {
		// First time seeing this user, store the version
		t.versions[userID] = &groupVersionEntry{
			groupsVersion: groupsVersion,
			lastChecked:   time.Now(),
		}
		return false // Don't force refresh on first auth
	}

	// Check if version changed
	if entry.groupsVersion != groupsVersion {
		// Version changed, update and return true to force refresh
		entry.groupsVersion = groupsVersion
		entry.lastChecked = time.Now()
		return true
	}

	// Version unchanged
	entry.lastChecked = time.Now()
	return false
}

// Clean removes entries older than the specified duration
func (t *GroupsVersionTracker) Clean(maxAge time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for userID, entry := range t.versions {
		if now.Sub(entry.lastChecked) > maxAge {
			delete(t.versions, userID)
		}
	}
}