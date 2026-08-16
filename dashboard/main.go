package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/santifer/career-ops/dashboard/internal/data"
	"github.com/santifer/career-ops/dashboard/internal/model"
	"github.com/santifer/career-ops/dashboard/internal/theme"
	"github.com/santifer/career-ops/dashboard/internal/ui/screens"
)

type viewState int

const (
	viewPipeline viewState = iota
	viewReport
	viewProgress
	viewListings
	viewPreferences
)

type appModel struct {
	pipeline        screens.PipelineModel
	viewer          screens.ViewerModel
	progress        screens.ProgressModel
	listings        screens.ListingsModel
	preferences     screens.PreferencesModel
	state           viewState
	careerOpsPath   string
	theme           theme.Theme
	progressMetrics model.ProgressMetrics
}

func (m *appModel) reloadPipelineData() {
	apps := data.ParseApplications(m.careerOpsPath)
	metrics := data.ComputeMetrics(apps)
	m.progressMetrics = data.ComputeProgressMetrics(apps)
	m.pipeline = m.pipeline.WithReloadedData(apps, metrics)
}

func (m *appModel) reloadListings() {
	listings := data.ParseListings(m.careerOpsPath)
	m.listings = screens.NewListingsModel(m.theme, listings, m.careerOpsPath, m.pipeline.Width(), m.pipeline.Height())
}

// openURLCmd returns a tea.Cmd that opens url in the system's default browser.
func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "linux":
			cmd = exec.Command("xdg-open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", "", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Run()
		return nil
	}
}

// applyHandoffDoneMsg is emitted after an interactive claude apply session
// exits, so both the listings view and the underlying application tracker
// (applications.md may have gained a new entry) get refreshed.
type applyHandoffDoneMsg struct{}

// enrichDoneMsg signals that the Levels.fyi company-profile refresh finished.
type enrichDoneMsg struct{ summary string }

// outreachHandoff drafts an outreach email for a listing by handing off to an
// interactive claude session. The draft is always left for review -- the
// dashboard never sends mail itself.
func (m appModel) outreachHandoff(msg screens.ListingsEmailMsg) tea.Cmd {
	// Contacts come from data/contacts.tsv, which the candidate maintains.
	// When a company has none, the draft is still produced with the recipient
	// left blank so it can be addressed once a contact is found.
	recipient := "No contact on file for this company in data/contacts.tsv. " +
		"Draft the email with the To: line left blank, and note which role at the " +
		"company would be the right person to reach (for example the hiring manager " +
		"named in the posting, or a VP of Engineering). Do not search for, guess, or " +
		"construct anyone's email address."
	if len(msg.Contacts) > 0 {
		var b strings.Builder
		b.WriteString("Contacts on file for this company (from data/contacts.tsv):\n")
		for _, c := range msg.Contacts {
			fmt.Fprintf(&b, "  - %s, %s <%s>\n", c.Name, c.Title, c.Email)
		}
		b.WriteString("Address the draft to the most senior relevant person on that list.")
		recipient = b.String()
	}

	prompt := fmt.Sprintf(
		"Draft a cold outreach email about the %s role at %s (%s).\n\n"+
			"%s\n\n"+
			"Read the posting, cv.md, modes/_profile.md and modes/contacto.md first. "+
			"Follow the stealth rules in modes/_profile.md: use the personal email "+
			"address, and do not name the current employer's clients or project names. "+
			"Keep it under 150 words, specific to this company, and free of em-dashes. "+
			"Write the draft to outreach/%s-<today>.md and show it to me. "+
			"Do NOT send anything -- I will review, address, and send it myself.",
		msg.Title, msg.Company, msg.URL, recipient, slugifyCompany(msg.Company),
	)

	cmd := exec.Command("claude", prompt)
	cmd.Dir = m.careerOpsPath
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return applyHandoffDoneMsg{}
	})
}

