//go:build !linux

package containerd

import (
	"errors"
	"os"
	"time"
)

// Survivor detection reads /proc and SO_PEERCRED, so off Linux there is
// nothing to find and no owner to check.

func findSurvivors(string, string) ([]*os.Process, error) { return nil, nil }

func stopSurvivors([]*os.Process, time.Duration) error { return nil }

func socketOwner(string) (int, error) {
	return 0, errors.ErrUnsupported
}
