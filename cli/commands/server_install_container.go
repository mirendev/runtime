package commands

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/quic-go/quic-go/http3"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/ui"
)

// ServerInstallContainer sets up a container (via Docker or Podman) to run the
// miren server.
func ServerInstallContainer(ctx *Context, opts struct {
	Image         string            `short:"i" long:"image" description:"Container image to use" default:"oci.miren.cloud/miren:latest"`
	Name          string            `short:"n" long:"name" description:"Container name"`
	Runtime       string            `long:"runtime" description:"Container runtime to use: docker or podman (auto-detected by default, preferring docker)"`
	Force         bool              `short:"f" long:"force" description:"Remove existing container if present"`
	AllowRootless bool              `long:"allow-rootless" description:"Install even on a rootless runtime (control plane only; app workloads won't run)"`
	HTTPPort      int               `long:"http-port" description:"HTTP port mapping" default:"80"`
	IngressMode   string            `long:"ingress-mode" description:"Ingress mode: tls-autoprovision (default), behind-proxy-http (Miren serves plain HTTP behind a TLS-terminating proxy like tailscale serve / nginx), or behind-proxy-https (Miren terminates TLS on :443 behind a TCP-passthrough proxy)"`
	HostNetwork   bool              `long:"host-network" description:"Use host networking (ignores port mappings)"`
	WithoutCloud  bool              `long:"without-cloud" description:"Skip cloud registration setup"`
	ClusterName   string            `long:"cluster-name" description:"Cluster name for cloud registration"`
	CloudURL      string            `short:"u" long:"url" description:"Cloud URL for registration" default:"https://miren.cloud"`
	Tags          map[string]string `short:"t" long:"tag" description:"Tags for the cluster (key:value)"`
	Labs          []string          `short:"l" long:"labs" description:"Miren Labs features to enable (e.g. sagas). Prefix with - to disable."`
}) error {
	if opts.ClusterName == "" {
		opts.ClusterName = opts.Name
	} else if opts.Name == "" {
		opts.Name = opts.ClusterName
	}

	if opts.Name == "" {
		opts.Name = "miren"
	}

	if err := validateIngressMode(opts.IngressMode); err != nil {
		return err
	}

	// Pick the container runtime (docker or podman) before any work so we fail
	// fast with clear guidance when neither is installed.
	ctx.Info("Checking container runtime...")
	rt, err := resolveContainerRuntime(opts.Runtime)
	if err != nil {
		return err
	}
	ctx.Completed("Using %s", rt.bin)

	// A rootless runtime can host the server and control plane, but Miren's
	// privileged nested sandboxing can't start app workloads there. Stop early
	// with guidance unless the operator explicitly opts into a control-plane-only
	// install.
	if rt.isRootless() {
		if !opts.AllowRootless {
			return rootlessRuntimeError(ctx, rt)
		}
		ctx.Warn("Rootless %s: installing anyway (--allow-rootless). The control plane "+
			"will come up, but app deploys will crash-loop.", rt.bin)
	}

	// Heads-up (never a blocker) when the runtime host is short on memory for the
	// nested stack (containerd + buildkit + etcd + the server).
	warnLowContainerMemory(ctx, rt)

	// Derive volume name from container name
	volumeName := opts.Name + "-data"

	// Check if container already exists
	containerExists, err := rt.containerExists(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for existing container: %w", err)
	}

	if containerExists {
		if !opts.Force {
			return fmt.Errorf("container '%s' already exists (use --force to remove and recreate)", opts.Name)
		}

		ctx.Info("Removing existing container '%s'...", opts.Name)
		if err := rt.removeContainer(opts.Name, true); err != nil {
			return fmt.Errorf("failed to remove existing container: %w", err)
		}
		ctx.Completed("Existing container removed")
	}

	config := containerConfig{
		HTTPPort:    opts.HTTPPort,
		IngressMode: opts.IngressMode,
		VolumeName:  volumeName,
		HostNetwork: opts.HostNetwork,
		Labs:        opts.Labs,
	}

	// Verify the host ports we're about to publish are actually free before we
	// do any expensive setup (volume creation, cloud registration). We run this
	// after removing any existing miren container so we don't flag its ports as
	// "taken" by itself. Every required port is a clear, actionable stop — we
	// don't silently relocate any of them, so what the user asked for is what
	// they get (or a clear explanation of why they can't have it).
	if err := preflightPortCheck(ctx, config); err != nil {
		return err
	}

	// Create volume if it doesn't exist
	if err := rt.ensureVolume(volumeName); err != nil {
		return fmt.Errorf("failed to ensure volume exists: %w", err)
	}

	// Register with cloud unless --without-cloud is specified
	if !opts.WithoutCloud {
		if err := performRegistrationPreStart(ctx, rt, opts.Image, volumeName, containerRegistrationOptions{
			ClusterName: opts.ClusterName,
			CloudURL:    opts.CloudURL,
			Tags:        opts.Tags,
		}); err != nil {
			ctx.Warn("Cloud registration failed: %v", err)
			ctx.Info("Continuing with installation without cloud registration")
			ctx.Info("You can register later by running: %s exec %s miren server register", rt.bin, opts.Name)
		} else {
			ctx.Completed("Cloud registration complete")
		}
	} else {
		ctx.Info("Skipping cloud registration (--without-cloud specified)")
	}

	// Create and optionally start the container
	ctx.Info("Creating miren server container...")
	containerID, err := rt.createContainer(opts.Name, opts.Image, config)
	if err != nil {
		// The pre-flight check above catches the common case, but a port can
		// still be grabbed in the race between check and create, or be
		// undetectable when we lack privileges to test-bind it (binding <1024
		// unprivileged returns EACCES, not EADDRINUSE). Translate the engine's
		// raw "address already in use" error into friendly guidance rather than
		// leaking a stack trace.
		if friendly, ok := enginePortConflictError(ctx, err); ok {
			return friendly
		}
		return fmt.Errorf("failed to create container: %w", err)
	}
	ctx.Completed("Container created: %s", containerID[:12])

	// Start the container
	ctx.Info("Starting miren server container...")
	if err := rt.startContainer(opts.Name); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	ctx.Completed("Container started")

	// Wait for server to be ready
	ctx.Info("Waiting for miren server to initialize...")

	// NOTE: We've found that, on docker desktop, if we make a http3 UDP request
	// to the port BEFORE the container has booted enough, docker desktop seems to
	// stop listening on that port entirely until the container is restarted.
	// Only a 1 second delay was required to fix the issue in testing, but
	// I'm making it 3 seconds here to be safe.
	time.Sleep(3 * time.Second)

	if err := waitForServerReady(ctx); err != nil {
		ctx.Warn("Failed to confirm server is ready: %v", err)
		ctx.Info("The server may still be starting. Check logs with: %s logs %s", rt.bin, opts.Name)
	} else {
		ctx.Completed("Server is ready")
	}

	// Copy client configuration from container
	ctx.Info("Configuring miren client...")
	if err := copyClientConfig(ctx, rt, opts.Name); err != nil {
		ctx.Warn("Failed to copy client configuration: %v", err)
		ctx.Info("You may need to configure the client manually")
	} else {
		ctx.Completed("Client configuration saved")
	}

	// Print helpful information
	fmt.Println()
	ctx.Info("Installation complete!")
	fmt.Println()
	ctx.Info("Container management:")
	fmt.Printf("  View status:  %s ps -f name=%s\n", rt.bin, opts.Name)
	fmt.Printf("  View logs:    %s logs %s\n", rt.bin, opts.Name)
	fmt.Printf("  Follow logs:  %s logs -f %s\n", rt.bin, opts.Name)
	fmt.Printf("  Stop:         %s stop %s\n", rt.bin, opts.Name)
	fmt.Printf("  Start:        %s start %s\n", rt.bin, opts.Name)
	fmt.Printf("  Remove:       %s rm -f %s\n", rt.bin, opts.Name)
	fmt.Println()

	return nil
}

