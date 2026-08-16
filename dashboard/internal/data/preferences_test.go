package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santifer/career-ops/dashboard/internal/model"
)

func TestReadWritePreferencesPatchesTargetedFieldsOnly(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	profileYAML := `candidate:
  full_name: "Test Candidate"

compensation:
  target_range: "$130K-170K"
  currency: "USD"
  minimum: "$125K"
  location_flexibility: "Hybrid preferred"

location:
  country: "United States"
`
	profilePath := filepath.Join(configDir, "profile.yml")
	if err := os.WriteFile(profilePath, []byte(profileYAML), 0o644); err != nil {
		t.Fatalf("failed to write profile.yml: %v", err)
	}

	portalsYAML := `title_filter:
  positive:
    # [CUSTOMIZE] Add keywords matching YOUR target roles
    - "AI"
    - "ML"
  negative:
    - "Sales"
`
	portalsPath := filepath.Join(tempDir, "portals.yml")
	if err := os.WriteFile(portalsPath, []byte(portalsYAML), 0o644); err != nil {
		t.Fatalf("failed to write portals.yml: %v", err)
	}

	prefs, err := ReadPreferences(tempDir)
	if err != nil {
		t.Fatalf("ReadPreferences failed: %v", err)
	}
	if prefs.SalaryTarget != "$130K-170K" || prefs.SalaryMinimum != "$125K" || prefs.LocationFlexibility != "Hybrid preferred" {
		t.Fatalf("unexpected preferences read: %+v", prefs)
	}
	if len(prefs.PositionKeywords) != 2 || prefs.PositionKeywords[0] != "AI" || prefs.PositionKeywords[1] != "ML" {
		t.Fatalf("unexpected position keywords: %v", prefs.PositionKeywords)
	}

	newPrefs := model.Preferences{
		SalaryTarget:        "$150K-190K",
		SalaryMinimum:       "$140K",
		LocationFlexibility: "Remote only",
		PositionKeywords:    []string{"AI", "Platform Engineer"}, // "AI" already exists, should not duplicate
	}
	if err := WritePreferences(tempDir, newPrefs); err != nil {
		t.Fatalf("WritePreferences failed: %v", err)
	}

	updatedProfile, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("failed to read updated profile.yml: %v", err)
	}
	profileStr := string(updatedProfile)

	if !strings.Contains(profileStr, `target_range: "$150K-190K"`) {
		t.Fatalf("expected updated target_range, got:\n%s", profileStr)
	}
	if !strings.Contains(profileStr, `minimum: "$140K"`) {
		t.Fatalf("expected updated minimum, got:\n%s", profileStr)
	}
	if !strings.Contains(profileStr, `location_flexibility: "Remote only"`) {
		t.Fatalf("expected updated location_flexibility, got:\n%s", profileStr)
	}
	if !strings.Contains(profileStr, `full_name: "Test Candidate"`) {
		t.Fatalf("expected unrelated fields untouched, got:\n%s", profileStr)
	}

	updatedPortals, err := os.ReadFile(portalsPath)
	if err != nil {
		t.Fatalf("failed to read updated portals.yml: %v", err)
	}
	portalsStr := string(updatedPortals)

	if !strings.Contains(portalsStr, `"Platform Engineer"`) {
		t.Fatalf("expected new keyword appended, got:\n%s", portalsStr)
	}
	if strings.Count(portalsStr, `"AI"`) != 1 {
		t.Fatalf("expected 'AI' to not be duplicated, got:\n%s", portalsStr)
	}
	if !strings.Contains(portalsStr, "# [CUSTOMIZE] Add keywords matching YOUR target roles") {
		t.Fatalf("expected existing comment preserved, got:\n%s", portalsStr)
	}
	if !strings.Contains(portalsStr, `"Sales"`) {
		t.Fatalf("expected negative list untouched, got:\n%s", portalsStr)
	}
}
