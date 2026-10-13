package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudapi"
)

type workbenchOptions struct {
	ConfigCentric
	App        string   `short:"a" long:"app" env:"MIREN_APP" description:"Application to focus on (optional)"`
	Identity   string   `short:"i" long:"identity" description:"Cloud identity (defaults to the cluster's identity)"`
	LocalTools bool     `long:"local-tools" description:"Allow read-only local source inspection; requested source content is sent to Cloud"`
	Dir        string   `short:"d" long:"dir" default:"." description:"Local source root for detective or --local-tools"`
	Question   []string `rest:"true" usage:"Question to ask; omit for interactive chat or piped input"`
}

const workbenchDescription = `Ask Workbench is a Miren Labs feature. Ask for access on Discord: https://miren.dev/discord

Ask the same cluster-scoped Workbench available in Miren Cloud. Requires a Cloud-registered cluster, a Cloud login, and Workbench enabled for the cluster's organization. Cluster selection follows the usual --cluster, MIREN_CLUSTER, per-app state, and active-cluster rules.

With no question, starts an interactive conversation; piped input is treated as one question. Pass -a to focus on an application. Responses stream as plain text. Ctrl-C aborts any active response and exits the CLI; it does not return to the chat prompt. Use /quit or Ctrl-D to exit interactive chat normally.

Detective enables read-only tools for listing, reading and searching source in the current directory (or -d). Workbench enables the same tools only with --local-tools. Requested file content is sent to Miren Cloud. There is no model-callable local shell or editing. In a Git repository, Git must be installed to resolve .git/info/exclude and the effective global excludes file, including included config and worktrees; failures refuse source access. Symlinks, gitignored files, most hidden paths, and common credential files are excluded; .miren/app.toml is eligible unless gitignored. These exclusions are not secret detection: choose a source-only directory and keep sensitive files out of it.

Local tools require the Cloud client-tool bridge. An unavailable bridge is an error, not a fallback that uploads your tree. Tools exist only while this CLI is connected; each invocation starts a new conversation.`

func Workbench(ctx *Context, opts workbenchOptions) error {
	return runWorkbench(ctx, opts)
}

func Detective(ctx *Context, opts workbenchOptions) error {
	opts.LocalTools = true
	return runWorkbench(ctx, opts)
}

func runWorkbench(ctx *Context, opts workbenchOptions) error {
	if ctx.clusterErr != nil {
		return ctx.unknownClusterError()
	}
	if ctx.ClusterConfig == nil || ctx.ClientConfig == nil {
		return fmt.Errorf("workbench needs a selected cluster; use --cluster or 'miren cluster switch'")
	}
	cluster := ctx.ClusterConfig
	if cluster.XID == "" {
		return fmt.Errorf("cluster %q has no Cloud ID; add a Cloud-registered cluster with 'miren cluster add'", ctx.ClusterName)
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
		return fmt.Errorf("workbench needs a Cloud login, not a certificate identity; run 'miren login'")
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
	if opts.LocalTools {
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
		absolute, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(ctx.Stderr, "Detective can read source under %s. Requested content is sent to Miren Cloud; no local writes or shell execution.\n", absolute)
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
			return fmt.Errorf("provide a question or run workbench in an interactive terminal")
		}
	}
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
			err := tools.Serve(runCtx, source.execute)
			if runCtx.Err() != nil {
				return nil
			}
			return err
		})
	}
	group.Go(func() error {
		defer stop()
		if !interactive {
			err := conversation.Ask(runCtx, question, uuid.NewString(), ctx.Stdout)
			ctx.Printf("\n")
			return err
		}
		return workbenchChat(runCtx, conversation, os.Stdin, ctx.Stdout, ctx.Stderr)
	})
	return group.Wait()
}

func workbenchChat(ctx context.Context, conversation *cloudapi.Workbench, input io.Reader, output, prompts io.Writer) error {
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
	fmt.Fprintln(prompts, "Ask Workbench. Type /quit or press Ctrl-D to exit.")
	for {
		fmt.Fprint(prompts, "> ")
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
			if err := conversation.Ask(ctx, question, uuid.NewString(), output); err != nil {
				return err
			}
			fmt.Fprintln(output)
		}
	}
}