// ServerUninstallContainer removes the miren container and optionally the volume.
func ServerUninstallContainer(ctx *Context, opts struct {
	Name         string `short:"n" long:"name" description:"Container name" default:"miren"`
	Runtime      string `long:"runtime" description:"Container runtime to use: docker or podman (auto-detected by default, preferring docker)"`
	RemoveVolume bool   `long:"remove-volume" description:"Remove the data volume"`
	Force        bool   `short:"f" long:"force" description:"Force removal even if container is running"`
}) error {
	rt, err := resolveContainerRuntime(opts.Runtime)
	if err != nil {
		return err
	}

	// Derive volume name from container name
	volumeName := opts.Name + "-data"

	// Check if container exists
	containerExists, err := rt.containerExists(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for container: %w", err)
	}

	if !containerExists {
		ctx.Warn("Container '%s' does not exist", opts.Name)
		if !opts.RemoveVolume {
			return nil
		}
	} else {
		// Check if container is running
		isRunning, err := rt.containerIsRunning(opts.Name)
		if err != nil {
			return fmt.Errorf("failed to check container status: %w", err)
		}

		// If container is running and not forcing, ask for confirmation
		if isRunning && !opts.Force {
			confirmed, err := ui.Confirm(
				ui.WithMessage(fmt.Sprintf("Container '%s' is currently running. Stop and remove it?", opts.Name)),
				ui.WithDefault(false),
			)
			if err != nil {
				return fmt.Errorf("confirmation failed: %w", err)
			}
			if !confirmed {
				ctx.Info("Uninstall cancelled")
				return nil
			}
		}

		// Stop container gracefully if running
		if isRunning {
			ctx.Info("Stopping container '%s' (waiting for graceful shutdown)...", opts.Name)
			if err := rt.stopContainer(opts.Name, 30); err != nil {
				ctx.Warn("Graceful stop failed: %v", err)
				ctx.Info("Forcing container removal...")
			} else {
				ctx.Completed("Container stopped")
			}
		}

		// Remove container
		ctx.Info("Removing container '%s'...", opts.Name)
		if err := rt.removeContainer(opts.Name, true); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
		ctx.Completed("Container removed")

		// Drop the client-config cluster this install created so uninstall is
		// symmetric with install and doesn't leave a dead cluster behind.
		removeInstalledClusters(ctx)
	}

	// Remove volume if requested
	if opts.RemoveVolume {
		ctx.Info("Removing volume '%s'...", volumeName)
		if err := rt.removeVolume(volumeName); err != nil {
			ctx.Warn("Failed to remove volume: %v", err)
			ctx.Info("You can remove it manually with: %s volume rm %s", rt.bin, volumeName)
		} else {
			ctx.Completed("Volume removed")
		}
	} else {
		fmt.Println()
		ctx.Info("Note: The data volume '%s' has not been removed.", volumeName)
		ctx.Info("To remove it: %s volume rm %s", rt.bin, volumeName)
	}

	fmt.Println()
	ctx.Info("Uninstallation complete!")

	return nil
}

