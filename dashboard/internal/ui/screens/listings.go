package screens

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/santifer/career-ops/dashboard/internal/data"
	"github.com/santifer/career-ops/dashboard/internal/model"
	"github.com/santifer/career-ops/dashboard/internal/theme"
)

// ListingsClosedMsg is emitted when the listings screen is dismissed.
type ListingsClosedMsg struct{}

// ListingsRefreshMsg requests a reload of pipeline.md/scan-history.tsv from disk.
type ListingsRefreshMsg struct{}

// ListingsScanMsg requests a fresh portal scan (hands off to an interactive claude session).
type ListingsScanMsg struct{}

// ListingsOpenURLMsg is emitted when a listing's URL should be opened in the browser.
type ListingsOpenURLMsg struct {
	URL string
}

// ListingsApplyMsg requests the guided-apply handoff for a listing
// (hands off to an interactive claude session running the oferta/apply pipeline).
type ListingsApplyMsg struct {
	URL     string
	Company string
	Title   string
}

// ListingsEmailMsg requests the outreach-draft handoff for a listing. The draft
// is written for review; nothing is ever sent from the dashboard.
type ListingsEmailMsg struct {
	URL      string
	Company  string
	Title    string
	Contacts []model.Contact
}

// ListingsEnrichMsg requests a company-profile refresh from Levels.fyi.
type ListingsEnrichMsg struct{}

// ListingsModel implements the job listings dashboard screen.
type ListingsModel struct {
	listings      []model.JobListing
	contacts      map[string][]model.Contact
	status        string
	cursor        int
	scrollOffset  int
	width, height int
	theme         theme.Theme
	careerOpsPath string
}

// NewListingsModel creates a new listings screen.
func NewListingsModel(t theme.Theme, listings []model.JobListing, careerOpsPath string, width, height int) ListingsModel {
	sorted := make([]model.JobListing, len(listings))
	copy(sorted, listings)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].FirstSeen != sorted[j].FirstSeen {
			return sorted[i].FirstSeen > sorted[j].FirstSeen
		}
		return strings.ToLower(sorted[i].Company) < strings.ToLower(sorted[j].Company)
	})

	return ListingsModel{
		listings:      sorted,
		contacts:      data.LoadContacts(careerOpsPath),
		width:         width,
		height:        height,
		theme:         t,
		careerOpsPath: careerOpsPath,
	}
}

// SetStatus sets a transient message shown in the header, used to report the
// outcome of a company-profile refresh.
func (m *ListingsModel) SetStatus(s string) { m.status = s }

// Listings returns the listings currently loaded into the screen.
func (m ListingsModel) Listings() []model.JobListing {
	return m.listings
}

// Resize updates dimensions.
func (m *ListingsModel) Resize(width, height int) {
	m.width = width
	m.height = height
}

// CurrentListing returns the currently selected listing, if any.
func (m ListingsModel) CurrentListing() (model.JobListing, bool) {
	if m.cursor < 0 || m.cursor >= len(m.listings) {
		return model.JobListing{}, false
	}
	return m.listings[m.cursor], true
}

