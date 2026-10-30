package commands

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestConfirmDeletionUnattended(t *testing.T) {
	for _, tt := range []struct {
		name                        string
		yes, force, allowUnattended bool
		confirmed, wantError        bool
		warning                     string
	}{
		{name: "requires yes", wantError: true},
		{name: "yes", yes: true, confirmed: true},
		{name: "force alias", force: true, confirmed: true, warning: "-f/--force"},
		{name: "force with yes still warns", yes: true, force: true, confirmed: true, warning: "-f/--force"},
		{name: "transition", allowUnattended: true, confirmed: true, warning: "--yes will be required next release"},
		{name: "transition with yes", yes: true, allowUnattended: true, confirmed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input, err := os.Open(os.DevNull)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			defer write.Close()
			stdout, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer stdout.Close()
			oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
			os.Stdin, os.Stdout, os.Stderr = input, stdout, write
			defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()

			confirmed, confirmErr := confirmDeletion("Delete resource?", tt.yes, tt.force, tt.allowUnattended)
			if confirmed != tt.confirmed || (confirmErr != nil) != tt.wantError {
				t.Fatalf("got (%v, %v), want confirmed=%v error=%v", confirmed, confirmErr, tt.confirmed, tt.wantError)
			}
			if tt.wantError && !strings.Contains(confirmErr.Error(), "--yes") {
				t.Fatalf("error must explain --yes: %v", confirmErr)
			}
			if err := write.Close(); err != nil {
				t.Fatal(err)
			}
			output, err := io.ReadAll(read)
			if err != nil {
				t.Fatal(err)
			}
			if tt.warning == "" && len(output) != 0 || tt.warning != "" && !strings.Contains(string(output), tt.warning) {
				t.Fatalf("unexpected warning: %q (want %q)", output, tt.warning)
			}
			info, err := stdout.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != 0 {
				t.Fatal("confirmation output contaminated stdout")
			}
		})
	}
}