// ServerStatusContainer shows the status of the miren container.
func ServerStatusContainer(ctx *Context, opts struct {
	Name    string `short:"n" long:"name" description:"Container name" default:"miren"`
	Runtime string `long:"runtime" description:"Container runtime to use: docker or podman (auto-detected by default, preferring docker)"`
	Follow  bool   `short:"f" long:"follow" description:"Follow logs in real-time"`
}) error {
	rt, err := resolveContainerRuntime(opts.Runtime)
	if err != nil {
		return err
	}

	// Check if container exists
	containerExists, err := rt.containerExists(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for container: %w", err)
	}

	if !containerExists {
		ctx.Warn("Container '%s' does not exist", opts.Name)
		ctx.Info("Run 'miren server container install' to create it")
		return nil
	}

	// Show container status using the runtime's ps
	cmd := rt.command("ps", "-a", "-f", fmt.Sprintf("name=%s", opts.Name), "--format", "table {{.Names}}\t{{.Status}}\t{{.Ports}}")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		ctx.Warn("Failed to get container status: %v", err)
	}

	fmt.Println()

	// Show recent logs
	if !opts.Follow {
		ctx.Info("Recent logs (use -f to follow):")
		fmt.Println()
		cmd = rt.command("logs", "--tail", "50", opts.Name)
	} else {
		ctx.Info("Following logs (Ctrl+C to stop)...")
		fmt.Println()
		cmd = rt.command("logs", "-f", opts.Name)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Helper functions

type containerConfig struct {
	HTTPPort    int
	IngressMode string
	VolumeName  string
	HostNetwork bool
	Labs        []string
}

// defaultAPIPort is the host UDP port the miren API / control plane (QUIC) is
// published on. The generated client config and the health check both dial it
// by this number, so it's fixed rather than negotiated.
const defaultAPIPort = 8443

// requiredPortsHelpURL points operators at the ingress-mode docs when a
// load-bearing ingress port (80/443) is already held by something else, such
// as a TLS-terminating reverse proxy like `tailscale serve`.
const requiredPortsHelpURL = "https://miren.md/tls#ingress-modes-and-tls"

// portRole identifies what a published host port is for, which determines the
// guidance we show when it's taken (each role has a different right answer).
type portRole int

const (
	roleHTTP  portRole = iota // app HTTP ingress + HTTP-01 ACME challenges
	roleHTTPS                 // app HTTPS ingress
	roleAPI                   // control-plane API (QUIC)
)

// requiredHostPort is one host port Miren needs for a given config. It is the
// single source of truth for both the pre-flight availability check and the
// actual `run -p` arguments, so the two can never drift apart.
type requiredHostPort struct {
	hostPort      int
	containerPort int
	proto         string // "tcp" or "udp"
	role          portRole
}

// requiredHostPorts returns the host ports Miren needs for a given config. The
// API port is needed in every mode; the app ingress ports vary by ingress mode
// (behind-proxy-http hands :443 to the proxy, behind-proxy-https gives up :80).
//
// Under host networking the server binds the container ports directly on the
// host, so the -p mappings (and --http-port) don't apply and the host port
// equals the container port. The pre-flight still needs this list to catch a
// busy port before the server fails to bind at startup; ingressArgs is what
// decides whether to actually publish these via -p.
func requiredHostPorts(config containerConfig) []requiredHostPort {
	httpHostPort := config.HTTPPort
	if config.HostNetwork {
		httpHostPort = 80
	}

	ports := []requiredHostPort{
		{hostPort: defaultAPIPort, containerPort: 8443, proto: "udp", role: roleAPI},
	}
	switch config.IngressMode {
	case serverconfig.IngressModeBehindProxyHTTP:
		ports = append(ports, requiredHostPort{httpHostPort, 80, "tcp", roleHTTP})
	case serverconfig.IngressModeBehindProxyHTTPS:
		ports = append(ports, requiredHostPort{443, 443, "tcp", roleHTTPS})
	default:
		ports = append(ports,
			requiredHostPort{httpHostPort, 80, "tcp", roleHTTP},
			requiredHostPort{443, 443, "tcp", roleHTTPS},
		)
	}
	return ports
}

type portStatus int

const (
	portFree portStatus = iota
	portInUse
	// portUndetermined means we couldn't tell — e.g. binding the port needs
	// privileges we don't have. We let the install proceed and rely on the
	// engine error backstop rather than blocking on a maybe.
	portUndetermined
)

func classifyBindErr(err error) portStatus {
	if err == nil {
		return portFree
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return portInUse
	}
	return portUndetermined
}

func checkHostTCPPort(port int) portStatus {
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err == nil {
		_ = ln.Close()
		return portFree
	}
	return classifyBindErr(err)
}

func checkHostUDPPort(port int) portStatus {
	conn, err := net.ListenPacket("udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err == nil {
		_ = conn.Close()
		return portFree
	}
	return classifyBindErr(err)
}

func hostPortStatus(p requiredHostPort) portStatus {
	if p.proto == "udp" {
		return checkHostUDPPort(p.hostPort)
	}
	return checkHostTCPPort(p.hostPort)
}

// describePortListener makes a best-effort attempt to name the process holding
// a port, purely to make the conflict message friendlier. proto is "TCP" or
// "UDP". Returns "" when lsof is unavailable or reveals nothing (e.g. the
// holder is a root-owned process and we're running unprivileged).
func describePortListener(port int, proto string) string {
	if _, err := exec.LookPath("lsof"); err != nil {
		return ""
	}
	args := []string{"-nP", fmt.Sprintf("-i%s:%d", proto, port)}
	if proto == "TCP" {
		args = append(args, "-sTCP:LISTEN")
	}
	out, err := exec.Command("lsof", args...).Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return ""
	}
	// lsof columns: COMMAND PID USER ...
	fields := strings.Fields(lines[1])
	if len(fields) < 2 {
		return ""
	}
	return fmt.Sprintf("%s, PID %s", fields[0], fields[1])
}

