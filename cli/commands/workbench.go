package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudapi"
)

type workbenchOptions struct {
	ConfigCentric
	App          string   `short:"a" long:"app" env:"MIREN_APP" description:"Application to focus on (defaults to the local app, if any)"`
	Identity     string   `short:"i" long:"identity" description:"Cloud identity (defaults to the cluster's identity)"`
	NoLocalTools bool     `long:"no-local-tools" description:"Disable local source access; ask about the cluster without sharing local files"`
	Dir          string   `short:"d" long:"dir" default:"." description:"Local source root"`
	Question     []string `rest:"true" usage:"Question to ask; omit for interactive chat or piped input"`
}

const workbenchDescription = `Ask Workbench is a Miren Labs feature. Ask for access on Discord: https://miren.dev/discord

Ask the same cluster-scoped Workbench available in Miren Cloud. Requires a Cloud-registered cluster, a Cloud login, and Workbench enabled for the cluster's organization. Cluster selection follows the usual --cluster, MIREN_CLUSTER, per-app state, and active-cluster rules.

With no question, starts an interactive conversation; piped input is treated as one question. Defaults to the app in the current directory or a parent directory, or in -d when specified. Pass -a or MIREN_APP to override the app. Outside an app directory, asks about the cluster. Terminal responses render Markdown with a waiting indicator; redirected responses stream as plain text. Local tool calls are shown as they happen. Ctrl-C aborts any active response and exits the CLI; it does not return to the chat prompt. Use /quit or Ctrl-D to exit interactive chat normally.

Cloud write requests show their summary, arguments, and consequences for approval in the terminal, including when a positional question is supplied. Use arrow keys and Enter to allow once, allow for this conversation, or deny (the default). Chat text and pasted text never grant permission. Inline approval requires terminal input and output; otherwise writes remain gated and Cloud's browser link is displayed.

Detective enables read-only tools for listing, reading and searching source in the current directory (or -d). Use --no-local-tools to ask about the cluster without exposing local source files. Requested file content is sent to Miren Cloud. There is no model-callable local shell or editing. In a Git repository, Git must be installed to resolve .git/info/exclude and the effective global excludes file, including included config and worktrees; failures refuse source access. Symlinks, gitignored files, most hidden paths, and common credential files are excluded; .miren/app.toml is eligible unless gitignored. These exclusions are not secret detection: choose a source-only directory and keep sensitive files out of it.

Local tools require the Cloud client-tool bridge. An unavailable bridge is an error, not a fallback that uploads your tree. Tools exist only while this CLI is connected; each invocation starts a new conversation.`

func Detective(ctx *Context, opts workbenchOptions) error {
	if ctx.clusterErr != nil {
		return ctx.unknownClusterError()
	}
	if ctx.ClusterConfig == nil || ctx.ClientConfig == nil {
		return fmt.Errorf("detective needs a selected cluster; use --cluster or 'miren cluster switch'")
	}
	cluster := ctx.ClusterConfig
	if cluster.XID == "" {
		return fmt.Errorf("cluster %q has no Cloud ID; add a Cloud-registered cluster with 'miren cluster add'", ctx.ClusterName)
	}
	if opts.App == "" {
		var ac *appconfig.AppConfig
		if opts.Dir != "" && opts.Dir != "." {
			var err error
			ac, err = appconfig.LoadAppConfigUnder(opts.Dir)
			if err != nil {
				printConfigWarning(err)
				ac = nil
			}
		} else {
			ac = loadLocalAppConfigOrWarn()
		}
		if ac != nil {
			opts.App = ac.Name
		}
	}
	identityName := opts.Identity
	if identityName == "" {
		identityName = cluster.Identity
	}
	restore := ctx.ProgressToStderr()
	identityName, err := pickCloudIdentity(ctx, ctx.ClientConfig, identityName, "")
	restore()
	if err != nil {
		return err
	}
	identity, err := lookupIdentity(ctx.ClientConfig, identityName)
	if err != nil {
		return err
	}
	if identity.Type == clientconfig.IdentityCertificate {
		return fmt.Errorf("detective needs a Cloud login, not a certificate identity; run 'miren login'")
	}
	cloudURL := cluster.CloudURL
	if cloudURL == "" {
		cloudURL = identity.Issuer
	}
	client, err := cloudapi.New(cloudURL, func(c context.Context) (string, error) {
		return ctx.ClientConfig.TokenForIdentity(c, identityName, identity, cloudURL)
	})
	if err != nil {
		return err
	}

	var source *workbenchSource
	if !opts.NoLocalTools {
		dir := opts.Dir
		if dir == "" {
			dir = "."
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			return fmt.Errorf("opening local source root: %w", err)
		}
		defer root.Close()
		source = &workbenchSource{root: root}
	}

	question := strings.TrimSpace(strings.Join(opts.Question, " "))
	interactive := question == "" && term.IsTerminal(int(os.Stdin.Fd()))
	if question == "" && !interactive {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20+1))
		if err != nil {
			return err
		}
		if len(data) > 1<<20 {
			return fmt.Errorf("question exceeds 1 MiB")
		}
		question = strings.TrimSpace(string(data))
		if question == "" {
			return fmt.Errorf("provide a question or run detective in an interactive terminal")
		}
	}
	display := &workbenchDisplay{output: ctx.Stdout, status: ctx.Stderr}
	display.start("Connecting to Miren Cloud")
	defer display.finish()
	conversation, err := client.OpenWorkbench(ctx, cluster.XID, "cli-"+uuid.NewString(), opts.App)
	if err != nil {
		return err
	}
	group, runCtx := errgroup.WithContext(ctx)
	runCtx, stop := context.WithCancel(runCtx)
	defer stop()
	if source != nil {
		tools, err := conversation.ConnectTools(runCtx, uuid.NewString(), source.tools())
		if err != nil {
			return err
		}
		defer tools.Close()
		group.Go(func() error {
			err := tools.Serve(runCtx, func(ctx context.Context, name string, input json.RawMessage) (string, error) {
				display.tool(name, input)
				return source.execute(ctx, name, input)
			})
			if runCtx.Err() != nil {
				return nil
			}
			return err
		})
	}
	if err := display.finish(); err != nil {
		return err
	}
	group.Go(func() error {
		defer stop()
		if term.IsTerminal(int(os.Stdin.Fd())) && isInteractiveOutput(display.output) && isInteractiveOutput(display.status) {
			return display.run(runCtx, conversation, os.Stdin, question, interactive)
		}
		if interactive {
			return workbenchChat(runCtx, conversation, os.Stdin, display)
		}
		return display.ask(runCtx, conversation, question)
	})
	return group.Wait()
}

// Plain-output chat has no input-enabled Bubble Tea program or approvals.
func workbenchChat(ctx context.Context, conversation *cloudapi.Workbench, input io.Reader, display *workbenchDisplay) error {
	lines := make(chan string)
	readErrors := make(chan error, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		readErrors <- scanner.Err()
	}()
	fmt.Fprintln(display.status, "Ask Workbench. Type /quit or press Ctrl-D to exit.")
	for {
		fmt.Fprint(display.status, "> ")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case question, ok := <-lines:
			if !ok {
				return <-readErrors
			}
			question = strings.TrimSpace(question)
			if question == "/quit" {
				return nil
			}
			if question == "" {
				continue
			}
			if err := display.ask(ctx, conversation, question); err != nil {
				return err
			}
		}
	}
}
