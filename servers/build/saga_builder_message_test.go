package build

import (
	"context"
	"strings"
	"testing"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

func TestEphemeralBuildRejectsDeploymentMessageBeforeWork(t *testing.T) {
	ctx := context.Background()
	inner := &Builder{}
	s := &SagaBuilder{inner: inner}
	client := build_v1alpha.BuilderClient{Client: rpc.LocalClient(build_v1alpha.AdaptBuilder(s))}
	req := &build_v1alpha.DeployRequest{}
	req.SetMessage("why this deploy happened")
	data := stream.ServeReader(ctx, strings.NewReader(""))
	status := stream.StreamRecv(func(*build_v1alpha.Status) error { return nil })

	t.Run("tar", func(t *testing.T) {
		_, err := client.BuildFromTar(ctx, "web", data, status, nil, "preview", "", req)
		if err == nil || !strings.Contains(err.Error(), "deployment message is not supported") {
			t.Fatalf("expected ephemeral message rejection before stream registration: %v", err)
		}
	})

	t.Run("prepared retains upload session", func(t *testing.T) {
		inner.sessions.Store("upload-1", &buildSession{})
		_, err := client.BuildFromPrepared(ctx, "upload-1", data, status, nil, "preview", "", req)
		if err == nil || !strings.Contains(err.Error(), "deployment message is not supported") {
			t.Fatalf("expected ephemeral message rejection before consuming session: %v", err)
		}
		if _, ok := inner.sessions.Load("upload-1"); !ok {
			t.Fatal("rejected request consumed upload session")
		}
	})
}

func TestOversizedBuildMessageRejectedBeforeWork(t *testing.T) {
	ctx := context.Background()
	inner := &Builder{}
	s := &SagaBuilder{inner: inner}
	client := build_v1alpha.BuilderClient{Client: rpc.LocalClient(build_v1alpha.AdaptBuilder(s))}
	req := &build_v1alpha.DeployRequest{}
	req.SetMessage(strings.Repeat("x", deploylifecycle.MaxDeploymentMessageBytes+1))
	data := stream.ServeReader(ctx, strings.NewReader(""))
	status := stream.StreamRecv(func(*build_v1alpha.Status) error { return nil })

	t.Run("tar", func(t *testing.T) {
		_, err := client.BuildFromTar(ctx, "web", data, status, nil, "", "", req)
		if err == nil || !strings.Contains(err.Error(), "at most 1024 bytes") {
			t.Fatalf("expected length rejection before starting saga: %v", err)
		}
	})

	t.Run("prepared retains upload session", func(t *testing.T) {
		inner.sessions.Store("upload-2", &buildSession{})
		_, err := client.BuildFromPrepared(ctx, "upload-2", data, status, nil, "", "", req)
		if err == nil || !strings.Contains(err.Error(), "at most 1024 bytes") {
			t.Fatalf("expected length rejection before consuming session: %v", err)
		}
		if _, ok := inner.sessions.Load("upload-2"); !ok {
			t.Fatal("rejected request consumed upload session")
		}
	})
}
