package commands

import (
	"encoding/json"
	"fmt"
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

// PrintJSONStdout prints data as formatted JSON to stdout
func PrintJSON(data interface{}) error {
	output, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(output))
	return nil
}
