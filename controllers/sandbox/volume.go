package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/config"

	compute "miren.dev/runtime/api/compute/v1alpha"
	"miren.dev/runtime/disk"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/pkg/units"
)

func (c *SandboxController) configureVolumes(ctx context.Context, sb *compute.Sandbox) error {
	for _, volume := range sb.Volume {
		switch volume.Provider {
		case "miren":
			if err := c.configureMirenVolume(ctx, sb, volume); err != nil {
				return err
			}
		case "host":
			if err := c.configureHostVolume(sb, volume); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported volume provider: %s", volume.Provider)
		}
	}

	return nil
}

func (c *SandboxController) configureHostVolume(sb *compute.Sandbox, volume compute.Volume) error {
	rawPath := c.sandboxPath(sb, "volumes", volume.Name)
	err := os.MkdirAll(filepath.Dir(rawPath), 0755)
	if err != nil {
		return fmt.Errorf("failed to create volume directory: %w", err)
	}

	path, ok := volume.Labels.Get("path")
	if !ok {
		if name, ok := volume.Labels.Get("name"); ok {
			path = filepath.Join(c.DataPath, "host-volumes", name)
			err = os.MkdirAll(path, 0755)
			if err != nil {
				return fmt.Errorf("failed to create named host volume directory: %w", err)
			}
		} else {
			return fmt.Errorf("missing path or name label for host volume")
		}
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if create, ok := volume.Labels.Get("create"); ok && create == "true" {
			if err := os.MkdirAll(path, 0755); err != nil {
				return fmt.Errorf("failed to create host path: %w", err)
			}
		} else {
			return fmt.Errorf("host path does not exist: %s", path)
		}
	}

	c.Log.Debug("creating host volume symlink", "path", path, "host-path", rawPath)

	return os.Symlink(path, rawPath)
}

func (c *SandboxController) configureMirenVolume(ctx context.Context, sb *compute.Sandbox, volume compute.Volume) error {
	name, ok := volume.Labels.Get("name")
	if !ok {
		return fmt.Errorf("missing name label for miren volume")
	}

	c.Log.Debug("creating miren volume", "name", name, "volume", volume.Name)

	dataPath := filepath.Join(c.DataPath, "miren-volumes", volume.Name, "data")
	err := os.MkdirAll(dataPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create volume directory: %w", err)
	}

	var sa lsvd.SegmentAccess

	if u, ok := volume.Labels.Get("s3"); ok {
		pu, err := url.Parse(u)
		if err != nil {
			return fmt.Errorf("failed to parse s3 url: %w", err)
		}

		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return fmt.Errorf("failed to load s3 config: %w", err)
		}

		sa, err = lsvd.NewS3Access(c.Log, pu.Host, pu.Path, cfg)
		if err != nil {
			return fmt.Errorf("failed to create s3 access: %w", err)
		}

		c.Log.Debug("using s3 for volume", "volume", volume, "bucket", pu.Host, "path", pu.Path)
	} else {
		sa = &lsvd.LocalFileAccess{Dir: dataPath, Log: c.Log}
		c.Log.Debug("using local file access for volume", "volume", volume, "path", dataPath)
	}

	size, ok := volume.Labels.Get("size")
	if !ok {
		size = "100GB"
	}

	sizeU, err := units.ParseData(size)
	if err != nil {
		return fmt.Errorf("failed to parse size: %w", err)
	}

	vi, err := sa.GetVolumeInfo(ctx, name)
	if err != nil || vi.Name == "" {
		err = sa.InitVolume(ctx, &lsvd.VolumeInfo{
			Name: name,
			Size: sizeU.Bytes(),
		})
		if err != nil {
			return fmt.Errorf("failed to create miren volume: %w", err)
		}

		c.Log.Debug("created miren volume", "name", name, "volume", volume.Name)
	}

	diskPath := c.sandboxPath(sb, "miren-volumes", volume.Name, "disk")
	err = os.MkdirAll(dataPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create volume directory: %w", err)
	}

	runner, err := disk.NewRunner(sa, diskPath, c.Log)
	if err != nil {
		return err
	}

	rawPath := c.sandboxPath(sb, "volumes", volume.Name)
	err = os.MkdirAll(filepath.Dir(rawPath), 0755)
	if err != nil {
		return fmt.Errorf("failed to create volume directory: %w", err)
	}

	c.Log.Debug("created miren volume path", "path", rawPath, "disk-path", diskPath)

	bindPath := c.sandboxPath(sb, "miren-volumes", volume.Name, "disk.sock")

	err = runner.Start(ctx, name, rawPath, bindPath)
	if err != nil {
		runner.Cleanup()
		os.RemoveAll(rawPath)
		c.Log.Error("failed to run miren volume runner", "error", err)
		return err
	}

	c.running.Add(1)
	go func() {
		defer c.running.Done()
		<-c.topCtx.Done()

		c.Log.Debug("stopping miren volume", "name", name)
		err := runner.Cleanup()
		if err != nil {
			c.Log.Error("failed to cleaning up miren volume", "error", err)
		}
		c.Log.Debug("stopped miren volume", "name", name)
	}()

	return nil
}
