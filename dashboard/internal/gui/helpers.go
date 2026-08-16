package gui

import "strings"

// normalizeCompany folds a company name to a comparable key.
//
// This mirrors the unexported helper of the same name in
// internal/data/career.go. It is duplicated rather than imported because that
// one is package-private to upstream's data package, and exporting it would
// mean editing an upstream-owned file — the one thing this package exists to
// avoid. Keep the two in sync if upstream's normalization changes.
func normalizeCompany(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	for _, suffix := range []string{" inc.", " inc", " llc", " ltd", " corp", " corporation", " technologies", " technology", " group", " co."} {
		s = strings.TrimSuffix(s, suffix)
	}
	return strings.TrimSpace(s)
}
