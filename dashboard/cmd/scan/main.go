// Command scan runs the native career-ops portal scanner from the terminal,
// independent of the desktop GUI. It hits the ATS APIs (Greenhouse/Ashby/Lever)
// configured in portals.yml, filters + dedupes, writes new roles to
// data/pipeline.md and data/scan-history.tsv, and prints a summary.
//
// Usage:
//
//	go run ./cmd/scan --path /path/to/career-ops
//	go run ./cmd/scan               # defaults to current directory
//
// Suitable for cron / a scheduled job.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santifer/career-ops/dashboard/internal/data"
)

func main() {
	pathFlag := flag.String("path", ".", "Path to the career-ops repo")
	quiet := flag.Bool("quiet", false, "Only print the summary line")
	flag.Parse()

	repo, err := filepath.Abs(*pathFlag)
	if err != nil {
		repo = *pathFlag
	}

	res, err := data.Scan(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Scanned %d companies | found %d | added %d | scored %d existing | skipped: %d title, %d location, %d duplicate\n",
		res.CompaniesScanned, res.Found, res.Added, res.Enriched,
		res.SkippedTitle, res.SkippedLocation, res.SkippedDup)
	if len(res.Errors) > 0 {
		fmt.Printf("%d companies unreachable\n", len(res.Errors))
	}

	if *quiet || res.Added == 0 {
		return
	}

	fmt.Println()
	fmt.Printf("%-4s  %-18s  %-42s  %-16s  %-5s\n", "FIT", "COMPANY", "TITLE", "SALARY", "YOE")
	for _, l := range res.NewListings {
		salary := l.Salary
		if salary == "" {
			salary = "—"
		}
		yoe := "—"
		if l.YOE > 0 {
			yoe = fmt.Sprintf("%d+", l.YOE)
		}
		fmt.Printf("%-4d  %-18.18s  %-42.42s  %-16s  %-5s\n",
			l.Fit, l.Company, l.Title, salary, yoe)
	}
}
