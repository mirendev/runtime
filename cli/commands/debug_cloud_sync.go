package commands

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"miren.dev/runtime/api/debug/debug_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
)

// DebugCloudSync reports the runtime side of cloud entity synchronization.
func DebugCloudSync(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/debug-cloud-sync")
	if err != nil {
		return err
	}
	results, err := debug_v1alpha.NewCloudSyncClient(client).GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("get cloud sync status: %w", err)
	}
	if !results.HasReport() {
		return errors.New("cloud sync status response did not include a report")
	}
	report := results.Report()
	if opts.IsJSON() {
		return PrintJSON(report)
	}

	ctx.Printf("Cloud entity sync\n")
	ctx.Info("State: %s", report.State())
	if report.Summary() != "" {
		ctx.Info("Summary: %s", report.Summary())
	}
	for _, fact := range report.Facts() {
		ctx.Info("%s: %s", cloudSyncLabel(fact.Name()), fact.Value())
	}
	for _, event := range report.Events() {
		summary := event.Summary()
		if event.HasAt() {
			summary += ", " + standard.FromTimestamp(event.At()).Format(time.RFC3339)
		}
		ctx.Info("Last %s: %s", cloudSyncLabel(event.Kind()), summary)
	}
	return nil
}

func cloudSyncLabel(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '.' })
	for i, word := range words {
		if word == "id" {
			words[i] = "ID"
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}
