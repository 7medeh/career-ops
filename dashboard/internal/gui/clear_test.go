package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedClearFixture writes a minimal but realistic data/ dir and returns its root.
func seedClearFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	write("scan-history.tsv", strings.Join([]string{
		"url\tfirst_seen\tportal\ttitle\tcompany\tstatus",
		"https://example.com/1\t2026-08-15\tATS\tSenior Engineer\tAcme\tadded",
		"https://example.com/2\t2026-08-15\tATS\tAccount Executive\tAcme\tskipped_title",
	}, "\n")+"\n")

	write("listing-details.tsv", "url\tsalary\tyoe\tfit\nhttps://example.com/1\t$200K\t3\t80\n")

	write("pipeline.md", strings.Join([]string{
		"# Pipeline Inbox",
		"",
		"## Pendientes",
		"",
		"- [ ] https://example.com/1 | Acme | Senior Engineer",
		"- [x] https://example.com/9 | Acme | Already Processed",
	}, "\n")+"\n")

	write("applications.md", "# Applications Tracker\n\n| # | Company |\n|---|---|\n| 1 | Acme |\n")
	return root
}

func TestClearListingsEmptiesStoresAndBacksThemUp(t *testing.T) {
	root := seedClearFixture(t)

	res, err := ClearListings(root)
	if err != nil {
		t.Fatalf("ClearListings: %v", err)
	}

	if got := len(ParseListings(root)); got != 0 {
		t.Errorf("listings after clear = %d, want 0", got)
	}
	if res.ScanHistoryRemoved != 2 {
		t.Errorf("ScanHistoryRemoved = %d, want 2", res.ScanHistoryRemoved)
	}
	if res.DetailsRemoved != 1 {
		t.Errorf("DetailsRemoved = %d, want 1", res.DetailsRemoved)
	}
	// Only the unchecked entry is a pending listing; "- [x]" is history.
	if res.PipelineRemoved != 1 {
		t.Errorf("PipelineRemoved = %d, want 1", res.PipelineRemoved)
	}

	// Headers survive so the next scan appends to a well-formed TSV.
	for _, name := range []string{"scan-history.tsv", "listing-details.tsv"} {
		body, err := os.ReadFile(filepath.Join(root, "data", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "url\t") {
			t.Errorf("%s = %q, want header row only", name, string(body))
		}
	}

	// Everything cleared is recoverable.
	backupDir := filepath.Join(root, res.BackupDir)
	for _, name := range []string{"scan-history.tsv", "listing-details.tsv", "pipeline.md"} {
		if _, err := os.Stat(filepath.Join(backupDir, name)); err != nil {
			t.Errorf("backup missing for %s: %v", name, err)
		}
	}
}

// The tracker is the scanner's other dedup source: clearing it would re-surface
// every role already evaluated or applied to, which is the opposite of intent.
func TestClearListingsLeavesApplicationsAndProcessedEntriesAlone(t *testing.T) {
	root := seedClearFixture(t)
	appsPath := filepath.Join(root, "data", "applications.md")

	before, err := os.ReadFile(appsPath)
	if err != nil {
		t.Fatalf("read applications.md: %v", err)
	}

	if _, err := ClearListings(root); err != nil {
		t.Fatalf("ClearListings: %v", err)
	}

	after, err := os.ReadFile(appsPath)
	if err != nil {
		t.Fatalf("read applications.md after clear: %v", err)
	}
	if string(before) != string(after) {
		t.Error("applications.md was modified; it must never be touched by a clear")
	}

	pipeline, err := os.ReadFile(filepath.Join(root, "data", "pipeline.md"))
	if err != nil {
		t.Fatalf("read pipeline.md: %v", err)
	}
	got := string(pipeline)
	if !strings.Contains(got, "## Pendientes") {
		t.Error("pipeline.md lost its document structure")
	}
	if !strings.Contains(got, "- [x] https://example.com/9") {
		t.Error("pipeline.md dropped a processed '- [x]' entry")
	}
	if strings.Contains(got, "- [ ]") {
		t.Error("pipeline.md still has pending entries after a clear")
	}
}

// A fresh install has no data files yet; clearing nothing must not error.
func TestClearListingsOnMissingFilesIsANoop(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	res, err := ClearListings(root)
	if err != nil {
		t.Fatalf("ClearListings on empty install: %v", err)
	}
	if res.ScanHistoryRemoved != 0 || res.PipelineRemoved != 0 || res.DetailsRemoved != 0 {
		t.Errorf("expected zero removals, got %+v", res)
	}
}
