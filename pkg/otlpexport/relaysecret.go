package otlpexport

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// LoadRelaySecret returns the secret the relay's callers must present,
// creating it at path the first time.
//
// It is kept on disk rather than minted per boot because it rides in
// buildkitd's env, and that env is part of a container spec that outlives a
// server restart; a new secret every boot would recreate the container every
// boot. A file that holds anything but a secret this function wrote, say one
// cut short by a crash, is replaced, which costs buildkitd one recreation.
func LoadRelaySecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if secret := strings.TrimSpace(string(data)); validRelaySecret(secret) {
			return secret, nil
		}
		if err := os.Remove(path); err != nil {
			return "", fmt.Errorf("removing unusable OTLP relay secret: %w", err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("reading OTLP relay secret: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", fmt.Errorf("creating OTLP relay secret directory: %w", err)
	}
	secret := rand.Text()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("creating OTLP relay secret: %w", err)
	}
	if _, err := f.WriteString(secret + "\n"); err != nil {
		f.Close()
		return "", fmt.Errorf("writing OTLP relay secret: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", fmt.Errorf("writing OTLP relay secret: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("writing OTLP relay secret: %w", err)
	}
	return secret, nil
}

// validRelaySecret accepts what rand.Text produces. The secret is written
// into an OTEL_EXPORTER_OTLP_*_HEADERS value, so anything outside that
// alphabet could break the header list apart.
func validRelaySecret(s string) bool {
	if len(s) < 26 {
		return false
	}
	return strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") == ""
}

// RelayHeaders is the OTEL_EXPORTER_OTLP_*_HEADERS value that authenticates
// an exporter to the relay.
func RelayHeaders(secret string) string {
	// Header values are percent-encoded in this variable, per the OTel spec.
	return "Authorization=Bearer%20" + secret
}

func relayAuthorized(r *http.Request, secret string) bool {
	if secret == "" {
		return false
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
}
