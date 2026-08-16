package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/santifer/career-ops/dashboard/internal/gui"
	"github.com/santifer/career-ops/dashboard/internal/theme"
)

// PreferencesClosedMsg is emitted when the preferences screen is dismissed without saving.
type PreferencesClosedMsg struct{}

// PreferencesSaveMsg requests persisting the edited preferences to
// config/profile.yml and portals.yml.
type PreferencesSaveMsg struct {
	CareerOpsPath string
	Prefs         gui.Preferences
}

const (
	prefFieldSalaryTarget = iota
	prefFieldSalaryMinimum
	prefFieldLocationFlex
	prefFieldAddKeyword
	prefFieldCount
)

var prefFieldLabels = [prefFieldCount]string{
	prefFieldSalaryTarget:  "Salary target range",
	prefFieldSalaryMinimum: "Salary minimum",
	prefFieldLocationFlex:  "Location flexibility",
	prefFieldAddKeyword:    "Add position keyword (Enter to add)",
}

// PreferencesModel implements the salary/position/location preferences editor screen.
type PreferencesModel struct {
	values        [prefFieldCount]string
	focus         int
	keywords      []string // existing + newly added, in display order
	newKeywords   []string // only the ones added this session (what gets written)
	saved         bool
	saveErr       string
	width, height int
	theme         theme.Theme
	careerOpsPath string
}

// NewPreferencesModel creates a new preferences screen seeded from the current values.
func NewPreferencesModel(t theme.Theme, prefs gui.Preferences, careerOpsPath string, width, height int) PreferencesModel {
	m := PreferencesModel{
		theme:         t,
		careerOpsPath: careerOpsPath,
		width:         width,
		height:        height,
	}
	m.values[prefFieldSalaryTarget] = prefs.SalaryTarget
	m.values[prefFieldSalaryMinimum] = prefs.SalaryMinimum
	m.values[prefFieldLocationFlex] = prefs.LocationFlexibility
	m.keywords = append(m.keywords, prefs.PositionKeywords...)
	return m
}

// Resize updates dimensions.
func (m *PreferencesModel) Resize(width, height int) {
	m.width = width
	m.height = height
}

// SetSaveResult records the outcome of a save attempt (called by main.go after WritePreferences runs).
func (m *PreferencesModel) SetSaveResult(err error) {
	if err != nil {
		m.saveErr = err.Error()
		m.saved = false
		return
	}
	m.saved = true
	m.saveErr = ""
	m.newKeywords = nil
}

// Update handles input for the preferences screen.
func (m PreferencesModel) Update(msg tea.Msg) (PreferencesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

func (m PreferencesModel) handleKey(msg tea.KeyMsg) (PreferencesModel, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m, func() tea.Msg { return PreferencesClosedMsg{} }

	case tea.KeyCtrlS:
		prefs := gui.Preferences{
			SalaryTarget:        m.values[prefFieldSalaryTarget],
			SalaryMinimum:       m.values[prefFieldSalaryMinimum],
			LocationFlexibility: m.values[prefFieldLocationFlex],
			PositionKeywords:    m.newKeywords,
		}
		path := m.careerOpsPath
		return m, func() tea.Msg {
			return PreferencesSaveMsg{CareerOpsPath: path, Prefs: prefs}
		}

	case tea.KeyTab, tea.KeyDown:
		m.focus = (m.focus + 1) % prefFieldCount
		m.saved = false

	case tea.KeyShiftTab, tea.KeyUp:
		m.focus = (m.focus - 1 + prefFieldCount) % prefFieldCount
		m.saved = false

	case tea.KeyBackspace:
		v := m.values[m.focus]
		if len(v) > 0 {
			runes := []rune(v)
			m.values[m.focus] = string(runes[:len(runes)-1])
		}
		m.saved = false

	case tea.KeyEnter:
		if m.focus == prefFieldAddKeyword {
			kw := strings.TrimSpace(m.values[prefFieldAddKeyword])
			if kw != "" && !containsFold(m.keywords, kw) {
				m.keywords = append(m.keywords, kw)
				m.newKeywords = append(m.newKeywords, kw)
			}
			m.values[prefFieldAddKeyword] = ""
			m.saved = false
		}

	case tea.KeySpace:
		m.values[m.focus] += " "
		m.saved = false

	case tea.KeyRunes:
		m.values[m.focus] += string(msg.Runes)
		m.saved = false
	}
	return m, nil
}