// preflightPortCheck stops the install with actionable guidance if any host
// port we're about to publish is already taken, before any expensive setup
// (volume creation, cloud registration). It's best-effort: binding a port we
// lack privileges for returns portUndetermined and we proceed, relying on the
// engine error backstop to catch what we couldn't test here.
func preflightPortCheck(ctx *Context, config containerConfig) error {
	for _, p := range requiredHostPorts(config) {
		if hostPortStatus(p) == portInUse {
			return portConflict(ctx, p)
		}
	}
	return nil
}

// portConflict prints actionable guidance for a taken host port and returns a
// terse error to halt the install. The remedy differs by role: the HTTP
// listener can simply be relocated with --http-port; a taken 443 is best handed
// off to a TLS-terminating proxy via behind-proxy mode (moving 443 wouldn't help
// — browsers expect HTTPS there); the API port just needs to be freed.
func portConflict(ctx *Context, p requiredHostPort) error {
	heldBy := ""
	if holder := describePortListener(p.hostPort, strings.ToUpper(p.proto)); holder != "" {
		heldBy = fmt.Sprintf(" (%s)", holder)
	}

	ctx.Printf("\n%s Port %d is already in use%s.\n\n", infoRed.Render("✗"), p.hostPort, heldBy)

	switch p.role {
	case roleHTTP:
		ctx.Printf("Miren publishes this port to serve your apps over HTTP and to answer\n")
		ctx.Printf("HTTP-01 TLS challenges. Move it to a free port with:\n")
		ctx.Printf("  %s\n\n", infoGray.Render("miren server container install --http-port 8080"))
		ctx.Printf("Otherwise, free the port and re-run.\n")
	case roleHTTPS:
		ctx.Printf("Miren publishes port 443 to serve your apps with automatic HTTPS.\n\n")
		ctx.Printf("If a TLS-terminating reverse proxy (tailscale serve, nginx, Caddy,\n")
		ctx.Printf("Cloudflare Tunnel) already owns 443, run Miren behind it instead —\n")
		ctx.Printf("Miren serves plain HTTP and the proxy handles TLS:\n")
		ctx.Printf("  %s\n", infoGray.Render("miren server container install --ingress-mode behind-proxy-http"))
		ctx.Printf("  %s\n\n", infoGray.Render(requiredPortsHelpURL))
		ctx.Printf("Otherwise, free the port and re-run.\n")
	case roleAPI:
		ctx.Printf("Miren's control-plane API listens on this port (UDP). The miren CLI\n")
		ctx.Printf("connects to it directly, so it can't be relocated — free the port and\n")
		ctx.Printf("re-run.\n")
	}

	return fmt.Errorf("required port %d is unavailable", p.hostPort)
}

