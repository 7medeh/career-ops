package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/santifer/career-ops/dashboard/internal/data"
	"github.com/santifer/career-ops/dashboard/internal/gui"
	guiscreens "github.com/santifer/career-ops/dashboard/internal/gui/screens"
	"github.com/santifer/career-ops/dashboard/internal/i18n"
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
	listings        guiscreens.ListingsModel
	preferences     guiscreens.PreferencesModel
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

// reloadListings rebuilds the listings screen from data/pipeline.md and
// data/scan-history.tsv, overlaid with cached Levels.fyi company profiles.
func (m *appModel) reloadListings() {
	listings := gui.ParseListings(m.careerOpsPath)
	m.listings = guiscreens.NewListingsModel(m.theme, listings, m.careerOpsPath, m.pipeline.Width(), m.pipeline.Height())
}

func (m appModel) Init() tea.Cmd {
	return nil
}

// Update manages global app state and routes incoming messages to active screens.
func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "t", "T":
			// Toggle language globally, unless the user is actively typing in a text input field
			if !(m.state == viewPipeline && m.pipeline.IsTextInputActive()) {
				i18n.ToggleLang()
			}
		}
	}

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

	case screens.PipelineUpdateStatusAndNotesMsg:
		// Issue 1380: atomic status + notes write from the discard reason picker.
		err := data.UpdateApplicationStatusAndNotes(msg.CareerOpsPath, msg.App, msg.NewStatus, msg.NotesAppend)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: status+notes update failed: %v\n", err)
		}
		m.reloadPipelineData()
		return m, nil

	case screens.PipelineRefreshMsg:
		m.reloadPipelineData()
		return m, nil

	case screens.PipelineOpenReportMsg:
		m.viewer = screens.NewViewerModel(
			m.theme,
			m.careerOpsPath,
			msg.Path, msg.Title,
			m.pipeline.Width(), m.pipeline.Height(),
			msg.App,
		)
		m.state = viewReport
		return m, nil

	case screens.ViewerClosedMsg:
		m.state = viewPipeline
		return m, nil

	case screens.ViewerOpenCoverLetterMsg:
		path := msg.Path
		return m, func() tea.Msg {
			if err := openWithDefaultApp(path); err != nil {
				fmt.Fprintf(os.Stderr, "WARN: could not open cover letter: %v\n", err)
			}
			return nil
		}

	case screens.ViewerUpdateStatusMsg:
		normalized := data.NormalizeStatus(msg.NewStatus)
		if normalized == "hired" {
			err := data.UpdateApplicationStatus(m.careerOpsPath, msg.App, msg.NewStatus)
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARN: status update failed: %v\n", err)
				m.reloadPipelineData()
				return m, nil
			}
			m.state = viewPipeline
			m.pipeline, _ = m.pipeline.StartHiredFlow(msg.App)
			m.reloadPipelineData()
			return m, nil
		}
		if normalized == "discarded" || normalized == "skip" {
			m.state = viewPipeline
			m.pipeline, _ = m.pipeline.StartDiscardReasonFlow(msg.App, msg.NewStatus)
			m.reloadPipelineData()
			return m, nil
		}

		err := data.UpdateApplicationStatus(m.careerOpsPath, msg.App, msg.NewStatus)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: status update failed: %v\n", err)
		}
		m.viewer.UpdateAppStatus(msg.NewStatus)
		m.reloadPipelineData()
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
		return m, openCmd(msg.URL)

	case screens.PipelineOpenPDFMsg:
		return m, openCmd(msg.Path)

	case screens.PipelineGeneratePDFMsg:
		return m, runGeneratePDF(msg)

	case guiscreens.OpenListingsMsg:
		m.reloadListings()
		m.state = viewListings
		return m, nil

	case guiscreens.ListingsClosedMsg:
		m.state = viewPipeline
		return m, nil

	case guiscreens.ListingsRefreshMsg:
		m.reloadListings()
		return m, nil

	case guiscreens.ListingsOpenURLMsg:
		return m, openCmd(msg.URL)

	case guiscreens.ListingsScanMsg:
		return m, gui.ScanHandoff(m.careerOpsPath, func() tea.Msg {
			return guiscreens.ListingsRefreshMsg{}
		})

	case guiscreens.ListingsApplyMsg:
		return m, gui.ApplyHandoff(m.careerOpsPath, msg.URL)

	case guiscreens.ListingsEmailMsg:
		return m, gui.OutreachHandoff(m.careerOpsPath, msg.Company, msg.Title, msg.URL, msg.Contacts)

	case guiscreens.ListingsEnrichMsg:
		return m, gui.EnrichCompaniesCmd(m.careerOpsPath, m.listings.Listings())

	case gui.EnrichDoneMsg:
		m.reloadListings()
		m.listings.SetStatus(msg.Summary)
		return m, nil

	case gui.ApplyHandoffDoneMsg:
		m.reloadListings()
		m.reloadPipelineData()
		return m, nil

	case guiscreens.OpenPreferencesMsg:
		prefs, _ := gui.ReadPreferences(m.careerOpsPath)
		m.preferences = guiscreens.NewPreferencesModel(m.theme, prefs, m.careerOpsPath, m.pipeline.Width(), m.pipeline.Height())
		m.state = viewPreferences
		return m, nil

	case guiscreens.PreferencesClosedMsg:
		m.state = viewPipeline
		return m, nil

	case guiscreens.PreferencesSaveMsg:
		err := gui.WritePreferences(msg.CareerOpsPath, msg.Prefs)
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