func containsFold(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// -- View --

// View renders the preferences screen.
func (m PreferencesModel) View() string {
	header := m.renderHeader()
	body := m.renderBody()
	help := m.renderHelp()
	return lipgloss.JoinVertical(lipgloss.Left, header, body, help)
}

func (m PreferencesModel) renderHeader() string {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.theme.Text).
		Background(m.theme.Surface).
		Width(m.width).
		Padding(0, 2)

	title := lipgloss.NewStyle().Bold(true).Foreground(m.theme.Blue).Render("PREFERENCES")

	status := ""
	if m.saveErr != "" {
		status = lipgloss.NewStyle().Foreground(m.theme.Red).Render("save failed: " + m.saveErr)
	} else if m.saved {
		status = lipgloss.NewStyle().Foreground(m.theme.Green).Render("saved")
	}
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(status) - 4
	if gap < 1 {
		gap = 1
	}
	return style.Render(title + strings.Repeat(" ", gap) + status)
}

func (m PreferencesModel) renderBody() string {
	padStyle := lipgloss.NewStyle().Padding(1, 2)
	labelStyle := lipgloss.NewStyle().Foreground(m.theme.Subtext)
	focusedLabelStyle := lipgloss.NewStyle().Foreground(m.theme.Blue).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(m.theme.Text)

	var lines []string
	for i := 0; i < prefFieldCount; i++ {
		label := prefFieldLabels[i]
		value := m.values[i]

		var rendered string
		if i == m.focus {
			cursor := lipgloss.NewStyle().Foreground(m.theme.Blue).Render("█")
			fieldBox := lipgloss.NewStyle().
				Foreground(m.theme.Text).
				Background(m.theme.Overlay).
				Padding(0, 1).
				Render(value + cursor)
			rendered = focusedLabelStyle.Render("> "+label) + "\n  " + fieldBox
		} else {
			display := value
			if display == "" {
				display = "—"
			}
			rendered = labelStyle.Render("  "+label) + "\n  " + valueStyle.Render(display)
		}
		lines = append(lines, rendered, "")
	}

	if len(m.keywords) > 0 {
		lines = append(lines, labelStyle.Render("  Position keywords (title_filter.positive):"))
		lines = append(lines, "  "+valueStyle.Render(strings.Join(m.keywords, ", ")))
		lines = append(lines, "")
	}
	lines = append(lines, labelStyle.Render("  Removing keywords isn't supported here — ask Claude to edit portals.yml directly."))

	return padStyle.Render(strings.Join(lines, "\n"))
}

func (m PreferencesModel) renderHelp() string {
	style := lipgloss.NewStyle().
		Foreground(m.theme.Subtext).
		Background(m.theme.Surface).
		Width(m.width).
		Padding(0, 1)

	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(m.theme.Text)
	descStyle := lipgloss.NewStyle().Foreground(m.theme.Subtext)

	brand := lipgloss.NewStyle().Foreground(m.theme.Overlay).Render("career-ops by santifer.io")

	keys := keyStyle.Render("Tab/↓") + descStyle.Render(" next  ") +
		keyStyle.Render("Shift+Tab/↑") + descStyle.Render(" prev  ") +
		keyStyle.Render("Enter") + descStyle.Render(" add keyword  ") +
		keyStyle.Render("Ctrl+S") + descStyle.Render(" save  ") +
		keyStyle.Render("Esc") + descStyle.Render(" back")

	gap := m.width - lipgloss.Width(keys) - lipgloss.Width(brand) - 2
	if gap < 1 {
		gap = 1
	}
	return style.Render(keys + strings.Repeat(" ", gap) + brand)
}
