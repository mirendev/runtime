package serverconfig

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// GenerateTOML generates a TOML configuration file from a Config struct
func GenerateTOML(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}

	buf := new(bytes.Buffer)
	encoder := toml.NewEncoder(buf)
	encoder.SetIndentTables(true)

	if err := encoder.Encode(cfg); err != nil {
		return nil, fmt.Errorf("failed to encode config to TOML: %w", err)
	}

	return buf.Bytes(), nil
}

// GenerateTOMLWithComments generates a TOML configuration file with helpful comments
func GenerateTOMLWithComments(cfg *Config) ([]byte, error) {
	// For now, just use the regular generator
	// TODO: Add comments manually by building the TOML string
	return GenerateTOML(cfg)
}
