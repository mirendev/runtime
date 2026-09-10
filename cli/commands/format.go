package commands

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// FormatOptions provides common output formatting options
type FormatOptions struct {
	Format string `long:"format" description:"Output format (text, json)" default:"text"`
	JSON   bool   `long:"json" description:"Shorthand for --format json"`
}

// IsJSON returns true if JSON format is selected (case-insensitive)
func (f *FormatOptions) IsJSON() bool {
	return f.JSON || strings.EqualFold(f.Format, "json")
}

// IsJSONL returns true if JSON Lines format is selected (case-insensitive).
// Only commands that stream progress support it; the others treat it as an
// unknown format.
func (f *FormatOptions) IsJSONL() bool {
	return strings.EqualFold(f.Format, "jsonl")
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
