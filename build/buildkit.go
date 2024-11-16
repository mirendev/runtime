package build

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/identity"
	"miren.dev/runtime/observability"

	"github.com/tonistiigi/fsutil"
)

type Buildkit struct {
	Client *client.Client

	Log       *slog.Logger
	LogWriter observability.LogWriter
}

func NewBuildkit(ctx context.Context, cl *client.Client, tempdir string) (*Buildkit, error) {
	bk := &Buildkit{
		Client: cl,
	}
	return bk, nil
}

type tarOutput struct {
	rc    io.ReadCloser
	mu    sync.Mutex
	bgErr error
}

func (t *tarOutput) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.bgErr != nil {
		return 0, t.bgErr
	}
	return t.rc.Read(p)
}

func (t *tarOutput) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	pe := t.rc.Close()

	if t.bgErr != nil {
		return t.bgErr
	}

	return pe
}

func (b *Buildkit) Transform(ctx context.Context, dfs fsutil.FS) (io.ReadCloser, error) {
	mounts := map[string]fsutil.FS{
		"dockerfile": dfs,
		"context":    dfs,
	}

	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	output := client.ExportEntry{
		Type: "oci",
		Output: func(attrs map[string]string) (io.WriteCloser, error) {
			return w, nil
		},
	}

	ref := identity.NewID()

	solveOpt := client.SolveOpt{
		Exports:     []client.ExportEntry{output},
		LocalMounts: mounts,
		Frontend:    "dockerfile.v0",
		Ref:         ref,
		/*
			TODO(emp): add this when we're ready to support verifying and/or displaying sbom
					FrontendAttrs: map[string]string{
						"attest:sbom": "",
					},
		*/
	}

	sreq := gateway.SolveRequest{
		Frontend:    solveOpt.Frontend,
		FrontendOpt: solveOpt.FrontendAttrs,
	}

	// not using shared context to not disrupt display but let is finish reporting errors
	//pw, err := progresswriter.NewPrinter(ctx, os.Stderr, "rawjson")
	//if err != nil {
	//return nil, err
	//}

	//mw := progresswriter.NewMultiWriter(pw)

	var to tarOutput
	to.rc = r

	go func() {
		defer w.Close()

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		ssProgress := make(chan *client.SolveStatus, 1)

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case ss := <-ssProgress:
					if data, err := json.Marshal(ss); err != nil {
						err := b.LogWriter.WriteEntry("build", ref, observability.LogEntry{
							Timestamp: time.Now(),
							Body:      string(data),
						})
						if err != nil {
							b.Log.Error("failed to write log entry", "err", err)
						}
					}
				}
			}
		}()

		_, err = b.Client.Build(ctx, solveOpt, "miren", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			res, err := c.Solve(ctx, sreq)
			if err != nil {
				return nil, err
			}

			return res, nil
		}, ssProgress)

		to.mu.Lock()
		to.bgErr = err
		to.mu.Unlock()
	}()

	return &to, nil
}
