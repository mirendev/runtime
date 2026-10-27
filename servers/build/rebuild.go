package build

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
)

// Source archives are private server data, not registry blobs: an uploaded
// build context can contain files that must never be served as image layers.
func (b *Builder) retainSource(dir string) (string, error) {
	if b.DataPath == "" {
		return "", nil
	}
	root := filepath.Join(b.DataPath, "build-sources")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(root, ".source-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	hash := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(f, hash))
	tw := tar.NewWriter(gz)
	if err := tw.AddFS(os.DirFS(dir)); err != nil {
		return "", err
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if err := os.Rename(f.Name(), filepath.Join(root, digest+".tar.gz")); err != nil {
		return "", err
	}
	return digest, nil
}

func (b *Builder) openSource(digest string) (*os.File, error) {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || b.DataPath == "" {
		return nil, fmt.Errorf("invalid or unavailable retained source archive")
	}
	return os.Open(filepath.Join(b.DataPath, "build-sources", digest+".tar.gz"))
}

func (s *SagaBuilder) Rebuild(ctx context.Context, state *build_v1alpha.BuilderRebuild) (retErr error) {
	args := state.Args()
	name := args.Application()
	if !rpc.AllowApp(ctx, name) {
		return rpc.AppAccessError(ctx, name)
	}
	if err := s.inner.authorizeBuild(ctx, name); err != nil {
		return err
	}
	if err := validateDeploymentMessage("", args.Deployment()); err != nil {
		return err
	}
	var app core_v1alpha.App
	if err := s.inner.ec.Get(ctx, name, &app); err != nil {
		return err
	}
	if app.ActiveVersion == "" {
		return fmt.Errorf("app %q has no deployed version to rebuild", name)
	}
	var version core_v1alpha.AppVersion
	if err := s.inner.ec.GetById(ctx, app.ActiveVersion, &version); err != nil {
		return err
	}
	if version.Source.Kind != "" && version.Source.Kind != "stack" && version.Source.Kind != "dockerfile" {
		return fmt.Errorf("app %q is not built from stackbuild or a Dockerfile; deploy it instead", name)
	}
	if version.SourceArchive == "" {
		return fmt.Errorf("app %q has no retained source; deploy it once with an updated server before rebuilding", name)
	}
	source, err := s.inner.openSource(version.SourceArchive)
	if err != nil {
		return fmt.Errorf("opening retained source for %s (deploy again to restore it): %w", name, err)
	}
	defer source.Close()

	work := newDeploymentContext(ctx)
	defer work.Close()
	defer func() { retErr = work.result(retErr) }()
	id := idgen.Gen("rb")
	s.statuses.Register(id, NewRPCStatusSender(args.Status(), s.inner.Log))
	defer s.statuses.Unregister(id)
	s.streams.Register(id, source)
	defer s.streams.Cleanup(id)

	// The deployed source, not the caller's working tree or metadata, owns
	// provenance. Carry it through the existing deployment tracking path.
	req := &build_v1alpha.DeployRequest{}
	if supplied := args.Deployment(); supplied != nil {
		req.SetClusterId(supplied.ClusterId())
		req.SetMessage(supplied.Message())
	}
	git := &build_v1alpha.GitInfo{}
	git.SetSha(version.Source.GitSha)
	git.SetBranch(version.Source.GitBranch)
	git.SetRepository(version.Source.Repository)
	req.SetGitInfo(git)
	executionID := "rebuild-" + id
	if err := s.startBuild(work.control, work.action, executionID, name, id, nil, nil, req, string(app.ActiveVersion)); err != nil {
		return err
	}
	return s.populateResults(work.control, executionID, name, state.Results(), "")
}
