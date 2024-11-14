package testutils

import (
	"log/slog"
	"os"

	containerd "github.com/containerd/containerd/v2/client"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/slogfmt"
)

func Registry() *asm.Registry {
	var r asm.Registry

	r.Provide(func() (*containerd.Client, error) {
		return containerd.New("/home/evanphx/tmp/miren-tmp/containerd/container.sock")
	})

	r.Register("namespace", "miren-test")

	log := slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	r.Register("log", log)

	return &r
}
