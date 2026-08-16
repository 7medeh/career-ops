package gui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanLive hits real ATS APIs. Run explicitly:
//
//	CAREEROPS_LIVE=1 go test ./internal/data/ -run TestScanLive -v
func TestScanLive(t *testing.T) {
	if os.Getenv("CAREEROPS_LIVE") == "" {
		t.Skip("set CAREEROPS_LIVE=1 to run the live network scan test")
	}
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tmp, "data", "applications.md"), []byte("# Applications Tracker\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "data", "pipeline.md"), []byte("# Pipeline\n\n## Pendientes\n\n## Procesadas\n"), 0o644)

	portals := `title_filter:
  positive:
    - "Engineer"
    - "AI"
  negative:
    - "Sales"
location_filter:
  remote_only: false
  allowed_locations: []
tracked_companies:
  - name: Anthropic
    api: https://boards-api.greenhouse.io/v1/boards/anthropic/jobs
    enabled: true
  - name: ElevenLabs
    api_provider: ashby
    api_company: elevenlabs
    api: https://jobs.ashbyhq.com/api/non-user-graphql?op=ApiJobBoardWithTeams
    enabled: true
  - name: Tinybird
    api: https://api.lever.co/v0/postings/tinybird?mode=json
    enabled: true
`
	if err := os.WriteFile(filepath.Join(tmp, "portals.yml"), []byte(portals), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Scan(tmp)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	t.Logf("companies=%d found=%d added=%d skippedTitle=%d skippedDup=%d errors=%v",
		res.CompaniesScanned, res.Found, res.Added, res.SkippedTitle, res.SkippedDup, res.Errors)
	if res.CompaniesScanned != 3 {
		t.Fatalf("expected 3 companies scanned, got %d", res.CompaniesScanned)
	}
	if res.Found == 0 {
		t.Fatal("expected to find some postings across 3 real companies")
	}
	if res.Added == 0 {
		t.Fatal("expected to add some listings on a fresh temp repo")
	}
	// Confirm persistence + salary column round-trips through ParseListings.
	reloaded := ParseListings(tmp)
	if len(reloaded) == 0 {
		t.Fatal("expected ParseListings to return the freshly-written listings")
	}
	withSalary := 0
	for _, l := range reloaded {
		if l.Salary != "" {
			withSalary++
		}
	}
	t.Logf("reloaded=%d withSalary=%d sample=%+v", len(reloaded), withSalary, reloaded[0])
}
