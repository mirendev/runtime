// Package appmetrics runs the coordinator's managed application metrics
// scraper and keeps its sandbox targets and remote-write identity current.
package appmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/base"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/slogout"
	"miren.dev/runtime/pkg/workloadidentity"
)

const (
	containerName = "miren-app-metrics"
	readyPort     = 8429

	tokenTTL           = time.Hour
	tokenRefreshLeeway = 5 * time.Minute
	tokenRetryInterval = time.Minute
)

const scrapeConfig = `global:
  scrape_interval: 30s
  scrape_timeout: 10s
scrape_configs:
  - job_name: miren-managed-apps
    honor_labels: false
    max_scrape_size: 2097152
    sample_limit: 10000
    label_limit: 64
    file_sd_configs:
      - files:
          - /vmagent-data/targets.json
`

type Config struct {
	RemoteWriteURL string
	Audience       string
	ClusterID      string
	HTTPPort       int
	// Image overrides the production pull-through reference in integration tests.
	Image string
}

type Component struct {
	*base.BaseComponent

	eac    *entityserver_v1alpha.EntityAccessClient
	issuer *workloadidentity.Issuer

	cancel    context.CancelFunc
	wg        sync.WaitGroup
	discovery *targetDiscovery
	httpPort  int
}

func New(log *slog.Logger, cc *containerd.Client, namespace, dataPath string, eac *entityserver_v1alpha.EntityAccessClient, issuer *workloadidentity.Issuer) *Component {
	component := &Component{
		BaseComponent: base.NewBaseComponent(log, cc, namespace, dataPath, "app metrics"),
		eac:           eac,
		issuer:        issuer,
	}
	component.CreateTask = component.createTask
	component.GetReadyPort = func() int { return component.httpPort }
	return component
}

func (c *Component) Start(ctx context.Context, config Config) error {
	c.LockOp()
	defer c.UnlockOp()

	if c.IsRunning() {
		return fmt.Errorf("app metrics component already running")
	}
	if c.eac == nil {
		return fmt.Errorf("app metrics requires entity access")
	}
	if c.issuer == nil {
		return fmt.Errorf("app metrics requires a workload identity issuer")
	}
	if config.RemoteWriteURL == "" || config.Audience == "" {
		return fmt.Errorf("app metrics remote-write URL and audience are required")
	}
	if config.HTTPPort == 0 {
		config.HTTPPort = readyPort
	}
	c.httpPort = config.HTTPPort

	dataPath := filepath.Join(c.DataPath, "app-metrics")
	if err := os.MkdirAll(filepath.Join(dataPath, "queue"), 0700); err != nil {
		return fmt.Errorf("creating app metrics data directory: %w", err)
	}
	configPath := filepath.Join(dataPath, "scrape.yml")
	targetsPath := filepath.Join(dataPath, "targets.json")
	tokenPath := filepath.Join(dataPath, "remote-write.token")
	if err := writeFileAtomic(configPath, []byte(scrapeConfig), 0644); err != nil {
		return fmt.Errorf("writing vmagent scrape config: %w", err)
	}
	if err := writeFileAtomic(targetsPath, []byte("[]\n"), 0644); err != nil {
		return fmt.Errorf("writing initial vmagent targets: %w", err)
	}
	if err := c.refreshToken(tokenPath, config.Audience); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.discovery = newTargetDiscovery(c.Log, c.eac, targetsPath, config.ClusterID)
	if err := c.discovery.Start(runCtx); err != nil {
		cancel()
		return fmt.Errorf("starting app metrics target discovery: %w", err)
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)
	imageRef := config.Image
	if imageRef == "" {
		imageRef = imagerefs.VMagent
	}
	c.Log.Info("pulling vmagent image", "image", imageRef)
	image, err := c.CC.Pull(ctx, imageRef, containerd.WithPullUnpack)
	if err != nil {
		c.stopBackground()
		return fmt.Errorf("pulling vmagent image: %w", err)
	}

	if existing, err := c.CC.LoadContainer(ctx, containerName); err == nil {
		// The destination and mounted config are part of the container spec. A
		// server restart must recreate it so an operator's config change cannot
		// leave an old destination running indefinitely.
		c.CleanupExistingContainer(ctx, existing)
	}

	container, err := c.createContainer(ctx, image, dataPath, config.RemoteWriteURL, config.HTTPPort)
	if err != nil {
		c.stopBackground()
		return fmt.Errorf("creating vmagent container: %w", err)
	}
	c.SetContainer(container)

	task, err := c.createTask(ctx, container)
	if err != nil {
		c.CleanupExistingContainer(ctx, container)
		c.stopBackground()
		return fmt.Errorf("creating vmagent task: %w", err)
	}
	if err := task.Start(ctx); err != nil {
		task.Delete(ctx)
		c.CleanupExistingContainer(ctx, container)
		c.stopBackground()
		return fmt.Errorf("starting vmagent task: %w", err)
	}
	c.SetTask(task)
	if err := c.WaitForReady(ctx, "127.0.0.1", config.HTTPPort); err != nil {
		c.stopBackground()
		c.StopTask(ctx, task)
		c.CleanupExistingContainer(ctx, container)
		c.SetRunning(false)
		return err
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.rotateToken(runCtx, tokenPath, config.Audience)
	}()
	c.StartExitMonitor(runCtx)
	c.Log.Info("managed application metrics started")
	return nil
}

