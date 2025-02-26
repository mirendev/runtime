package fsmon

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type FileAccess struct {
	ProcessName string
	PID         string
	User        string
	FilePath    string
}

func AccessUnder(devPath string) ([]FileAccess, error) {
	// Get absolute path
	absPath, err := filepath.Abs(devPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %v", err)
	}

	// Run lsof command
	cmd := exec.Command("lsof", absPath)
	output, err := cmd.Output()
	if err != nil {
		// lsof returns error if no files are open
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []FileAccess{}, nil
		}
		return nil, fmt.Errorf("failed to execute lsof: %v", err)
	}

	// Parse output
	lines := strings.Split(string(output), "\n")

	if len(lines) < 2 { // Check if we have data (header + at least one line)
		return []FileAccess{}, nil
	}

	var results []FileAccess
	// Skip header line (first line)
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}

		// Split line into fields
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		access := FileAccess{
			ProcessName: fields[0],
			PID:         fields[1],
			User:        fields[2],
			FilePath:    fields[8],
		}
		results = append(results, access)
	}

	return results, nil
}
