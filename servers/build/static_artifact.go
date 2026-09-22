package build

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/saga"
)

type extractStaticIn struct {
	FinalImageURL string               `json:"final_image_url" saga:"final_image_url"`
	AppConfig     *appconfig.AppConfig `json:"app_config,omitempty" saga:"app_config,optional"`
	BuildStack    BuildStack           `json:"build_stack" saga:"build_stack"`
	SourceDir     string               `json:"source_dir" saga:"source_dir"`
	StreamID      string               `json:"stream_id" saga:"stream_id"`
}

type extractStaticOut struct {
	StaticArtifact string `json:"static_artifact,omitempty" saga:"static_artifact,optional"`
}

func extractStatic(ctx context.Context, in extractStaticIn) (extractStaticOut, error) {
	if in.AppConfig == nil || in.AppConfig.StaticDir == "" {
		return extractStaticOut{}, nil
	}

	deps := saga.Get[*buildSagaDeps](ctx)
	b := deps.builder
	status := deps.statuses.SenderFor(in.StreamID)

	tempDir := b.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return extractStaticOut{}, fmt.Errorf("creating static export directory: %w", err)
	}
	archivePath := filepath.Join(tempDir, idgen.Gen("static")+".tar")
	defer os.Remove(archivePath)

	status.SendMessage(fmt.Sprintf("Exporting static files from %s", in.AppConfig.StaticDir))
	if in.BuildStack.Stack == "static" {
		if err := exportStaticSource(in.SourceDir, in.AppConfig.StaticDir, archivePath); err != nil {
			return extractStaticOut{}, err
		}
	} else {
		if b.BuildKit == nil {
			return extractStaticOut{}, fmt.Errorf("BuildKit is required to export static_dir")
		}
		client, err := b.BuildKit.Client(ctx)
		if err != nil {
			return extractStaticOut{}, fmt.Errorf("connecting to BuildKit for static export: %w", err)
		}
		defer client.Close()

		bk := &Buildkit{Client: client, Log: b.Log, WorkloadIssuer: b.WorkloadIssuer}
		if err := bk.ExportStatic(ctx, in.FinalImageURL, in.AppConfig.StaticDir, archivePath); err != nil {
			return extractStaticOut{}, err
		}
	}

	archive, err := os.Open(archivePath)
	if err != nil {
		return extractStaticOut{}, fmt.Errorf("opening static file archive: %w", err)
	}
	defer archive.Close()

	digest, _, err := ocireg.NewBlobStore(b.DataPath).Put(archive)
	if err != nil {
		return extractStaticOut{}, fmt.Errorf("storing static file artifact: %w", err)
	}
	return extractStaticOut{StaticArtifact: digest}, nil
}

func exportStaticSource(sourceDir, staticDir, destination string) error {
	rel, err := filepath.Rel("/app", filepath.Clean(staticDir))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("static source directory %q must be within /app", staticDir)
	}
	if rel == ".miren" || strings.HasPrefix(rel, ".miren"+string(filepath.Separator)) {
		return fmt.Errorf("static source directory cannot expose .miren configuration")
	}

	root, err := filepath.EvalSymlinks(sourceDir)
	if err != nil {
		return fmt.Errorf("resolving static source root: %w", err)
	}
	selected := filepath.Join(root, rel)
	resolved, err := filepath.EvalSymlinks(selected)
	if err != nil {
		return fmt.Errorf("resolving static source directory %q: %w", staticDir, err)
	}
	if resolved != selected {
		return fmt.Errorf("static source directory %q traverses a symbolic link", staticDir)
	}
	info, err := os.Stat(selected)
	if err != nil {
		return fmt.Errorf("reading static source directory %q: %w", staticDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("static source path %q is not a directory", staticDir)
	}

	output, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("creating static source archive: %w", err)
	}
	archive := tar.NewWriter(output)
	walkErr := filepath.WalkDir(selected, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		archiveName, err := filepath.Rel(selected, name)
		if err != nil || archiveName == "." {
			return err
		}
		if archiveName == ".miren" || strings.HasPrefix(archiveName, ".miren"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(archiveName)
		if entry.IsDir() {
			header.Name += "/"
		}
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(name)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(archive, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	archiveErr := archive.Close()
	closeErr := output.Close()
	if walkErr != nil {
		return fmt.Errorf("archiving static source directory: %w", walkErr)
	}
	if archiveErr != nil {
		return fmt.Errorf("closing static source archive: %w", archiveErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing static source artifact: %w", closeErr)
	}
	return nil
}

func undoExtractStatic(context.Context, extractStaticIn, extractStaticOut) error {
	// Static artifacts are content-addressed and may be shared by another
	// version. Blob GC removes them after no AppVersion references them.
	return nil
}