// openCmd wraps openWithDefaultApp (OS-specific) as a tea.Cmd. Shared by the
// job-URL (`o`) and CV-PDF (`d`) actions.
func openCmd(target string) tea.Cmd {
	return func() tea.Msg {
		if err := openWithDefaultApp(target); err != nil {
			fmt.Fprintf(os.Stderr, "WARN: failed to open %q: %v\n", target, err)
		}
		return nil
	}
}

// runGeneratePDF shells out to node generate-pdf.mjs in the career-ops root,
// opens the resulting PDF on success, and reports the outcome back to the
// pipeline screen as a PipelinePDFGeneratedMsg. Runs in a tea.Cmd goroutine,
// so the UI stays responsive while Chromium renders.
func runGeneratePDF(msg screens.PipelineGeneratePDFMsg) tea.Cmd {
	return func() tea.Msg {
		args := []string{"generate-pdf.mjs", msg.HTMLPath, msg.PDFPath}
		if msg.Format != "" {
			args = append(args, "--format="+msg.Format)
		}
		if msg.ReportNumber != "" {
			args = append(args, "--report="+msg.ReportNumber)
		}
		cmd := exec.Command("node", args...)
		cmd.Dir = msg.CareerOpsPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			return screens.PipelinePDFGeneratedMsg{Err: summarizeCmdError(err, out)}
		}
		pdfAbs := filepath.Join(msg.CareerOpsPath, filepath.FromSlash(msg.PDFPath))
		if err := openWithDefaultApp(pdfAbs); err != nil {
			return screens.PipelinePDFGeneratedMsg{Err: fmt.Sprintf("PDF generated but could not open: %v", err)}
		}
		return screens.PipelinePDFGeneratedMsg{Path: pdfAbs}
	}
}

// summarizeCmdError condenses a failed command into one help-bar-sized line:
// the last non-empty output line when there is one (generate-pdf.mjs prints
// its error there), otherwise the exec error itself.
func summarizeCmdError(err error, out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return err.Error()
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
	langFlag := flag.String("lang", "", "Language for UI (en, tr). Defaults to auto-detect/en.")
	flag.Parse()

	if *langFlag != "" {
		i18n.SetLang(*langFlag)
	} else if os.Getenv("LANG") != "" {
		i18n.SetLang(os.Getenv("LANG"))
	}

	careerOpsPath := *pathFlag

	// Load applications
	apps := data.ParseApplications(careerOpsPath)
	if apps == nil {
		fmt.Fprintf(os.Stderr, "Error: could not find applications.md in %s or %s/data/\n", careerOpsPath, careerOpsPath)
		os.Exit(1)
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
