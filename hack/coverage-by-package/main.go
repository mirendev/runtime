package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/cover"
)

type PackageCoverage struct {
	Package         string
	CoveredStmts    int64
	TotalStmts      int64
	CoveragePercent float64
}

func main() {
	var (
		coverFile   = flag.String("coverage", "coverage.out", "Path to coverage profile")
		minCoverage = flag.Float64("min", 0, "Minimum coverage threshold (0-100)")
		sortBy      = flag.String("sort", "name", "Sort by: name, coverage, or statements")
	)
	flag.Parse()

	profiles, err := cover.ParseProfiles(*coverFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing coverage profile: %v\n", err)
		os.Exit(1)
	}

	// Map of package -> coverage stats
	packageStats := make(map[string]*PackageCoverage)

	for _, profile := range profiles {
		// Extract package name from file path
		// Example: miren.dev/runtime/pkg/entity/store.go -> miren.dev/runtime/pkg/entity
		pkg := extractPackage(profile.FileName)

		if _, exists := packageStats[pkg]; !exists {
			packageStats[pkg] = &PackageCoverage{
				Package: pkg,
			}
		}

		// Count covered and total statements in this file
		for _, block := range profile.Blocks {
			stmts := int64(block.NumStmt)
			packageStats[pkg].TotalStmts += stmts
			if block.Count > 0 {
				packageStats[pkg].CoveredStmts += stmts
			}
		}
	}

	// Calculate percentages
	var packages []PackageCoverage
	for _, stats := range packageStats {
		if stats.TotalStmts > 0 {
			stats.CoveragePercent = float64(stats.CoveredStmts) / float64(stats.TotalStmts) * 100
		}
		packages = append(packages, *stats)
	}

	// Sort packages
	switch *sortBy {
	case "coverage":
		sort.Slice(packages, func(i, j int) bool {
			return packages[i].CoveragePercent < packages[j].CoveragePercent
		})
	case "statements":
		sort.Slice(packages, func(i, j int) bool {
			return packages[i].TotalStmts > packages[j].TotalStmts
		})
	default: // name
		sort.Slice(packages, func(i, j int) bool {
			return packages[i].Package < packages[j].Package
		})
	}

	// Print results
	fmt.Println("Coverage by Package")
	fmt.Println("===================")
	fmt.Println()
	fmt.Printf("%-60s %10s %10s %10s\n", "Package", "Coverage", "Covered", "Total")
	fmt.Printf("%-60s %10s %10s %10s\n", strings.Repeat("-", 60), "--------", "-------", "-----")

	var totalCovered, totalStmts int64
	belowThreshold := 0

	for _, pkg := range packages {
		status := ""
		if *minCoverage > 0 && pkg.CoveragePercent < *minCoverage {
			status = " ⚠️"
			belowThreshold++
		}

		fmt.Printf("%-60s %9.1f%% %10d %10d%s\n",
			pkg.Package,
			pkg.CoveragePercent,
			pkg.CoveredStmts,
			pkg.TotalStmts,
			status,
		)

		totalCovered += pkg.CoveredStmts
		totalStmts += pkg.TotalStmts
	}

	fmt.Printf("%-60s %10s %10s %10s\n", strings.Repeat("-", 60), "--------", "-------", "-----")

	overallPercent := float64(0)
	if totalStmts > 0 {
		overallPercent = float64(totalCovered) / float64(totalStmts) * 100
	}

	fmt.Printf("%-60s %9.1f%% %10d %10d\n",
		"TOTAL",
		overallPercent,
		totalCovered,
		totalStmts,
	)

	fmt.Println()
	fmt.Printf("Packages: %d\n", len(packages))
	if *minCoverage > 0 {
		fmt.Printf("Below threshold (%.1f%%): %d\n", *minCoverage, belowThreshold)
		if belowThreshold > 0 {
			os.Exit(1)
		}
	}
}

// extractPackage extracts the package path from a file path
// Example: miren.dev/runtime/pkg/entity/store.go -> miren.dev/runtime/pkg/entity
func extractPackage(filePath string) string {
	// Find the last slash to remove the filename
	lastSlash := strings.LastIndex(filePath, "/")
	if lastSlash == -1 {
		return filePath
	}
	return filePath[:lastSlash]
}
