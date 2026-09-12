// This peer is compiled against both runtime revisions by hack/test-rpc-compat.
// It uses the production generated watch and exec APIs, with deterministic
// handlers so the transport can be checked without etcd or a running sandbox.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"time"

	entityserver "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	execapi "miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: compat matrix OLD NEW | server [TARGET] | client ADDR")
	}
	switch os.Args[1] {
	case "matrix":
		if len(os.Args) != 4 {
			return errors.New("usage: compat matrix OLD NEW")
		}
		return matrix(os.Args[2], os.Args[3])
	case "server":
		return serve()
	case "client":
		if len(os.Args) != 3 {
			return errors.New("usage: compat client ADDR")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return client(ctx, os.Args[2])
	default:
		return fmt.Errorf("unknown mode %q", os.Args[1])
	}
}

type watchServer struct{ entityserver.EntityAccess }

func (*watchServer) WatchIndex(ctx context.Context, call *entityserver.EntityAccessWatchIndex) error {
	args := call.Args()
	values := args.Values()
	defer values.Close()
	for i := int64(1); i <= 16; i++ {
		op := &entityserver.EntityOp{}
		op.SetRevision(i)
		if _, err := values.Send(ctx, op); err != nil {
			return err
		}
		if args.FromRevision() < 0 {
			<-ctx.Done()
			return ctx.Err()
		}
	}
	return nil
}

type execServer struct {
	execapi.SandboxExec
	state  *rpc.State
	target string
}

func (s *execServer) Exec(ctx context.Context, call *execapi.SandboxExecExec) error {
	args := call.Args()
	input, output := stream.ToReader(ctx, args.Input()), stream.ToWriter(ctx, args.Output())
	defer input.Close()
	defer output.Close()
	if s.target != "" {
		remote, err := s.state.Connect(s.target, "exec")
		if err != nil {
			return err
		}
		windows := make(chan *execapi.WindowSize, 1)
		stream.ChanWriter(ctx, args.WindowUpdates(), windows)
		result, err := execapi.NewSandboxExecClient(remote).Exec(ctx, args.Category(), args.Value(), args.Command(), args.Options(), stream.ServeReader(ctx, input), stream.ServeWriter(ctx, output), stream.ChanReader(windows))
		if err != nil {
			return err
		}
		call.Results().SetCode(result.Code())
		return nil
	}
	// Exercise all three callback capabilities used by interactive exec.
	win, err := args.WindowUpdates().Recv(ctx, 1)
	if err != nil {
		return err
	}
	defer args.WindowUpdates().Close()
	if win.Value().Width() != 80 || win.Value().Height() != 24 {
		return errors.New("incorrect terminal size")
	}
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	call.Results().SetCode(17)
	return nil
}

func serve() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	state, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		return err
	}
	defer state.Close()
	target := ""
	if len(os.Args) > 2 {
		target = os.Args[2]
	}
	state.Server().ExposeValue("watch", entityserver.AdaptEntityAccess(&watchServer{}))
	state.Server().ExposeValue("exec", execapi.AdaptSandboxExec(&execServer{state: state, target: target}))
	fmt.Println(state.LoopbackAddr())
	<-ctx.Done()
	return nil
}

func client(ctx context.Context, addr string) error {
	state, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		return err
	}
	defer state.Close()
	remote, err := state.Connect(addr, "watch")
	if err != nil {
		return err
	}
	watch := entityserver.NewEntityAccessClient(remote)
	var mu sync.Mutex
	var revisions []int64
	_, err = watch.WatchIndex(ctx, entity.Attr{}, 0, stream.Callback(func(op *entityserver.EntityOp) error {
		mu.Lock()
		defer mu.Unlock()
		revisions = append(revisions, op.Revision())
		return nil
	}))
	if err != nil {
		return fmt.Errorf("watch: %w", err)
	}
	mu.Lock()
	if len(revisions) != 16 {
		mu.Unlock()
		return fmt.Errorf("watch got %d revisions", len(revisions))
	}
	for i, v := range revisions {
		if v != int64(i+1) {
			mu.Unlock()
			return fmt.Errorf("watch revision %d: %d", i, v)
		}
	}
	mu.Unlock()

	// Cancel an established long-lived call, then make a fresh streaming call.
	watchCtx, cancelWatch := context.WithCancel(ctx)
	started := make(chan struct{}, 1)
	finished := make(chan error, 1)
	go func() {
		_, err := watch.WatchIndex(watchCtx, entity.Attr{}, -1, stream.Callback(func(*entityserver.EntityOp) error {
			select {
			case started <- struct{}{}:
			default:
			}
			return nil
		}))
		finished <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		cancelWatch()
		return ctx.Err()
	}
	cancelWatch()
	select {
	case err := <-finished:
		if err == nil {
			return errors.New("canceled watch succeeded")
		}
	case <-ctx.Done():
		return fmt.Errorf("watch cancellation: %w", ctx.Err())
	}
	remote, err = state.Connect(addr, "exec")
	if err != nil {
		return err
	}
	payload := bytes.Repeat([]byte("interactive exec payload\n"), 16384)
	var output lockedBuffer
	windows := make(chan *execapi.WindowSize, 1)
	size := &execapi.WindowSize{}
	size.SetWidth(80)
	size.SetHeight(24)
	windows <- size
	close(windows)
	result, err := execapi.NewSandboxExecClient(remote).Exec(ctx, "id", "sandbox", "echo", &execapi.ShellOptions{}, stream.ServeReader(ctx, bytes.NewReader(payload)), stream.ServeWriter(ctx, &output), stream.ChanReader(windows))
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	if result.Code() != 17 || !output.equal(payload) {
		return errors.New("exec output or exit code mismatch")
	}
	fmt.Println("PASS watch, cancellation, exec stdin/stdout/window callbacks")
	return nil
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *lockedBuffer) equal(p []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Equal(b.buf.Bytes(), p)
}
