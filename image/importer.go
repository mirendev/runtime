package image

import (
	"context"
	"io"

	containerd "github.com/containerd/containerd/v2/client"
	tarchive "github.com/containerd/containerd/v2/core/transfer/archive"
	"github.com/containerd/containerd/v2/core/transfer/image"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/platforms"
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
	"miren.dev/runtime/pkg/asm/autoreg"
)

type ImageImporter struct {
	CC        *containerd.Client
	Namespace string `asm:"namespace"`
}

var _ = autoreg.Register[ImageImporter]()

func (i *ImageImporter) ImportImage(ctx context.Context, r io.Reader, indexName string) error {
	ctx = namespaces.WithNamespace(ctx, i.Namespace)
	var opts []image.StoreOpt
	opts = append(opts, image.WithNamedPrefix("mn-tmp", true))

	// Only when all-platforms not specified, we will check platform value
	// Implicitly if the platforms is empty, it means all-platforms
	platSpec := platforms.DefaultSpec()
	opts = append(opts, image.WithPlatforms(platSpec))

	opts = append(opts, image.WithUnpack(platSpec, ""))

	is := image.NewStore(indexName, opts...)

	var iopts []tarchive.ImportOpt

	iis := tarchive.NewImageImportStream(r, "", iopts...)

	return i.CC.Transfer(ctx, iis, is)
}

func (i *ImageImporter) PullImage(ctx context.Context, ref string) (containerd.Image, error) {
	ctx = namespaces.WithNamespace(ctx, i.Namespace)

	img, err := i.CC.GetImage(ctx, ref)
	if err != nil {
		return i.CC.Pull(ctx, ref, containerd.WithPullUnpack)
	}

	return img, nil
}