func (c *Component) Stop(ctx context.Context) error {
	err := c.BaseComponent.Stop(ctx)
	c.stopBackground()
	return err
}

func (c *Component) stopBackground() {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.discovery != nil {
		c.discovery.Stop()
		c.discovery = nil
	}
	c.wg.Wait()
}

func (c *Component) refreshToken(path, audience string) error {
	token, err := c.issuer.IssueSystemWorkloadToken(
		workloadidentity.SystemWorkloadTelemetryWriter,
		workloadidentity.TokenOptions{Audience: []string{audience}, TTL: tokenTTL},
	)
	if err != nil {
		return fmt.Errorf("minting metrics remote-write token: %w", err)
	}
	if err := writeFileAtomic(path, []byte(token+"\n"), 0600); err != nil {
		return fmt.Errorf("writing metrics remote-write token: %w", err)
	}
	return nil
}

func (c *Component) rotateToken(ctx context.Context, path, audience string) {
	next := tokenTTL - tokenRefreshLeeway
	for {
		timer := time.NewTimer(next)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if err := c.refreshToken(path, audience); err != nil {
			c.Log.Error("failed to refresh metrics remote-write token", "error", err)
			next = tokenRetryInterval
			continue
		}
		next = tokenTTL - tokenRefreshLeeway
	}
}

func (c *Component) createTask(ctx context.Context, container containerd.Container) (containerd.Task, error) {
	return container.NewTask(ctx, slogout.WithLogger(c.Log, "vmagent"))
}

func (c *Component) createContainer(ctx context.Context, image containerd.Image, dataPath, remoteWriteURL string, httpPort int) (containerd.Container, error) {
	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithHostNamespace(specs.NetworkNamespace),
		oci.WithProcessArgs(vmagentArgs(remoteWriteURL, httpPort)...),
		oci.WithHostHostsFile,
		oci.WithHostResolvconf,
		containerdx.WithRlimitNOFILE(65536),
		oci.WithMounts([]specs.Mount{{
			Destination: "/vmagent-data",
			Type:        "bind",
			Source:      dataPath,
			Options:     []string{"rbind", "rw"},
		}}),
	}
	return c.CC.NewContainer(
		ctx,
		containerName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(containerName+"-snapshot", image),
		containerd.WithNewSpec(opts...),
	)
}

func vmagentArgs(remoteWriteURL string, httpPort int) []string {
	return []string{
		"/vmagent-prod",
		"-promscrape.config=/vmagent-data/scrape.yml",
		"-promscrape.fileSDCheckInterval=5s",
		"-remoteWrite.url=" + remoteWriteURL,
		"-remoteWrite.forcePromProto",
		"-remoteWrite.bearerTokenFile=/vmagent-data/remote-write.token",
		"-remoteWrite.tmpDataPath=/vmagent-data/queue",
		fmt.Sprintf("-httpListenAddr=127.0.0.1:%d", httpPort),
		"-enableTCP6",
	}
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