// enrichCompaniesCmd refreshes cached Levels.fyi profiles for every company in
// the current listings. Network work runs off the UI thread.
func (m appModel) enrichCompaniesCmd() tea.Cmd {
	companies := data.ListingCompanies(m.listings.Listings())
	careerOpsPath := m.careerOpsPath
	return func() tea.Msg {
		res, err := data.RefreshCompanyProfiles(careerOpsPath, companies)
		return enrichDoneMsg{summary: enrichSummary(res, err)}
	}
}

// enrichSummary turns a refresh result into a one-line header message. Being
// rate-limited is called out explicitly so a partial refresh is not mistaken
// for missing data.
func enrichSummary(res data.RefreshResult, err error) string {
	if err != nil {
		return fmt.Sprintf("enrich failed: %v", err)
	}
	switch {
	case res.Throttled:
		return fmt.Sprintf("enriched %d, rate-limited by levels.fyi with %d left - press E again later",
			res.Resolved, res.Remaining)
	case res.Resolved == 0 && res.Missed == 0:
		return "company profiles already up to date"
	case res.Missed > 0:
		return fmt.Sprintf("enriched %d companies, %d not on levels.fyi", res.Resolved, res.Missed)
	default:
		return fmt.Sprintf("enriched %d companies from levels.fyi", res.Resolved)
	}
}

var reCompanySlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugifyCompany(s string) string {
	return strings.Trim(reCompanySlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func (m appModel) Init() tea.Cmd {
	return nil
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.pipeline.Resize(msg.Width, msg.Height)
		if m.state == viewReport {
			m.viewer.Resize(msg.Width, msg.Height)
		}
		if m.state == viewProgress {
			m.progress.Resize(msg.Width, msg.Height)
		}
		if m.state == viewListings {
			m.listings.Resize(msg.Width, msg.Height)
		}
		if m.state == viewPreferences {
			m.preferences.Resize(msg.Width, msg.Height)
		}
		pm, cmd := m.pipeline.Update(msg)
		m.pipeline = pm
		return m, cmd

	case screens.PipelineClosedMsg:
		return m, tea.Quit

	case screens.PipelineLoadReportMsg:
		archetype, tldr, remote, comp := data.LoadReportSummary(msg.CareerOpsPath, msg.ReportPath)
		m.pipeline.EnrichReport(msg.ReportPath, archetype, tldr, remote, comp)
		return m, nil

	case screens.PipelineUpdateStatusMsg:
		err := data.UpdateApplicationStatus(msg.CareerOpsPath, msg.App, msg.NewStatus)
		if err != nil {
			// Log the error but still reload data to keep UI consistent
			fmt.Fprintf(os.Stderr, "WARN: status update failed: %v\n", err)
		}
		m.reloadPipelineData()
		return m, nil

	case screens.PipelineRefreshMsg:
		m.reloadPipelineData()
		return m, nil

	case screens.PipelineOpenReportMsg:
		m.viewer = screens.NewViewerModel(
			m.theme,
			msg.Path, msg.Title,
			m.pipeline.Width(), m.pipeline.Height(),
		)
		m.state = viewReport
		return m, nil

	case screens.ViewerClosedMsg:
		m.state = viewPipeline
		return m, nil

	case screens.PipelineOpenProgressMsg:
		m.progress = screens.NewProgressModel(
			theme.NewTheme("catppuccin-mocha"),
			m.progressMetrics,
			m.pipeline.Width(), m.pipeline.Height(),
		)
		m.state = viewProgress
		return m, nil

	case screens.ProgressClosedMsg:
		m.state = viewPipeline
		return m, nil

	case screens.PipelineOpenURLMsg:
		return m, openURLCmd(msg.URL)

	case screens.PipelineOpenListingsMsg:
		m.reloadListings()
		m.state = viewListings
		return m, nil

	case screens.ListingsClosedMsg:
		m.state = viewPipeline
		return m, nil

	case screens.ListingsRefreshMsg:
		m.reloadListings()
		return m, nil

	case screens.ListingsOpenURLMsg:
		return m, openURLCmd(msg.URL)

	case screens.ListingsScanMsg:
		cmd := exec.Command("claude", "/career-ops scan")
		cmd.Dir = m.careerOpsPath
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return screens.ListingsRefreshMsg{}
		})

	case screens.ListingsApplyMsg:
		prompt := fmt.Sprintf(
			"Evaluate %s if not already evaluated (mode: oferta), then run the apply pipeline: "+
				"curate a tailored resume, then use Chrome browser automation to open the posting "+
				"and fill in the fields you're confident about. For anything ambiguous, or requiring "+
				"a judgment call, stop and ask me. Never click Submit -- I will review and submit myself.",
			msg.URL,
		)
		cmd := exec.Command("claude", prompt)
		cmd.Dir = m.careerOpsPath
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return applyHandoffDoneMsg{}
		})

	case screens.ListingsEmailMsg:
		return m, m.outreachHandoff(msg)

	case screens.ListingsEnrichMsg:
		return m, m.enrichCompaniesCmd()

	case enrichDoneMsg:
		m.reloadListings()
		m.listings.SetStatus(msg.summary)
		return m, nil

	case applyHandoffDoneMsg:
		m.reloadListings()
		m.reloadPipelineData()
		return m, nil

	case screens.PipelineOpenPreferencesMsg:
		prefs, _ := data.ReadPreferences(m.careerOpsPath)
		m.preferences = screens.NewPreferencesModel(m.theme, prefs, m.careerOpsPath, m.pipeline.Width(), m.pipeline.Height())
		m.state = viewPreferences
		return m, nil

	case screens.PreferencesClosedMsg:
		m.state = viewPipeline
		return m, nil

	case screens.PreferencesSaveMsg:
		err := data.WritePreferences(msg.CareerOpsPath, msg.Prefs)
		m.preferences.SetSaveResult(err)
		return m, nil

	default:
		if m.state == viewReport {
			vm, cmd := m.viewer.Update(msg)
			m.viewer = vm
			return m, cmd
		}
		if m.state == viewProgress {
			pg, cmd := m.progress.Update(msg)
			m.progress = pg
			return m, cmd
		}
		if m.state == viewListings {
			lm, cmd := m.listings.Update(msg)
			m.listings = lm
			return m, cmd
		}
		if m.state == viewPreferences {
			prm, cmd := m.preferences.Update(msg)
			m.preferences = prm
			return m, cmd
		}
		pm, cmd := m.pipeline.Update(msg)
		m.pipeline = pm
		return m, cmd
	}
}