// validateIngressMode rejects an unknown --ingress-mode early with a clear
// message rather than letting the container boot and fail. An empty value means
// "use the default" (tls-autoprovision).
func validateIngressMode(mode string) error {
	switch mode {
	case "",
		serverconfig.IngressModeAutoprovision,
		serverconfig.IngressModeBehindProxyHTTP,
		serverconfig.IngressModeBehindProxyHTTPS:
		return nil
	default:
		return fmt.Errorf("invalid --ingress-mode %q (valid: %s, %s, %s)",
			mode,
			serverconfig.IngressModeAutoprovision,
			serverconfig.IngressModeBehindProxyHTTP,
			serverconfig.IngressModeBehindProxyHTTPS)
	}
}

// rootlessRuntimeError explains why a rootless runtime can't run Miren workloads
// and how to switch to a rootful one, returning a terse error to halt the
// install. Overridable with --allow-rootless for control-plane-only testing.
func rootlessRuntimeError(ctx *Context, rt containerRuntime) error {
	ctx.Printf("\n%s Rootless %s detected.\n\n", infoRed.Render("✗"), rt.bin)
	ctx.Printf("Miren's server will start, but it can't run app workloads under a rootless\n")
	ctx.Printf("runtime yet: the sandbox runtime does privileged, nested containerization\n")
	ctx.Printf("(its own containerd, per-sandbox namespaces and devices) that a rootless\n")
	ctx.Printf("container can't provide. The control plane comes up; app deploys crash-loop.\n\n")
	if rt.bin == "podman" {
		ctx.Printf("Fix: switch Podman to rootful and reinstall:\n")
		ctx.Printf("  %s\n", infoGray.Render("podman machine set --rootful     # macOS"))
		ctx.Printf("  %s\n\n", infoGray.Render("# or run podman as root on Linux"))
	}
	ctx.Printf("Docker is rootful by default and works out of the box.\n\n")
	ctx.Printf("To install anyway (control plane only), re-run with --allow-rootless.\n")
	return fmt.Errorf("rootless %s is not supported for running app workloads", rt.bin)
}

// warnLowContainerMemory prints a heads-up (never blocks) when the runtime host
// has less memory than Miren recommends. It reads the runtime's own view of
// host memory (the VM on macOS/Windows), which is what the nested stack actually
// runs within — not the machine's total. Silent when memory can't be detected.
func warnLowContainerMemory(ctx *Context, rt containerRuntime) {
	mem := rt.hostMemoryBytes()
	if mem <= 0 {
		return
	}
	switch {
	case !meetsThreshold(mem, minMemoryBytes):
		ctx.Warn("%s has %s of memory available to containers, below the %s minimum.",
			rt.bin, formatBytes(mem), formatBytes(minMemoryBytes))
		fmt.Println("  Miren runs containerd, etcd, and buildkit inside the container — ~600 MB")
		fmt.Println("  at idle, spiking during builds — so deploys may fail under memory pressure.")
		fmt.Printf("  Raise the %s VM's memory (macOS/Windows) or free host memory (Linux).\n", rt.bin)
		fmt.Println("  More: https://miren.md/system-requirements")
		fmt.Println()
	case !meetsThreshold(mem, recommendedMemoryBytes):
		ctx.Warn("%s has %s of memory available to containers — it'll work, but we recommend %s.",
			rt.bin, formatBytes(mem), formatBytes(recommendedMemoryBytes))
		fmt.Println()
	}
}

