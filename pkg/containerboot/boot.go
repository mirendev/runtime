// Package containerboot decides which miren binary a container install
// runs. The image's own binary never changes while the container exists,
// which makes it the right place to decide and the wrong thing to serve
// from: upgrades land in the data volume's release directory, and that is
// what has to run. The image binary stays as the known-good fallback.
package containerboot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// DefaultReleaseDir is where the image bakes the release bundle and where
// the data volume, mounted over it, keeps it from then on.
const DefaultReleaseDir = "/var/lib/miren/release"

// imageMarker records the digest of the image binary that last seeded the
// release directory. It is how a boot tells "the volume was upgraded in
// place" (marker matches, binary differs) from "someone installed a new
// image over this volume" (marker differs) without comparing versions,
// which dev builds do not carry.
const imageMarker = ".image-sha256"

type Boot struct {
	ReleaseDir string
	// ImageBinary is the miren shipped in the image, normally the one
	// running this code.
	ImageBinary string
	Log         *slog.Logger
}

// Prepare makes the release directory ready to boot from and returns the
// binary to exec. The volume's binary is the one to run except when the
// image changed underneath it: docker seeds a volume once, on creation, so a
// `container install` with a newer (or older) image against an existing
// volume would otherwise keep running whatever the volume had. A changed
// image reseeds the binary, as the install used to do implicitly by running
// the image's copy.
func (b Boot) Prepare(ctx context.Context) (string, error) {
	imageSum, err := fileSHA256(b.ImageBinary)
	if err != nil {
		return "", fmt.Errorf("digest image binary: %w", err)
	}
	releaseBin := filepath.Join(b.ReleaseDir, "miren")
	seeded, err := os.ReadFile(filepath.Join(b.ReleaseDir, imageMarker))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read image marker: %w", err)
	}
	seededSum := strings.TrimSpace(string(seeded))

	_, statErr := os.Stat(releaseBin)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		b.Log.Warn("release binary missing from the data volume; seeding it from the image", "path", releaseBin)
	case statErr != nil:
		return "", fmt.Errorf("stat release binary: %w", statErr)
	case seededSum == imageSum:
		return releaseBin, nil
	case seededSum == "":
		b.Log.Info("recording which image seeded the release directory", "dir", b.ReleaseDir)
	default:
		b.Log.Info("image changed since the release directory was seeded; installing the image's binary", "dir", b.ReleaseDir)
	}

	if err := b.seed(releaseBin, imageSum); err != nil {
		return "", err
	}
	return releaseBin, nil
}

// seed copies the image binary over the release one. A first boot on a fresh
// volume copies the image's binary over an identical file, which is cheap
// and keeps one code path.
func (b Boot) seed(releaseBin, imageSum string) error {
	if err := os.MkdirAll(b.ReleaseDir, 0755); err != nil {
		return fmt.Errorf("create release dir: %w", err)
	}
	if err := copyAtomic(b.ImageBinary, releaseBin, 0755); err != nil {
		return fmt.Errorf("seed release binary: %w", err)
	}
	if err := writeAtomic(filepath.Join(b.ReleaseDir, imageMarker), []byte(imageSum+"\n"), 0644); err != nil {
		return fmt.Errorf("write image marker: %w", err)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyAtomic(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return stageAndRename(dst, mode, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	})
}

func writeAtomic(dst string, data []byte, mode os.FileMode) error {
	return stageAndRename(dst, mode, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// stageAndRename writes into a temp file beside dst and renames it over, so
// a boot interrupted midway leaves the old file, not half of the new one.
func stageAndRename(dst string, mode os.FileMode, fill func(io.Writer) error) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dst)+"-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := fill(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
