package commands

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/pkg/progress/upload"
	"miren.dev/runtime/pkg/rpc/stream"
)

const diskBackupService = "dev.miren.runtime/disk-backup"

// diskProgress renders the server's progress events for backup and restore.
//
// Disk images are large enough that a silent wait is indistinguishable from a
// hang, so transfer events get a line that updates in place on a terminal. Off a
// terminal the same events are printed one per line and throttled, so a log does
// not fill with thousands of near-identical rows.
func diskProgress(ctx *Context) stream.SendStream[*disk_v1alpha.Progress] {
	tty := term.IsTerminal(int(os.Stdout.Fd()))
	var lastLine time.Time
	inPlace := false

	// Clear a partially written in-place line before printing anything else, so
	// a message never lands on top of a progress bar.
	clear := func() {
		if inPlace {
			fmt.Fprint(os.Stdout, "\r\033[K")
			inPlace = false
		}
	}

	return stream.Callback(func(p *disk_v1alpha.Progress) error {
		switch p.Update().Which() {
		case "message":
			clear()
			ctx.Info("%s", p.Update().Message())

		case "warning":
			clear()
			ctx.Warn("%s", p.Update().Warning())

		case "error":
			clear()
			ctx.Warn("%s", p.Update().Error())

		case "transfer":
			t := p.Update().Transfer()
			if t == nil {
				return nil
			}
			line := formatTransfer(t)

			if tty {
				fmt.Fprintf(os.Stdout, "\r\033[K  %s", line)
				inPlace = true
				return nil
			}
			// One line per half second is enough to show life in a log.
			if now := time.Now(); now.Sub(lastLine) >= 500*time.Millisecond {
				lastLine = now
				ctx.Info("  %s", line)
			}
		}
		return nil
	})
}

func formatTransfer(t *disk_v1alpha.Transfer) string {
	moved := upload.FormatBytes(t.Done())
	speed := upload.FormatSpeed(float64(t.BytesPerSecond()))

	if t.Total() <= 0 {
		return fmt.Sprintf("%s at %s", moved, speed)
	}

	pct := float64(t.Done()) / float64(t.Total()) * 100
	out := fmt.Sprintf("%s of %s (%.0f%%) at %s",
		moved, upload.FormatBytes(t.Total()), pct, speed)
	if eta := t.EtaSeconds(); eta > 0 {
		out += fmt.Sprintf(", %s left", upload.FormatDuration(time.Duration(eta)*time.Second))
	}
	return out
}
