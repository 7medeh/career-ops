package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClearResult reports what a ClearListings call removed.
type ClearResult struct {
	ScanHistoryRemoved int    `json:"scanHistoryRemoved"`
	PipelineRemoved    int    `json:"pipelineRemoved"`
	DetailsRemoved     int    `json:"detailsRemoved"`
	BackupDir          string `json:"backupDir"`
}

// ClearListings empties the two stores the Listings screen reads from, so the
// next scan repopulates them from scratch under the current portals.yml filter.
//
// Why this exists: scan-history.tsv is the scanner's dedup memory, and
// shouldDedupScanHistoryRow in scan.mjs treats every non-"added" status as
// permanent. A posting rejected under an old title_filter is therefore
// suppressed forever, even after the filter is rewritten to include it —
// retuning the keywords does nothing on its own. Clearing the history is the
// only way to let the new filter reconsider what the old one threw away.
//
// applications.md is deliberately NOT touched. It is a separate dedup source
// for the scanner, so every role already evaluated or applied to stays
// suppressed after a clear; only never-tracked postings come back.
//
// Everything removed is copied to data/.clear-backups/<timestamp>/ first.
func ClearListings(careerOpsPath string) (ClearResult, error) {
	var res ClearResult

	stamp := time.Now().Format("2006-01-02T15-04-05")
	backupDir := filepath.Join(careerOpsPath, "data", ".clear-backups", stamp)
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return res, fmt.Errorf("could not create backup dir: %w", err)
	}
	res.BackupDir = filepath.Join("data", ".clear-backups", stamp)

	history := filepath.Join(careerOpsPath, "data", "scan-history.tsv")
	pipeline := filepath.Join(careerOpsPath, "data", "pipeline.md")
	details := filepath.Join(careerOpsPath, "data", "listing-details.tsv")

	n, err := truncateToHeader(history, backupDir)
	if err != nil {
		return res, err
	}
	res.ScanHistoryRemoved = n

	if n, err = truncateToHeader(details, backupDir); err != nil {
		return res, err
	}
	res.DetailsRemoved = n

	if n, err = clearPipelineEntries(pipeline, backupDir); err != nil {
		return res, err
	}
	res.PipelineRemoved = n

	return res, nil
}

// backup copies path into dir. A missing source is not an error: a fresh
// install has no scan history yet, and clearing nothing is a valid outcome.
func backup(path, dir string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("could not read %s: %w", filepath.Base(path), err)
	}
	dest := filepath.Join(dir, filepath.Base(path))
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return nil, false, fmt.Errorf("could not back up %s: %w", filepath.Base(path), err)
	}
	return data, true, nil
}

// truncateToHeader keeps a TSV's first line and drops every data row, returning
// the number of rows removed.
func truncateToHeader(path, backupDir string) (int, error) {
	data, ok, err := backup(path, backupDir)
	if err != nil || !ok {
		return 0, err
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= 1 {
		return 0, nil
	}
	removed := len(lines) - 1
	if err := os.WriteFile(path, []byte(lines[0]+"\n"), 0o644); err != nil {
		return 0, fmt.Errorf("could not rewrite %s: %w", filepath.Base(path), err)
	}
	return removed, nil
}

// clearPipelineEntries drops the unchecked "- [ ] <url> | …" inbox lines from
// pipeline.md and leaves the rest of the document intact.
//
// Checked lines ("- [x] …") are kept: they record postings already processed,
// which is history rather than a pending listing.
func clearPipelineEntries(path, backupDir string) (int, error) {
	data, ok, err := backup(path, backupDir)
	if err != nil || !ok {
		return 0, err
	}

	var kept []string
	removed := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- [ ]") {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		return 0, fmt.Errorf("could not rewrite pipeline.md: %w", err)
	}
	return removed, nil
}