// enginePortConflictError is the backstop for conflicts the pre-flight check
// couldn't see — most often an unprivileged CLI that can't test-bind ports
// below 1024 (binding returns EACCES, not EADDRINUSE), so the engine is the
// first to discover the clash. We match only the stable "port already taken"
// phrasings shared by Docker and Podman (not the exact port, which is phrased
// differently across versions and platforms) and turn them into friendly
// guidance. Returns (nil, false) for any other error so the caller falls back
// to generic handling.
func enginePortConflictError(ctx *Context, err error) (error, bool) {
	msg := err.Error()
	if !strings.Contains(msg, "address already in use") && !strings.Contains(msg, "Ports are not available") {
		return nil, false
	}

	// We don't parse the offending port out of the error: the engine's
	// bind-conflict phrasing varies across versions and rootful-vs-rootless, so
	// the precise, per-port guidance lives in the pre-flight (portConflict). This
	// fallback only fires when the pre-flight couldn't test-bind, so it stays
	// generic and names both levers without guessing which port clashed.
	ctx.Printf("\n%s A host port Miren needs (80, 443, or %d) is already in use.\n\n", infoRed.Render("✗"), defaultAPIPort)
	ctx.Printf("Free the port and re-run. If the conflict is on the HTTP port, move it\n")
	ctx.Printf("with --http-port. If a TLS-terminating reverse proxy (tailscale serve,\n")
	ctx.Printf("nginx, Caddy) already owns 443, run Miren behind it with\n")
	ctx.Printf("--ingress-mode behind-proxy-http instead:\n")
	ctx.Printf("  %s\n", infoGray.Render(requiredPortsHelpURL))
	return fmt.Errorf("a required host port is unavailable"), true
}

func (r containerRuntime) containerExists(name string) (bool, error) {
	cmd := r.command("ps", "-a", "-q", "-f", fmt.Sprintf("name=^%s$", name))
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

func (r containerRuntime) containerIsRunning(name string) (bool, error) {
	cmd := r.command("inspect", "-f", "{{.State.Running}}", name)
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) == "true", nil
}

// ingressArgs builds the `run` arguments that publish ports and configure the
// ingress mode. The published ports come straight from requiredHostPorts so
// they can't drift from what the pre-flight check verified. In behind-proxy
// modes the server otherwise binds its ingress to 127.0.0.1, which the engine's
// port publishing can't reach, so we pin it to 0.0.0.0 via env.
func ingressArgs(config containerConfig) []string {
	var args []string

	if config.HostNetwork {
		args = append(args, "--network", "host")
	} else {
		for _, p := range requiredHostPorts(config) {
			args = append(args, "-p", fmt.Sprintf("%d:%d/%s", p.hostPort, p.containerPort, p.proto))
		}
	}

	switch config.IngressMode {
	case serverconfig.IngressModeBehindProxyHTTP:
		args = append(args,
			"-e", "MIREN_INGRESS_MODE="+serverconfig.IngressModeBehindProxyHTTP,
			"-e", "MIREN_INGRESS_ADDRESS=0.0.0.0:80",
		)
	case serverconfig.IngressModeBehindProxyHTTPS:
		args = append(args,
			"-e", "MIREN_INGRESS_MODE="+serverconfig.IngressModeBehindProxyHTTPS,
			"-e", "MIREN_INGRESS_ADDRESS=0.0.0.0:443",
		)
	}

	return args
}

func (r containerRuntime) createContainer(name, image string, config containerConfig) (string, error) {
	args := []string{
		"run", "-d",
		"--name", name,
		"--init",
		"--privileged",
		"--restart", "always",
	}

	args = append(args, ingressArgs(config)...)

	if len(config.Labs) > 0 {
		args = append(args, "-e", fmt.Sprintf("MIREN_LABS=%s", strings.Join(config.Labs, ",")))
	}

	// Add volume and image
	args = append(args,
		"-v", fmt.Sprintf("%s:/var/lib/miren", config.VolumeName),
		image,
	)

	cmd := r.command(args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, exitErr.Stderr)
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func (r containerRuntime) startContainer(name string) error {
	return r.command("start", name).Run()
}

func (r containerRuntime) stopContainer(name string, timeout int) error {
	cmd := r.command("stop", "-t", fmt.Sprintf("%d", timeout), name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}

func (r containerRuntime) removeContainer(name string, force bool) error {
	// If force is requested, first try to stop gracefully, then force remove
	if force {
		// Check if container is running
		isRunning, err := r.containerIsRunning(name)
		if err == nil && isRunning {
			// Try graceful stop first (30 second timeout)
			_ = r.stopContainer(name, 30)
		}
	}

	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, name)

	cmd := r.command(args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}

func (r containerRuntime) removeVolume(name string) error {
	cmd := r.command("volume", "rm", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}

func (r containerRuntime) exec(containerName string, command []string) (string, error) {
	args := append([]string{"exec", containerName}, command...)
	cmd := r.command(args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, exitErr.Stderr)
		}
		return "", err
	}
	return string(output), nil
}

func waitForServerReady(ctx *Context) error {
	maxRetries := 30
	retryDelay := 2 * time.Second

	ctx.Log.Debug("checking server health", "host", "localhost", "max_retries", maxRetries)

	// Create HTTP3 client with InsecureSkipVerify since we're using self-signed certs
	client := &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 5 * time.Second,
	}
	defer client.CloseIdleConnections()

	healthURL := fmt.Sprintf("https://localhost:%d/healthz", defaultAPIPort)
	ctx.Log.Debug("health check endpoint", "url", healthURL)

	for i := range maxRetries {
		// Try to connect to the server's health endpoint via HTTP3
		reqCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, "GET", healthURL, nil)
		if err != nil {
			cancel()
			return fmt.Errorf("failed to create request: %w", err)
		}

		ctx.Log.Debug("attempting health check", "attempt", i+1, "max_retries", maxRetries)
		resp, err := client.Do(req)
		cancel()

		if err == nil {
			resp.Body.Close()
			ctx.Log.Info("server health check successful", "attempt", i+1)
			// Server responded to health check, it's ready
			return nil
		}

		// Check if it's a connection error vs server responding with error
		// If we get any response (even an error response), the server is up
		if resp != nil {
			resp.Body.Close()
			ctx.Log.Info("server responded with error but is running", "attempt", i+1)
			return nil
		}

		ctx.Log.Debug("health check failed", "attempt", i+1, "error", err)

		if i < maxRetries-1 {
			ctx.Info("Waiting for server... (attempts remaining: %d)", maxRetries-i-1)
			time.Sleep(retryDelay)
		}
	}

	ctx.Log.Error("server failed to become ready", "max_retries", maxRetries, "url", healthURL)
	return fmt.Errorf("timeout waiting for server to start")
}

