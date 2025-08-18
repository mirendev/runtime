package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progresswriter"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/progress/upload"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/tarx"
)

func Deploy(ctx *Context, opts struct {
	AppCentric

	Explain       bool   `short:"x" long:"explain" description:"Explain the build process"`
	ExplainFormat string `long:"explain-format" description:"Explain format" choice:"auto" choice:"plain" choice:"tty" choice:"rawjson" default:"auto"`
}) error {
	cl, err := ctx.RPCClient("dev.miren.runtime/build")
	if err != nil {
		return err
	}

	bc := build_v1alpha.NewBuilderClient(cl)

	name := opts.App
	dir := opts.Dir

	ctx.Log.Info("building code", "name", name, "dir", dir)

	// Load AppConfig to get include patterns
	var includePatterns []string
	ac, err := appconfig.LoadAppConfigUnder(dir)
	if err != nil {
		return err
	}
	if ac != nil && ac.Include != nil {
		// Validate patterns before using them
		for _, pattern := range ac.Include {
			if err := tarx.ValidatePattern(pattern); err != nil {
				return fmt.Errorf("invalid include pattern %q: %w", pattern, err)
			}
		}
		includePatterns = ac.Include
	}

	r, err := tarx.MakeTar(dir, includePatterns)
	if err != nil {
		return err
	}

	var (
		cb      stream.SendStream[*build_v1alpha.Status]
		results *build_v1alpha.BuilderClientBuildFromTarResults
	)

	var buildErrors []string
	var buildLogs []string

	if opts.Explain {
		// In explain mode, write to stderr
		pw, err := progresswriter.NewPrinter(ctx, os.Stderr, opts.ExplainFormat)
		if err != nil {
			return err
		}

		// Add upload progress tracking in explain mode
		uploadStartTime := time.Now()
		var uploadBytes int64
		var lastPrintTime time.Time

		progressReader := upload.NewProgressReader(r, func(progress upload.Progress) {
			uploadBytes = progress.BytesRead
			// Print progress every 500ms to avoid spamming
			if time.Since(lastPrintTime) >= 500*time.Millisecond {
				lastPrintTime = time.Now()
				fmt.Fprintf(os.Stderr, "\r\033[K") // Clear to end of line
				fmt.Fprintf(os.Stderr, "Uploading artifacts: %s at %s",
					upload.FormatBytes(progress.BytesRead),
					upload.FormatSpeed(progress.BytesPerSecond))
			}
		})
		r = progressReader

		// Progress handler for explain mode
		progressHandler := func(status *client.SolveStatus) error {
			// Clear the upload progress line when buildkit starts
			if uploadBytes > 0 {
				uploadDuration := time.Since(uploadStartTime)
				avgSpeed := float64(uploadBytes) / uploadDuration.Seconds()
				fmt.Fprintf(os.Stderr, "\rUpload complete: %s in %.1fs at %s\n",
					upload.FormatBytes(uploadBytes),
					uploadDuration.Seconds(),
					upload.FormatSpeed(avgSpeed))
				uploadBytes = 0 // Only print once
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case pw.Status() <- status:
				// ok
			}
			return nil
		}

		cb = createBuildStatusCallback(ctx, nil, nil, nil, &buildErrors, nil, progressHandler)

		results, err = bc.BuildFromTar(ctx, name, stream.ServeReader(ctx, r), cb)
		if err != nil {
			ctx.Printf("\n\nBuild failed with the following errors:\n")
			printBuildErrors(ctx, buildErrors, nil)
			return err
		}

		close(pw.Status())
		<-pw.Done()

		if pw.Err() != nil {
			return pw.Err()
		}
	} else {
		var (
			updateCh         = make(chan string, 1)
			transferCh       = make(chan transferUpdate, 1)
			uploadProgressCh = make(chan upload.Progress, 1)
			transfers        = map[string]transfer{}
			wg               sync.WaitGroup
		)

		defer wg.Wait()

		progressReader := upload.NewProgressReader(r, func(progress upload.Progress) {
			select {
			case uploadProgressCh <- progress:
			default:
			}
		})
		r = progressReader

		// Create a context that can be cancelled
		deployCtx, cancelDeploy := context.WithCancel(ctx)
		defer cancelDeploy()

		model := initialModel(updateCh, transferCh, uploadProgressCh)
		p := tea.NewProgram(model)

		var finalModel tea.Model
		var runErr error

		wg.Add(1)
		go func() {
			defer wg.Done()
			finalModel, runErr = p.Run()
			if runErr == nil {
				// Check if we exited due to interrupt or timeout
				if dm, ok := finalModel.(*deployInfo); ok && (dm.interrupted || dm.currentPhase == "timeout") {
					cancelDeploy() // Cancel the deployment context
				}
			} else {
				// UI died; ensure we don't keep uploading/building
				cancelDeploy()
			}
		}()

		defer p.Quit()

		// Progress handler for interactive mode
		progressHandler := func(status *client.SolveStatus) error {
			p.Send(status)
			return nil
		}

		cb = createBuildStatusCallback(deployCtx, updateCh, transferCh, transfers, &buildErrors, &buildLogs, progressHandler)

		results, err = bc.BuildFromTar(deployCtx, name, stream.ServeReader(deployCtx, r), cb)

		// Ensure the progress UI is shut down before printing
		p.Quit()
		wg.Wait()

		// Get the final model to extract phase summaries
		if m, ok := finalModel.(*deployInfo); ok && m.currentPhase == "buildkit" && err == nil {
			// Complete the buildkit phase if it's still running and we succeeded
			duration := time.Since(m.phaseStart)
			buildPhase := phaseSummary{
				name:     "Build & push image",
				duration: duration,
				details:  fmt.Sprintf("%d layers processed", m.parts),
			}

			// Only print the final build phase summary (TEA UI already showed the others)
			ctx.Printf("%s\n", renderPhaseSummary(buildPhase))
		}

		if err != nil {
			// Check if this was a context cancellation (from timeout or interrupt)
			if deployCtx.Err() != nil {
				ctx.Printf("\n\n❌ Deploy cancelled.\n")
				return deployCtx.Err()
			}

			ctx.Printf("\n\nBuild failed.\n")
			printBuildErrors(ctx, buildErrors, buildLogs)
			return err
		}

	}

	if results.Version() == "" {
		ctx.Printf("\n\nError detected in building %s. No version returned.\n", name)
		printBuildErrors(ctx, buildErrors, buildLogs)
		return fmt.Errorf("build failed: no version returned")
	}

	ctx.Printf("\n\nUpdated version %s deployed. All traffic moved to new version.\n", results.Version())

	return nil
}