func (m appModel) View() string {
	switch m.state {
	case viewReport:
		return m.viewer.View()
	case viewProgress:
		return m.progress.View()
	case viewListings:
		return m.listings.View()
	case viewPreferences:
		return m.preferences.View()
	default:
		return m.pipeline.View()
	}
}

func main() {
	pathFlag := flag.String("path", ".", "Path to career-ops directory")
	flag.Parse()

	careerOpsPath := *pathFlag

	// Load applications
	apps := data.ParseApplications(careerOpsPath)
	if apps == nil {
		fmt.Fprintf(os.Stderr, "Error: could not find applications.md in %s or %s/data/\n", careerOpsPath, careerOpsPath)
		os.Exit(1)
	}

	// Seed data/contacts.tsv so the outreach flow has somewhere to read from.
	// Existing files are left alone.
	if _, err := data.EnsureContactsFile(careerOpsPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create contacts.tsv: %v\n", err)
	}

	// Compute metrics
	metrics := data.ComputeMetrics(apps)
	progressMetrics := data.ComputeProgressMetrics(apps)

	// Batch-load all report summaries
	t := theme.NewTheme("auto")
	pm := screens.NewPipelineModel(t, apps, metrics, careerOpsPath, 120, 40)

	for _, app := range apps {
		if app.ReportPath == "" {
			continue
		}
		archetype, tldr, remote, comp := data.LoadReportSummary(careerOpsPath, app.ReportPath)
		if archetype != "" || tldr != "" || remote != "" || comp != "" {
			pm.EnrichReport(app.ReportPath, archetype, tldr, remote, comp)
		}
	}

	m := appModel{
		pipeline:        pm,
		careerOpsPath:   careerOpsPath,
		theme:           t,
		progressMetrics: progressMetrics,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