func copyClientConfig(ctx *Context, rt containerRuntime, containerName string) error {
	ctx.Log.Debug("generating client config", "host", "localhost")

	// Generate client config by running miren auth generate
	target := fmt.Sprintf("localhost:%d", defaultAPIPort)
	ctx.Log.Debug("running auth generate", "target", target, "cluster", "local")
	output, err := rt.exec(containerName, []string{"miren", "auth", "generate", "-C", "local", "-t", target, "-c", "-"})
	if err != nil {
		ctx.Log.Error("failed to generate client config", "error", err)
		return fmt.Errorf("failed to generate client config: %w", err)
	}

	ctx.Log.Debug("auth generate output received", "output_length", len(output))

	if len(strings.TrimSpace(output)) == 0 {
		ctx.Log.Error("generated client config is empty")
		return fmt.Errorf("generated client config is empty")
	}

	// Parse the generated config to extract cluster information
	var generatedConfig clientconfig.ConfigData
	if err := yaml.Unmarshal([]byte(output), &generatedConfig); err != nil {
		ctx.Log.Error("failed to parse generated config", "error", err)
		return fmt.Errorf("failed to parse generated config: %w", err)
	}

	ctx.Log.Debug("parsed generated config", "clusters", len(generatedConfig.Clusters))

	// The cluster is named "local", matching the native install (server install)
	// and the server's own default cluster name, so a container-hosted server
	// looks the same to the client as a native one regardless of runtime.
	localCluster, ok := generatedConfig.Clusters["local"]
	if !ok {
		ctx.Log.Error("local cluster not found in generated config", "available_clusters", generatedConfig.Clusters)
		return fmt.Errorf("local cluster not found in generated config")
	}

	ctx.Log.Debug("found local cluster in config", "hostname", localCluster.Hostname)

	// Load existing user config
	config, err := clientconfig.LoadConfig()
	if err != nil {
		if !errors.Is(err, clientconfig.ErrNoConfig) {
			ctx.Log.Error("failed to load existing client config", "error", err)
			return fmt.Errorf("failed to load existing client config: %w", err)
		}
		ctx.Log.Warn("error loading existing client config, creating new one", "error", err)
		config = clientconfig.NewConfig()
	} else {
		ctx.Log.Debug("loaded existing client config", "active_cluster", config.ActiveCluster())
	}

	// Create leaf config data with the local cluster (saved to
	// clientconfig.d/50-local.yaml, same as the native install).
	leafConfigData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			"local": localCluster,
		},
	}
	ctx.Log.Debug("setting leaf config", "name", "50-local")
	config.SetLeafConfig("50-local", leafConfigData)

	// Migrate off the old "docker" cluster name. Earlier container installs
	// wrote a "docker" cluster (50-docker.yaml) and made it active. Point an
	// unset-or-"docker" active cluster at "local", then drop the stale "docker"
	// cluster so `-C docker` and its leaf don't linger. A user's own active
	// cluster (e.g. a cloud cluster) is left untouched. Order matters:
	// RemoveCluster refuses to remove the active cluster, so we re-point first.
	if active := config.ActiveCluster(); active == "" || active == "docker" {
		config.SetActiveCluster("local")
	}
	if config.HasCluster("docker") {
		if err := config.RemoveCluster("docker"); err != nil {
			ctx.Log.Warn("could not remove legacy docker cluster", "error", err)
		}
	}

	// Save the config
	ctx.Log.Debug("saving client config")
	if err := config.Save(); err != nil {
		ctx.Log.Error("failed to save local cluster leaf config", "error", err)
		return fmt.Errorf("failed to save local cluster leaf config: %w", err)
	}

	ctx.Log.Info("wrote local cluster config", "cluster", "local", "address", localCluster.Hostname)
	return nil
}

