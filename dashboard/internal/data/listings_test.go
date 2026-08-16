package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseListingsMergesPipelineAndScanHistoryAndExcludesEvaluated(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	pipeline := `# Pipeline Inbox

## Pendientes

- [ ] https://example.com/jobs/1 | Acme | AI Engineer
- [ ] https://example.com/jobs/2 | Beta Corp | Backend Engineer

## Procesadas
`
	if err := os.WriteFile(filepath.Join(dataDir, "pipeline.md"), []byte(pipeline), 0o644); err != nil {
		t.Fatalf("failed to write pipeline.md: %v", err)
	}

	scanHistory := "url\tfirst_seen\tportal\ttitle\tcompany\tstatus\n" +
		"https://example.com/jobs/3\t2026-08-01\tGreenhouse\tSolutions Engineer\tGamma Inc\tadded\n" +
		"https://example.com/jobs/4\t2026-08-01\tGreenhouse\tJunior Dev\tDelta LLC\tskipped_title\n"
	if err := os.WriteFile(filepath.Join(dataDir, "scan-history.tsv"), []byte(scanHistory), 0o644); err != nil {
		t.Fatalf("failed to write scan-history.tsv: %v", err)
	}

	applications := `# Applications Tracker

| # | Date | Company | Role | Score | Status | PDF | Report | Notes |
|---|------|---------|------|-------|--------|-----|--------|-------|
| 1 | 2026-04-08 | Acme | AI Engineer | 4.0/5 | Evaluated | ✅ | [1](reports/1-acme-2026-04-08.md) | already evaluated |
`
	if err := os.WriteFile(filepath.Join(dataDir, "applications.md"), []byte(applications), 0o644); err != nil {
		t.Fatalf("failed to write applications.md: %v", err)
	}

	listings := ParseListings(tempDir)

	if len(listings) != 2 {
		t.Fatalf("expected 2 listings (Acme excluded as already evaluated, Delta skipped_title excluded), got %d: %+v", len(listings), listings)
	}

	var companies []string
	for _, l := range listings {
		companies = append(companies, l.Company)
	}

	foundBeta, foundGamma := false, false
	for _, c := range companies {
		if c == "Beta Corp" {
			foundBeta = true
		}
		if c == "Gamma Inc" {
			foundGamma = true
		}
	}
	if !foundBeta || !foundGamma {
		t.Fatalf("expected Beta Corp and Gamma Inc in listings, got %v", companies)
	}
}
