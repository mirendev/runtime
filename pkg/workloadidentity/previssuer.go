package workloadidentity

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Files, under the server data directory, that track the cluster's identity
// anchor across restarts.
//
//   - CurrentIssuerFile is the anchor the last run minted under. Comparing it
//     to the anchor this run resolved is how a move is detected, and the server
//     is the only thing that knows both — which is why the transition is
//     recorded here rather than by whatever flipped the setting.
//   - PrevIssuerFile is an anchor left behind by a move, still accepted for
//     verification until the tokens carrying it have expired.
const (
	CurrentIssuerFile = "workload-identity.issuer"
	PrevIssuerFile    = "workload-identity.prev-issuer"
)

// issuerOverlap is how long a superseded anchor keeps verifying: the maximum
// token lifetime plus an hour, so the window outlives any token that could
// still have been minted under it.
//
// Moving the anchor changes the iss claim of every token minted from then on.
// Tokens already in circulation carry the old one, and they have to keep
// verifying or the move takes out the cluster's own services — the local
// registry, runner telemetry, and every in-cluster API call — until the last
// one expires. Rewriting the mounted token files covers a workload that
// re-reads them; an app that read its token once and held it in memory is
// beyond reach, and this window is what carries it.
const issuerOverlap = MaxTTL + time.Hour

// prevIssuer is the on-disk record of a superseded anchor.
type prevIssuer struct {
	Issuer string `json:"issuer"`
	// NotAfter is when the last token minted under Issuer can no longer be
	// valid. Past it the record is ignored, so a long-dead anchor cannot be
	// resurrected by a stale file.
	NotAfter time.Time `json:"not_after"`
}

// trackAnchorMove reconciles the anchor this run resolved against the one the
// last run used, and returns the superseded anchor to keep accepting (empty if
// the anchor did not move or its overlap has lapsed).
//
// Called during NewIssuer, which already owns writing to this directory.
func trackAnchorMove(serverDir, issuerURL string, now time.Time) (string, error) {
	currentPath := filepath.Join(serverDir, CurrentIssuerFile)
	prevPath := filepath.Join(serverDir, PrevIssuerFile)

	last, err := readIssuerFile(currentPath)
	if err != nil {
		return "", err
	}

	if last != "" && last != issuerURL {
		// The anchor moved across this restart. Record what it moved from
		// before anything is minted under the new one.
		if err := writePrevIssuer(prevPath, last, now); err != nil {
			return "", err
		}
		slog.Info("workload identity anchor moved",
			"from", last, "to", issuerURL,
			"accepting_previous_until", now.Add(issuerOverlap))
	}

	if last != issuerURL {
		if err := writeIssuerFile(currentPath, issuerURL); err != nil {
			return "", err
		}
	}

	record, err := readPrevIssuer(prevPath, now)
	if err != nil {
		return "", err
	}
	if record == nil || record.Issuer == issuerURL {
		return "", nil
	}
	return record.Issuer, nil
}

// writePrevIssuer records a superseded anchor.
//
// There is one slot. Moving the anchor twice inside a single overlap window
// therefore drops the middle value, and tokens carrying it stop verifying —
// worth a warning, but not worth refusing a move over, since the alternative is
// a cluster stuck on an anchor it is trying to leave.
func writePrevIssuer(path, issuer string, now time.Time) error {
	if existing, err := readPrevIssuer(path, now); err != nil {
		return err
	} else if existing != nil && existing.Issuer != issuer {
		slog.Warn("workload identity anchor moved twice inside one overlap window; "+
			"tokens still carrying the earliest anchor will stop verifying",
			"dropped", existing.Issuer, "keeping", issuer)
	}

	record, err := json.Marshal(prevIssuer{Issuer: issuer, NotAfter: now.Add(issuerOverlap)})
	if err != nil {
		return fmt.Errorf("encoding previous issuer: %w", err)
	}
	if err := os.WriteFile(path, record, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// readPrevIssuer returns the recorded anchor, or nil when there is none, it has
// lapsed, or the file is unreadable.
//
// A malformed record is treated as absent rather than fatal: it can only widen
// what verifies, so losing it costs an overlap, while refusing to boot over it
// would strand a cluster on a file that is not load-bearing.
func readPrevIssuer(path string, now time.Time) (*prevIssuer, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var record prevIssuer
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, nil
	}
	if record.Issuer == "" || !now.Before(record.NotAfter) {
		return nil, nil
	}
	return &record, nil
}

// readIssuerFile returns the recorded anchor, or "" when the cluster has not
// recorded one yet (a first boot, or an upgrade from before this existed).
func readIssuerFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), nil
}

func writeIssuerFile(path, issuer string) error {
	if err := os.WriteFile(path, []byte(issuer), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
