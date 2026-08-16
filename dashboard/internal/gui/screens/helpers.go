package screens

// truncateRunes shortens s to maxRunes runes, appending an ellipsis when there
// is room for one.
//
// Mirrors the unexported helper of the same name in
// internal/ui/screens/pipeline.go. Duplicated rather than imported because that
// one is package-private to upstream's screens package, and exporting it would
// mean editing an upstream-owned file. Keep the two in sync.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}