// Helper function to print build errors and logs
func printBuildErrors(ctx *Context, buildErrors []string, buildLogs []string) {
	if len(buildErrors) > 0 {
		ctx.Printf("\nErrors:\n")
		for _, errMsg := range buildErrors {
			ctx.Printf("  - %s\n", errMsg)
		}
	}

	if len(buildLogs) > 0 {
		ctx.Printf("\nBuild output:\n")
		for _, log := range buildLogs {
			ctx.Printf("%s\n", log)
		}
	}
}


// createBuildStatusCallback creates a callback for handling build status updates
func createBuildStatusCallback(
	ctx context.Context,
	updateCh chan<- string,
	transferCh chan<- transferUpdate,
	transfers map[string]transfer,
	buildErrors *[]string,
	buildLogs *[]string,
	progressHandler func(*client.SolveStatus) error,
) stream.SendStream[*build_v1alpha.Status] {
	return stream.Callback(func(su *build_v1alpha.Status) error {
		update := su.Update()

		switch update.Which() {
		case "buildkit":
			sj := update.Buildkit()

			var status client.SolveStatus
			if err := json.Unmarshal(sj, &status); err != nil {
				return err
			}

			// Handle transfers if we have a transfer channel
			if transferCh != nil {
				var updated bool
				for _, st := range status.Statuses {
					if st.Total != 0 {
						updated = true
						transfers[st.ID] = transfer{total: st.Total, current: st.Current}
					}
				}

				if updated {
					select {
					case <-ctx.Done():
						// UI/operation cancelled, drop the update
					case transferCh <- transferUpdate{transfers: transfers}:
						// ok
					default:
						// channel full, drop to avoid blocking
					}
				}
			}

			// Call the progress handler if provided
			if progressHandler != nil {
				if err := progressHandler(&status); err != nil {
					return err
				}
			}

			// Extract error messages from status
			for _, vertex := range status.Vertexes {
				if vertex.Error != "" {
					*buildErrors = append(*buildErrors, vertex.Error)
				}
			}

			// Collect all logs for potential output on failure
			if buildLogs != nil {
				for _, log := range status.Logs {
					if log.Data != nil {
						logStr := strings.TrimSpace(string(log.Data))
						if logStr != "" {
							*buildLogs = append(*buildLogs, logStr)
						}
					}
				}
			}

			// Fail the build if we detected any errors
			if len(*buildErrors) > 0 {
				return fmt.Errorf("build failed with %d error(s)", len(*buildErrors))
			}

			return nil
		case "message":
			msg := update.Message()
			if updateCh != nil {
				select {
				case updateCh <- msg:
					// sent successfully
				default:
					// drop if UI isn't consuming
				}
			}
		case "error":
			*buildErrors = append(*buildErrors, update.Error())
		}

		return nil
	})
}