// Update handles input for the listings screen.
func (m ListingsModel) Update(msg tea.Msg) (ListingsModel, tea.Cmd) {
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

func (m ListingsModel) handleKey(msg tea.KeyMsg) (ListingsModel, tea.Cmd) {
	// Any keypress dismisses a lingering refresh status.
	m.status = ""

	switch msg.String() {
	case "q", "esc":
		return m, func() tea.Msg { return ListingsClosedMsg{} }

	case "down", "j":
		if len(m.listings) > 0 {
			m.cursor++
			if m.cursor >= len(m.listings) {
				m.cursor = len(m.listings) - 1
			}
			m.adjustScroll()
		}

	case "up", "k":
		if len(m.listings) > 0 {
			m.cursor--
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.adjustScroll()
		}

	case "r":
		return m, func() tea.Msg { return ListingsRefreshMsg{} }

	case "s":
		return m, func() tea.Msg { return ListingsScanMsg{} }

	case "o":
		if l, ok := m.CurrentListing(); ok && l.URL != "" {
			return m, func() tea.Msg { return ListingsOpenURLMsg{URL: l.URL} }
		}

	case "a", "enter":
		if l, ok := m.CurrentListing(); ok && l.URL != "" {
			return m, func() tea.Msg {
				return ListingsApplyMsg{URL: l.URL, Company: l.Company, Title: l.Title}
			}
		}

	case "e":
		if l, ok := m.CurrentListing(); ok && l.Company != "" {
			contacts := data.ContactsFor(m.contacts, l.Company)
			return m, func() tea.Msg {
				return ListingsEmailMsg{
					URL:      l.URL,
					Company:  l.Company,
					Title:    l.Title,
					Contacts: contacts,
				}
			}
		}

	case "E":
		return m, func() tea.Msg { return ListingsEnrichMsg{} }
	}
	return m, nil
}

func (m *ListingsModel) adjustScroll() {
	availHeight := m.height - 6
	if availHeight < 5 {
		availHeight = 5
	}
	margin := 3

	if m.cursor >= m.scrollOffset+availHeight-margin {
		m.scrollOffset = m.cursor - availHeight + margin + 1
	}
	if m.cursor < m.scrollOffset+margin {
		m.scrollOffset = m.cursor - margin
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// -- View --

// View renders the listings screen.
func (m ListingsModel) View() string {
	header := m.renderHeader()
	body := m.renderBody()
	help := m.renderHelp()

	bodyLines := strings.Split(body, "\n")
	if m.scrollOffset > 0 && m.scrollOffset < len(bodyLines) {
		bodyLines = bodyLines[m.scrollOffset:]
	}
	availHeight := m.height - 4
	if availHeight < 3 {
		availHeight = 3
	}
	if len(bodyLines) > availHeight {
		bodyLines = bodyLines[:availHeight]
	}
	body = strings.Join(bodyLines, "\n")

	return lipgloss.JoinVertical(lipgloss.Left, header, body, help)
}

func (m ListingsModel) renderHeader() string {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.theme.Text).
		Background(m.theme.Surface).
		Width(m.width).
		Padding(0, 2)

	right := lipgloss.NewStyle().Foreground(m.theme.Subtext)
	infoText := fmt.Sprintf("%d open, not yet evaluated", len(m.listings))
	if m.status != "" {
		right = right.Foreground(m.theme.Peach)
		infoText = m.status
	}
	info := right.Render(infoText)

	title := lipgloss.NewStyle().Bold(true).Foreground(m.theme.Blue).Render("LISTINGS")
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(info) - 4
	if gap < 1 {
		gap = 1
	}

	return style.Render(title + strings.Repeat(" ", gap) + info)
}

func (m ListingsModel) renderBody() string {
	if len(m.listings) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(m.theme.Subtext).Padding(1, 2)
		return emptyStyle.Render("No open listings yet. Press 's' to run a scan.")
	}

	var lines []string
	for i, l := range m.listings {
		selected := i == m.cursor
		lines = append(lines, m.renderListingLine(l, selected))
	}
	return strings.Join(lines, "\n")
}

// Column widths for the listings table. titleW is whatever is left over.
const (
	colCompany   = 18
	colStage     = 10
	colHeadcount = 7
	colSalary    = 12
	colSeen      = 10
	colPortal    = 14
)

func (m ListingsModel) renderListingLine(l model.JobListing, selected bool) string {
	padStyle := lipgloss.NewStyle().Padding(0, 2)

	titleW := m.width - colCompany - colStage - colHeadcount - colSalary - colSeen - colPortal - 15
	if titleW < 15 {
		titleW = 15
	}

	companyStyle := lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Width(colCompany)
	titleStyle := lipgloss.NewStyle().Foreground(m.theme.Subtext).Width(titleW)

	// Startups are the signal being surfaced, so they get the accent colour;
	// everything else stays muted.
	stageStyle := lipgloss.NewStyle().Width(colStage)
	if l.Stage.IsStartup() {
		stageStyle = stageStyle.Foreground(m.theme.Green)
	} else {
		stageStyle = stageStyle.Foreground(m.theme.Subtext)
	}

	headcountStyle := lipgloss.NewStyle().Foreground(m.theme.Subtext).Width(colHeadcount)

	// An estimated salary is a company-wide median from Levels.fyi, not the
	// posting's own range -- rendered dimmer so the two are not confused.
	salaryStyle := lipgloss.NewStyle().Width(colSalary)
	if l.SalaryEstimated {
		salaryStyle = salaryStyle.Foreground(m.theme.Overlay)
	} else {
		salaryStyle = salaryStyle.Foreground(m.theme.Yellow)
	}

	seenStyle := lipgloss.NewStyle().Foreground(m.theme.Subtext).Width(colSeen)
	portalStyle := lipgloss.NewStyle().Foreground(m.theme.Sky).Width(colPortal)

	line := fmt.Sprintf(" %s %s %s %s %s %s %s",
		companyStyle.Render(truncateRunes(l.Company, colCompany)),
		titleStyle.Render(truncateRunes(l.Title, titleW)),
		stageStyle.Render(truncateRunes(stageLabel(l), colStage-1)),
		headcountStyle.Render(truncateRunes(headcountLabel(l), colHeadcount-1)),
		salaryStyle.Render(truncateRunes(orDash(l.Salary), colSalary-1)),
		seenStyle.Render(truncateRunes(orDash(l.FirstSeen), colSeen)),
		portalStyle.Render(truncateRunes(orDash(l.Portal), colPortal-1)),
	)

	if selected {
		selStyle := lipgloss.NewStyle().Background(m.theme.Overlay).Width(m.width - 4)
		return padStyle.Render(selStyle.Render(line))
	}
	return padStyle.Render(line)
}

// stageLabel renders the funding stage, prefixed with "~" when the stage was
// inferred from headcount and age rather than reported by Levels.fyi.
func stageLabel(l model.JobListing) string {
	s := l.Stage.String()
	if s == "" {
		return "—"
	}
	if l.StageInferred {
		return "~" + s
	}
	return s
}

// headcountLabel prefers the exact employee count and falls back to the range.
func headcountLabel(l model.JobListing) string {
	if l.Headcount > 0 {
		return formatHeadcount(l.Headcount)
	}
	if l.HeadcountRange != "" {
		return l.HeadcountRange
	}
	return "—"
}

// formatHeadcount abbreviates counts so they fit the column (1200 -> "1.2k").
func formatHeadcount(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 10000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return strconv.Itoa(n/1000) + "k"
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (m ListingsModel) renderHelp() string {
	style := lipgloss.NewStyle().
		Foreground(m.theme.Subtext).
		Background(m.theme.Surface).
		Width(m.width).
		Padding(0, 1)

	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(m.theme.Text)
	descStyle := lipgloss.NewStyle().Foreground(m.theme.Subtext)

	// Levels.fyi asks that displayed figures link back and cite them; the stage,
	// headcount and estimated-salary columns are all sourced from there.
	brand := lipgloss.NewStyle().Foreground(m.theme.Overlay).
		Render("company data: levels.fyi  ·  career-ops by santifer.io")

	keys := keyStyle.Render("↑↓/jk") + descStyle.Render(" nav  ") +
		keyStyle.Render("o") + descStyle.Render(" open URL  ") +
		keyStyle.Render("a/Enter") + descStyle.Render(" apply  ") +
		keyStyle.Render("e") + descStyle.Render(" email  ") +
		keyStyle.Render("s") + descStyle.Render(" scan  ") +
		keyStyle.Render("E") + descStyle.Render(" enrich  ") +
		keyStyle.Render("r") + descStyle.Render(" refresh  ") +
		keyStyle.Render("Esc") + descStyle.Render(" back")

	gap := m.width - lipgloss.Width(keys) - lipgloss.Width(brand) - 2
	if gap < 1 {
		gap = 1
	}

	return style.Render(keys + strings.Repeat(" ", gap) + brand)
}
