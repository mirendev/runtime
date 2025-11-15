package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/cover"
)

type ChangedLineRange struct {
	File      string
	StartLine int
	EndLine   int
}

type FileCoverage struct {
	File            string
	CoveredStmts    int64
	TotalStmts      int64
	CoveragePercent float64
}

func main() {
	var (
		coverFile   = flag.String("coverage", "coverage.out", "Path to coverage profile")
		baseBranch  = flag.String("base", "main", "Base branch to compare against")
		minCoverage = flag.Float64("min", 0, "Minimum coverage threshold (0-100)")
		verbose     = flag.Bool("v", false, "Verbose output (show all files)")
		summary     = flag.Bool("summary", false, "Show only summary (no per-file breakdown)")
	)
	flag.Parse()

	// Parse coverage data
	profiles, err := cover.ParseProfiles(*coverFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing coverage profile: %v\n", err)
		os.Exit(1)
	}

	// Get changed line ranges from git diff
	changedRanges, err := getChangedLineRanges(*baseBranch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting changed lines: %v\n", err)
		os.Exit(1)
	}

	if len(changedRanges) == 0 {
		fmt.Println("No Go files changed compared to", *baseBranch)
		os.Exit(0)
	}

	// Build index: file -> []ChangedLineRange
	changedByFile := make(map[string][]ChangedLineRange)
	for _, change := range changedRanges {
		changedByFile[change.File] = append(changedByFile[change.File], change)
	}

	// Filter coverage to only changed lines
	fileCoverageMap := make(map[string]*FileCoverage)

	for _, profile := range profiles {
		// Normalize file path (remove module prefix)
		normalizedPath := normalizeFilePath(profile.FileName)

		// Skip if file not changed
		changes, hasChanges := changedByFile[normalizedPath]
		if !hasChanges {
			continue
		}

		// Initialize file coverage if needed
		if _, exists := fileCoverageMap[normalizedPath]; !exists {
			fileCoverageMap[normalizedPath] = &FileCoverage{
				File: normalizedPath,
			}
		}

		// Check each coverage block
		for _, block := range profile.Blocks {
			// Check if block overlaps with any changed line range
			if overlapsAnyRange(block, changes) {
				fileCoverageMap[normalizedPath].TotalStmts += int64(block.NumStmt)
				if block.Count > 0 {
					fileCoverageMap[normalizedPath].CoveredStmts += int64(block.NumStmt)
				}
			}
		}
	}

	// Calculate percentages and create sorted list
	var files []FileCoverage
	var totalCovered, totalStmts int64

	for _, fc := range fileCoverageMap {
		if fc.TotalStmts > 0 {
			fc.CoveragePercent = float64(fc.CoveredStmts) / float64(fc.TotalStmts) * 100
		}
		files = append(files, *fc)
		totalCovered += fc.CoveredStmts
		totalStmts += fc.TotalStmts
	}

	// Sort by filename
	sort.Slice(files, func(i, j int) bool {
		return files[i].File < files[j].File
	})

	// Print results
	if !*summary {
		fmt.Printf("Coverage for Changed Lines (vs %s)\n", *baseBranch)
		fmt.Println(strings.Repeat("=", 80))
		fmt.Println()
		fmt.Printf("%-55s %10s %10s %10s\n", "File", "Coverage", "Covered", "Total")
		fmt.Printf("%-55s %10s %10s %10s\n", strings.Repeat("-", 55), "--------", "-------", "-----")

		for _, fc := range files {
			if *verbose || fc.TotalStmts > 0 {
				status := ""
				if *minCoverage > 0 && fc.CoveragePercent < *minCoverage {
					status = " ⚠️"
				}

				fmt.Printf("%-55s %9.1f%% %10d %10d%s\n",
					truncate(fc.File, 55),
					fc.CoveragePercent,
					fc.CoveredStmts,
					fc.TotalStmts,
					status,
				)
			}
		}

		fmt.Printf("%-55s %10s %10s %10s\n", strings.Repeat("-", 55), "--------", "-------", "-----")
	}

	// Calculate overall coverage
	overallPercent := float64(0)
	if totalStmts > 0 {
		overallPercent = float64(totalCovered) / float64(totalStmts) * 100
	}

	if !*summary {
		fmt.Printf("%-55s %9.1f%% %10d %10d\n",
			"TOTAL",
			overallPercent,
			totalCovered,
			totalStmts,
		)
		fmt.Println()
	}

	// Print summary
	fmt.Printf("Changed files: %d\n", len(files))
	fmt.Printf("Changed statements: %d\n", totalStmts)
	fmt.Printf("Covered statements: %d\n", totalCovered)
	fmt.Printf("Changed line coverage: %.1f%%\n", overallPercent)

	// Check threshold
	if *minCoverage > 0 {
		if overallPercent < *minCoverage {
			fmt.Printf("\n❌ Changed line coverage %.1f%% is below threshold %.1f%%\n", overallPercent, *minCoverage)
			os.Exit(1)
		} else {
			fmt.Printf("\n✅ Changed line coverage %.1f%% meets threshold %.1f%%\n", overallPercent, *minCoverage)
		}
	}
}

func getChangedLineRanges(baseBranch string) ([]ChangedLineRange, error) {
	// Run: git diff --unified=0 <baseBranch> HEAD -- '*.go'
	cmd := exec.Command("git", "diff", "--unified=0", baseBranch, "HEAD", "--", "*.go")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w\nOutput: %s", err, string(output))
	}

	var ranges []ChangedLineRange
	var currentFile string

	// Regex patterns
	diffFilePattern := regexp.MustCompile(`^diff --git a/(.*) b/(.*)$`)
	chunkPattern := regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		// Track current file (use the "b/" path - new version)
		if matches := diffFilePattern.FindStringSubmatch(line); matches != nil {
			currentFile = matches[2]
			continue
		}

		// Parse chunk headers
		if matches := chunkPattern.FindStringSubmatch(line); matches != nil {
			if currentFile == "" {
				continue
			}

			startLine, _ := strconv.Atoi(matches[1])
			count := 1
			if len(matches) > 2 && matches[2] != "" {
				count, _ = strconv.Atoi(matches[2])
			}

			// If count is 0, it's a deletion - skip
			if count > 0 {
				ranges = append(ranges, ChangedLineRange{
					File:      currentFile,
					StartLine: startLine,
					EndLine:   startLine + count - 1,
				})
			}
		}
	}

	return ranges, nil
}

func normalizeFilePath(coveragePath string) string {
	// Convert: miren.dev/runtime/pkg/entity/store.go
	//      to: pkg/entity/store.go
	prefix := "miren.dev/runtime/"
	return strings.TrimPrefix(coveragePath, prefix)
}

func overlapsAnyRange(block cover.ProfileBlock, ranges []ChangedLineRange) bool {
	for _, r := range ranges {
		// Check if block overlaps with changed range
		// Block: [block.StartLine, block.EndLine]
		// Range: [r.StartLine, r.EndLine]
		if block.StartLine <= r.EndLine && block.EndLine >= r.StartLine {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
