package commands

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// FormatOptions provides common output formatting options
type FormatOptions struct {
	Format string `long:"format" description:"Output format (table, json)" default:"table"`
}

// IsJSON returns true if JSON format is selected (case-insensitive)
func (f *FormatOptions) IsJSON() bool {
	return strings.EqualFold(f.Format, "json")
}

// PrintJSON prints data as formatted JSON to stdout
func PrintJSON(data any) error {
	return PrintJSONTo(os.Stdout, data)
}

// PrintJSONTo prints JSON to the given writer with pretty formatting.
func PrintJSONTo(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(data)
}
