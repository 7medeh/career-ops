package screens

// Messages the pipeline screen emits to open the GUI-layer screens. They live
// here, not in internal/ui/screens, so upstream's pipeline.go carries only the
// six-line key/help hook and nothing else this layer owns.

// OpenListingsMsg is emitted when the listings screen should open.
type OpenListingsMsg struct{}

// OpenPreferencesMsg is emitted when the preferences screen should open.
type OpenPreferencesMsg struct{}
