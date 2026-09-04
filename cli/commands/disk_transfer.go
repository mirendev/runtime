package commands

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"miren.dev/runtime/pkg/cond"
)

const (
	// transferAttempts is how many times a backup or restore will pick itself
	// up after the connection drops.
	//
	// This is not a tight retry loop around a flaky call — each attempt resumes
	// from where the last one stopped, so the work already done is kept and the
	// cost of another attempt is only the time to reconnect. Six covers a
	// cluster uplink that flaps repeatedly during one long transfer, which is
	// the case that motivated resume in the first place.
	transferAttempts = 6

	// transferRetryDelay is the wait before picking a transfer back up. A
	// cluster's link to the cloud can take up to a minute to come back
	// (RFD-101), so this backs off toward that rather than hammering.
	transferRetryDelay = 5 * time.Second
	transferRetryMax   = 60 * time.Second
)

// newTransferID names one backup or restore so an interrupted one can be
// resumed.
//
// The client picks it, not the server: an id the server handed back would be
// lost along with the call that carried it, which is precisely the call that
// failed.
func newTransferID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating a transfer id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// resumable reports whether an error is worth another attempt.
//
// A refusal is not. The server declining to overwrite a mounted disk, or
// reporting that no such disk exists, means the same thing on every attempt,
// and retrying it six times only makes the operator wait longer to read it.
func resumable(err error) bool {
	if err == nil {
		return false
	}

	var validation cond.ErrValidationFailure
	var notFound cond.ErrNotFound
	return !errors.As(err, &validation) && !errors.As(err, &notFound)
}

// runTransfer calls attempt until it succeeds, the error turns out not to be
// worth retrying, or the attempts run out.
//
// Each attempt is expected to resume rather than restart: this loop exists to
// survive a dropped connection, not to paper over a call that fails the same
// way every time.
func runTransfer(ctx *Context, what string, attempt func(try int) error) error {
	delay := transferRetryDelay

	for try := 1; ; try++ {
		err := attempt(try)
		if err == nil {
			return nil
		}
		if !resumable(err) || try >= transferAttempts {
			return err
		}
		// A cancelled command is the operator's decision, not a failure to
		// retry around.
		if ctx.Err() != nil {
			return err
		}

		ctx.Warn("%s was interrupted: %v", what, err)
		ctx.Info("Picking up where it stopped in %s (attempt %d of %d)", delay, try+1, transferAttempts)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return err
		}

		if delay *= 2; delay > transferRetryMax {
			delay = transferRetryMax
		}
	}
}
