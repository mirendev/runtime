package query

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

func readGPUs(ctx context.Context, name string) ([]GPUInfo, error) {
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,uuid,name,temperature.gpu,utilization.gpu,memory.used,memory.total,power.draw",
		"--format=csv,noheader,nounits")
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("query Nvidia GPUs with nvidia-smi: %w", err)
	}
	return parseGPUs(data, name)
}

func parseGPUs(data []byte, name string) ([]GPUInfo, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse Nvidia GPU output: %w", err)
	}
	result := make([]GPUInfo, 0, len(rows))
	for _, row := range rows {
		if len(row) != 8 {
			return nil, fmt.Errorf("unexpected Nvidia GPU output: %d fields", len(row))
		}
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		index, err := strconv.Atoi(row[0])
		if err != nil || row[1] == "" || row[2] == "" {
			return nil, fmt.Errorf("invalid Nvidia GPU identity %q", row[0])
		}
		if !processNameMatches(name, row[2]) {
			continue
		}
		gpu := GPUInfo{Index: index, UUID: row[1], Name: row[2]}
		for _, field := range []struct {
			value   string
			float   **float64
			integer **uint64
		}{
			{row[3], &gpu.Temperature, nil}, {row[4], &gpu.Utilization, nil},
			{row[5], nil, &gpu.MemoryUsed}, {row[6], nil, &gpu.MemoryTotal},
			{row[7], &gpu.Power, nil},
		} {
			if field.value == "N/A" || field.value == "[N/A]" || field.value == "" {
				continue
			}
			if field.float != nil {
				value, err := strconv.ParseFloat(field.value, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid Nvidia GPU metric %q: %w", field.value, err)
				}
				*field.float = &value
			} else {
				value, err := strconv.ParseUint(field.value, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid Nvidia GPU memory %q: %w", field.value, err)
				}
				*field.integer = &value
			}
		}
		result = append(result, gpu)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Index < result[j].Index })
	return result, nil
}
