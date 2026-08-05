package commands

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/opencontainers/runtime-spec/specs-go"
	"miren.dev/runtime/components/etcd"
)

const debugEtcdctlProbeTimeout = 5 * time.Second

const debugEtcdctlDescription = `Place Miren's ` + "`" + `-n` + "`" + `/` + "`" + `--namespace` + "`" + ` and ` + "`" + `--socket` + "`" + ` flags before the etcdctl arguments. Once the etcdctl arguments begin, all remaining flags are passed through to etcdctl unchanged.`

type debugEtcdctlOptions struct {
	Namespace   string   `short:"n" long:"namespace" description:"containerd namespace" default:"miren"`
	Socket      string   `long:"socket" description:"path to containerd socket"`
	Args        []string `rest:"true"`
	Passthrough []string `unknown:"true"`
}

func (o debugEtcdctlOptions) etcdctlArgs() []string {
	args := make([]string, 0, len(o.Args)+len(o.Passthrough))
	args = append(args, o.Args...)
	return append(args, o.Passthrough...)
}

func DebugEtcdctl(ctx *Context, opts debugEtcdctlOptions) error {
	socket := opts.Socket
	if socket == "" {
		socket = defaultContainerdSocket()
	}

	client, err := containerd.New(socket)
	if err != nil {
		return localEtcdUnavailableError(socket, opts.Namespace, err)
	}
	defer func() {
		if client != nil {
			client.Close()
		}
	}()

	containerCtx, cancel := context.WithTimeout(
		namespaces.WithNamespace(ctx, opts.Namespace),
		debugEtcdctlProbeTimeout,
	)
	defer cancel()

	container, err := client.LoadContainer(containerCtx, etcd.ContainerName)
	if err != nil {
		return localEtcdUnavailableError(socket, opts.Namespace, err)
	}
	if _, err := container.Task(containerCtx, nil); err != nil {
		return localEtcdTaskUnavailableError(err)
	}

	spec, err := container.Spec(containerCtx)
	if err != nil {
		return fmt.Errorf("reading etcd container spec: %w", err)
	}

	args, err := buildEtcdctlExecArgs(spec, opts.etcdctlArgs(), newEtcdctlExecID())
	if err != nil {
		return err
	}
	cancel()

	_ = client.Close()
	client = nil

	return execCtr(socket, opts.Namespace, args)
}

func localEtcdUnavailableError(socket, namespace string, err error) error {
	if errdefs.IsNotFound(err) {
		return fmt.Errorf(
			"embedded etcd container %q was not found in containerd namespace %q; "+
				"this command must be run on a local Miren coordinator with embedded etcd",
			etcd.ContainerName, namespace,
		)
	}

	if errdefs.IsPermissionDenied(err) || strings.Contains(strings.ToLower(err.Error()), "permission denied") {
		return fmt.Errorf(
			"permission denied accessing containerd at %s; run this command with sufficient permissions "+
				"on a local Miren coordinator",
			socket,
		)
	}

	return fmt.Errorf(
		"cannot inspect embedded etcd through containerd at %s: %w "+
			"(this command must be run on a local Miren coordinator with embedded etcd)",
		socket, err,
	)
}

func localEtcdTaskUnavailableError(err error) error {
	if errdefs.IsNotFound(err) {
		return fmt.Errorf(
			"embedded etcd container %q exists but is not running; "+
				"check the Miren server and coordinator logs",
			etcd.ContainerName,
		)
	}

	return fmt.Errorf("checking embedded etcd task: %w", err)
}

func buildEtcdctlExecArgs(spec *specs.Spec, etcdctlArgs []string, execID string) ([]string, error) {
	if spec == nil || spec.Process == nil {
		return nil, fmt.Errorf("etcd container spec has no process configuration")
	}

	endpoint, tlsEnabled, err := etcdClientEndpoint(spec.Process.Args)
	if err != nil {
		return nil, err
	}

	connectionArgs := []string{"--endpoints=" + endpoint}
	if tlsEnabled {
		if !hasMount(spec.Mounts, "/certs") {
			return nil, fmt.Errorf("etcd advertises a TLS endpoint but its client certificates are not mounted at /certs")
		}
		connectionArgs = append(connectionArgs,
			"--cacert=/certs/ca.crt",
			"--cert=/certs/client.crt",
			"--key=/certs/client.key",
		)
	}

	args := []string{
		"tasks", "exec",
		"--exec-id", execID,
		etcd.ContainerName,
		"/usr/local/bin/etcdctl",
	}
	args = append(args, connectionArgs...)
	args = append(args, etcdctlArgs...)
	return args, nil
}

func etcdClientEndpoint(processArgs []string) (string, bool, error) {
	endpoint := processFlagValue(processArgs, "--advertise-client-urls")
	if endpoint == "" {
		endpoint = processFlagValue(processArgs, "--listen-client-urls")
	}
	if endpoint == "" {
		return "", false, fmt.Errorf("etcd container spec has no client endpoint")
	}

	var scheme string
	for _, rawEndpoint := range strings.Split(endpoint, ",") {
		parsed, err := url.Parse(rawEndpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "", false, fmt.Errorf("etcd container spec has invalid client endpoint %q", rawEndpoint)
		}
		if scheme != "" && parsed.Scheme != scheme {
			return "", false, fmt.Errorf("etcd container spec mixes HTTP and HTTPS client endpoints")
		}
		scheme = parsed.Scheme
	}

	return endpoint, scheme == "https", nil
}

func processFlagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
		if value, ok := strings.CutPrefix(arg, flag+"="); ok {
			return value
		}
	}
	return ""
}

func hasMount(mounts []specs.Mount, destination string) bool {
	for _, mount := range mounts {
		if mount.Destination == destination {
			return true
		}
	}
	return false
}

func newEtcdctlExecID() string {
	return fmt.Sprintf("miren-etcdctl-%d-%d", os.Getpid(), time.Now().UnixNano())
}
