//go:build linux

package server

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"miren.dev/runtime/components/victorialogs"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
)

type victoriaLogsBootInputs struct {
	log      *slog.Logger
	config   serverconfig.VictoriaLogsConfig
	dataPath string
}

type victoriaLogsBootOutput struct {
	address string
}

type victoriaLogsBoot struct {
	component *boot.Component
	inputs    victoriaLogsBootInputs
	log       *slog.Logger
	server    *victorialogs.VictoriaLogsComponent
	output    boot.Output[victoriaLogsBootOutput]
}

func victoriaLogsInputs(options StartOptions) victoriaLogsBootInputs {
	return victoriaLogsBootInputs{log: options.Log, config: options.Config.Victorialogs, dataPath: options.Config.Server.GetDataPath()}
}

func newVictoriaLogsBoot(inputs victoriaLogsBootInputs, containerd boot.Output[containerdBootOutput]) *victoriaLogsBoot {
	b := &victoriaLogsBoot{inputs: inputs}
	stop := boot.WithStop(b.stop, componentStopTimeout)
	if inputs.config.GetStartEmbedded() {
		b.component, b.output = boot.Provide1("victorialogs", containerd, b.startEmbedded, stop)
	} else {
		b.component, b.output = boot.Provide0("victorialogs", b.startExternal, stop)
	}
	return b
}

func (b *victoriaLogsBoot) startExternal(ctx context.Context) (victoriaLogsBootOutput, error) {
	b.log = b.inputs.log
	if b.inputs.config.GetAddress() == "" {
		return victoriaLogsBootOutput{}, fmt.Errorf("victorialogs address not specified and embedded victorialogs not started")
	}
	b.log.Info("using external victorialogs", "address", b.inputs.config.GetAddress())
	if err := waitForVictoriaHealth(ctx, "victorialogs", b.inputs.config.GetAddress()); err != nil {
		return victoriaLogsBootOutput{}, err
	}
	if err := checkExternalVictoriaLogsVersion(ctx, b.inputs.config.GetAddress()); err != nil {
		b.log.Warn("external victorialogs all-field search unavailable or unverified (requires v1.50+)", "error", err)
	}
	return victoriaLogsBootOutput{address: b.inputs.config.GetAddress()}, nil
}

var victoriaLogsVersionMetric = regexp.MustCompile(`^vm_app_version\{[^\n]*short_version="v([0-9]+)\.([0-9]+)(?:\.[0-9]+)?"`)

func checkExternalVictoriaLogsVersion(ctx context.Context, address string) error {
	baseURL := strings.TrimRight(address, "/")
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/metrics", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metrics endpoint returned %s", resp.Status)
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "vm_app_version{") {
			continue
		}
		matches := victoriaLogsVersionMetric.FindStringSubmatch(line)
		if len(matches) != 3 {
			return fmt.Errorf("cannot parse vm_app_version short_version")
		}
		major, _ := strconv.Atoi(matches[1])
		minor, _ := strconv.Atoi(matches[2])
		if major < 1 || (major == 1 && minor < 50) {
			return fmt.Errorf("external victorialogs v%d.%d lacks all-field search (requires v1.50+)", major, minor)
		}
		return nil
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("vm_app_version metric unavailable")
}

func (b *victoriaLogsBoot) startEmbedded(ctx context.Context, containerd containerdBootOutput) (victoriaLogsBootOutput, error) {
	b.log = b.inputs.log
	log := b.log
	log.Info("starting embedded victorialogs server", "http-port", b.inputs.config.GetHTTPPort())
	b.server = victorialogs.NewVictoriaLogsComponent(log, containerd.Client, containerd.Namespace, b.inputs.dataPath)
	if err := b.server.Start(ctx, victorialogs.VictoriaLogsConfig{
		HTTPPort:        b.inputs.config.GetHTTPPort(),
		RetentionPeriod: b.inputs.config.GetRetentionPeriod(),
	}); err != nil {
		return victoriaLogsBootOutput{}, err
	}
	address := b.server.HTTPEndpoint()
	if err := waitForVictoriaHealth(ctx, "victorialogs", address); err != nil {
		return victoriaLogsBootOutput{}, err
	}
	log.Info("embedded victorialogs started", "http-endpoint", address)
	return victoriaLogsBootOutput{address: address}, nil
}

func (b *victoriaLogsBoot) stop(ctx context.Context) error {
	if b.server == nil {
		return nil
	}
	b.log.Info("stopping embedded victorialogs")
	return b.server.Stop(ctx)
}
