package data

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/santifer/career-ops/dashboard/internal/model"
)

var (
	reTargetRange        = regexp.MustCompile(`(?m)^(\s*target_range:\s*)"([^"]*)"`)
	reMinimum            = regexp.MustCompile(`(?m)^(\s*minimum:\s*)"([^"]*)"`)
	reLocationFlex       = regexp.MustCompile(`(?m)^(\s*location_flexibility:\s*)"([^"]*)"`)
	rePositiveBlock      = regexp.MustCompile(`(?s)(positive:\s*\n)(.*?)(\n\s*negative:)`)
	rePositiveItem       = regexp.MustCompile(`(?m)^\s*-\s*"([^"]*)"\s*$`)
	rePositiveItemIndent = regexp.MustCompile(`(?m)^(\s*)-\s*"[^"]*"\s*$`)
)

// ReadPreferences extracts the current salary/location/position preferences
// from config/profile.yml and portals.yml. It does not parse the full YAML
// structurally -- these files are heavily comment-annotated, so a targeted
// regex read (mirrored by an equally targeted write in WritePreferences)
// avoids needing a round-trip YAML library that would strip comments on save.
func ReadPreferences(careerOpsPath string) (model.Preferences, error) {
	var prefs model.Preferences

	profilePath := filepath.Join(careerOpsPath, "config", "profile.yml")
	profileContent, err := os.ReadFile(profilePath)
	if err != nil {
		return prefs, fmt.Errorf("reading config/profile.yml: %w", err)
	}
	profileStr := string(profileContent)

	if m := reTargetRange.FindStringSubmatch(profileStr); m != nil {
		prefs.SalaryTarget = m[2]
	}
	if m := reMinimum.FindStringSubmatch(profileStr); m != nil {
		prefs.SalaryMinimum = m[2]
	}
	if m := reLocationFlex.FindStringSubmatch(profileStr); m != nil {
		prefs.LocationFlexibility = m[2]
	}

	portalsPath := filepath.Join(careerOpsPath, "portals.yml")
	portalsContent, err := os.ReadFile(portalsPath)
	if err == nil {
		if m := rePositiveBlock.FindStringSubmatch(string(portalsContent)); m != nil {
			for _, item := range rePositiveItem.FindAllStringSubmatch(m[2], -1) {
				prefs.PositionKeywords = append(prefs.PositionKeywords, item[1])
			}
		}
	}

	return prefs, nil
}

// WritePreferences patches config/profile.yml's compensation fields and
// appends any new (not-already-present) position keywords to portals.yml's
// title_filter.positive list. It only ever adds keywords -- it never removes
// existing ones, since a faulty regex-based delete risks corrupting the
// heavily comment-annotated positive list. Removing keywords stays a
// conversational edit (ask Claude to update portals.yml), same as any other
// portals.yml customization.
func WritePreferences(careerOpsPath string, prefs model.Preferences) error {
	profilePath := filepath.Join(careerOpsPath, "config", "profile.yml")
	profileContent, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("reading config/profile.yml: %w", err)
	}
	profileStr := string(profileContent)

	if prefs.SalaryTarget != "" {
		profileStr = patchQuotedField(profileStr, reTargetRange, prefs.SalaryTarget)
	}
	if prefs.SalaryMinimum != "" {
		profileStr = patchQuotedField(profileStr, reMinimum, prefs.SalaryMinimum)
	}
	if prefs.LocationFlexibility != "" {
		profileStr = patchQuotedField(profileStr, reLocationFlex, prefs.LocationFlexibility)
	}

	if err := os.WriteFile(profilePath, []byte(profileStr), 0o644); err != nil {
		return fmt.Errorf("writing config/profile.yml: %w", err)
	}

	if len(prefs.PositionKeywords) == 0 {
		return nil
	}

	portalsPath := filepath.Join(careerOpsPath, "portals.yml")
	portalsContent, err := os.ReadFile(portalsPath)
	if err != nil {
		return fmt.Errorf("reading portals.yml: %w", err)
	}
	portalsStr := string(portalsContent)

	blockMatch := rePositiveBlock.FindStringSubmatch(portalsStr)
	if blockMatch == nil {
		return fmt.Errorf("could not locate title_filter.positive block in portals.yml")
	}

	existing := make(map[string]bool)
	for _, item := range rePositiveItem.FindAllStringSubmatch(blockMatch[2], -1) {
		existing[strings.ToLower(item[1])] = true
	}

	indent := "    "
	if im := rePositiveItemIndent.FindStringSubmatch(blockMatch[2]); im != nil {
		indent = im[1]
	}

	var toAdd []string
	for _, kw := range prefs.PositionKeywords {
		if kw == "" || existing[strings.ToLower(kw)] {
			continue
		}
		toAdd = append(toAdd, kw)
	}
	if len(toAdd) == 0 {
		return nil
	}

	var newLines strings.Builder
	for _, kw := range toAdd {
		newLines.WriteString(indent + `- "` + escapeYAMLString(kw) + "\"\n")
	}

	patchedBlock := blockMatch[1] + blockMatch[2] + newLines.String()
	fullMatch := blockMatch[0]
	replacement := patchedBlock + blockMatch[3]
	portalsStr = strings.Replace(portalsStr, fullMatch, replacement, 1)

	if err := os.WriteFile(portalsPath, []byte(portalsStr), 0o644); err != nil {
		return fmt.Errorf("writing portals.yml: %w", err)
	}
	return nil
}

// escapeYAMLString escapes double quotes so user-entered values stay valid
// inside a double-quoted YAML scalar.
func escapeYAMLString(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// patchQuotedField replaces the quoted value captured by re's 2nd submatch
// group with newValue, via direct string splicing rather than Go's regexp
// replacement templates -- a template-based ReplaceAllString would misparse
// a literal "$" in newValue (e.g. "$150K") as a group reference.
func patchQuotedField(content string, re *regexp.Regexp, newValue string) string {
	loc := re.FindStringSubmatchIndex(content)
	if loc == nil {
		return content
	}
	// loc[4], loc[5] are the start/end byte offsets of submatch group 2 (the value).
	valueStart, valueEnd := loc[4], loc[5]
	return content[:valueStart] + escapeYAMLString(newValue) + content[valueEnd:]
}