// removeInstalledClusters drops the client-config cluster a container install
// created, so uninstall is symmetric with install and doesn't leave a dead
// cluster pointing at a server that's gone. It removes the current "local" name
// and the legacy "docker" name (from pre-rename installs). If one is the active
// cluster, RemoveCluster would refuse it, so we clear the active pointer first.
// Best-effort: client-config problems are warned, never fatal, so the container
// uninstall itself still succeeds.
func removeInstalledClusters(ctx *Context) {
	config, err := clientconfig.LoadConfig()
	if err != nil {
		return
	}

	changed := false
	activeCleared := false
	for _, name := range []string{"local", "docker"} {
		if !config.HasCluster(name) {
			continue
		}
		if config.ActiveCluster() == name {
			config.ClearActiveCluster()
			activeCleared = true
		}
		if err := config.RemoveCluster(name); err != nil {
			ctx.Log.Warn("could not remove cluster during uninstall", "cluster", name, "error", err)
			continue
		}
		changed = true
	}

	if !changed {
		return
	}
	if err := config.Save(); err != nil {
		ctx.Log.Warn("could not save client config during uninstall", "error", err)
		return
	}
	if activeCleared {
		ctx.Info("The active cluster pointed at this server; cleared it. Select another with 'miren cluster switch <name>'.")
	}
}

type containerRegistrationOptions struct {
	ClusterName string
	CloudURL    string
	Tags        map[string]string
}

func (r containerRuntime) ensureVolume(volumeName string) error {
	// Check if volume exists
	if err := r.command("volume", "inspect", volumeName).Run(); err == nil {
		return nil
	}

	// Create volume
	cmd := r.command("volume", "create", volumeName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}

func performRegistrationPreStart(ctx *Context, rt containerRuntime, image, volumeName string, opts containerRegistrationOptions) error {
	// Determine cluster name
	clusterName := opts.ClusterName
	if clusterName == "" {
		// Use hostname
		hostname, err := os.Hostname()
		if err != nil {
			clusterName = "miren-cluster"
		} else {
			clusterName = hostname
		}
	}

	ctx.Info("Setting up cloud registration for cluster '%s'...", clusterName)

	// Create temporary directory for registration
	tempDir, err := os.MkdirTemp("", "miren-registration-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Perform registration using the Register function
	regOpts := RegisterOptions{
		ClusterName: clusterName,
		CloudURL:    opts.CloudURL,
		Tags:        opts.Tags,
		OutputDir:   tempDir,
	}

	if err := Register(ctx, regOpts); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Copy registration files to the volume. We use a temporary container to
	// copy files into the volume.
	registrationFile := filepath.Join(tempDir, "registration.json")
	if _, err := os.Stat(registrationFile); err != nil {
		return fmt.Errorf("registration file not found: %w", err)
	}

	ctx.Info("Copying registration to %s volume...", rt.bin)

	// Create a temporary container to copy files
	tempContainerName := fmt.Sprintf("miren-reg-copy-%d", time.Now().Unix())

	// Register cleanup before the run: a non-zero exit inside the container
	// still leaves the container object behind, so a failure below must not
	// leak a miren-reg-copy-<ts> container on the host.
	defer func() {
		if err := rt.removeContainer(tempContainerName, true); err != nil {
			ctx.Warn("Failed to remove temp container %s: %v", tempContainerName, err)
		}
	}()

	// Run container to create the directory structure (override entrypoint)
	runArgs := []string{
		"run", "--name", tempContainerName,
		"--entrypoint", "mkdir",
		"-v", fmt.Sprintf("%s:/var/lib/miren", volumeName),
		image,
		"-p", "/var/lib/miren/server",
	}

	if output, err := rt.command(runArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create directory in volume: %w: %s", err, output)
	}

	// Copy registration file to container
	cpArgs := []string{
		"cp",
		registrationFile,
		fmt.Sprintf("%s:/var/lib/miren/server/registration.json", tempContainerName),
	}

	if output, err := rt.command(cpArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy registration file: %w: %s", err, output)
	}

	ctx.Completed("Registration copied to volume")

	return nil
}
