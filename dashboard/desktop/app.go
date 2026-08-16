package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/santifer/career-ops/dashboard/internal/data"
	"github.com/santifer/career-ops/dashboard/internal/model"
)

// App is the Wails backend. Its exported methods are bound to the frontend and
// are thin adapters over the existing internal/data layer -- no parsing or file
// logic is reimplemented here.
type App struct {
	ctx      context.Context
	repoPath string
}

// NewApp creates the backend bound to a career-ops repo path.
func NewApp(repoPath string) *App {
	return &App{repoPath: repoPath}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// PipelinePayload is everything the Pipeline tab needs in one call.
type PipelinePayload struct {
	Apps     []model.CareerApplication `json:"apps"`
	Metrics  model.PipelineMetrics     `json:"metrics"`
	Progress model.ProgressMetrics     `json:"progress"`
}

// GetApplications returns the tracker rows (enriched with report-summary fields)
// plus aggregate metrics.
func (a *App) GetApplications() PipelinePayload {
	apps := data.ParseApplications(a.repoPath)
	metrics := data.ComputeMetrics(apps)
	progress := data.ComputeProgressMetrics(apps)

	for i := range apps {
		if apps[i].ReportPath != "" {
			arch, tldr, remote, comp := data.LoadReportSummary(a.repoPath, apps[i].ReportPath)
			apps[i].Archetype, apps[i].TlDr, apps[i].Remote, apps[i].CompEstimate = arch, tldr, remote, comp
		}
	}
	if apps == nil {
		apps = []model.CareerApplication{}
	}

	return PipelinePayload{Apps: apps, Metrics: metrics, Progress: progress}
}

// GetListings returns open, not-yet-evaluated postings.
func (a *App) GetListings() []model.JobListing {
	listings := data.ParseListings(a.repoPath)
	if listings == nil {
		return []model.JobListing{}
	}
	return listings
}

// GetPreferences returns the current salary/location/position preferences.
func (a *App) GetPreferences() (model.Preferences, error) {
	return data.ReadPreferences(a.repoPath)
}

// SavePreferences persists edited preferences to profile.yml/portals.yml.
func (a *App) SavePreferences(prefs model.Preferences) error {
	return data.WritePreferences(a.repoPath, prefs)
}

// UpdateStatus changes an application's status in applications.md, matched by
// its report number.
func (a *App) UpdateStatus(reportNumber, newStatus string) error {
	apps := data.ParseApplications(a.repoPath)
	for _, app := range apps {
		if app.ReportNumber == reportNumber {
			return data.UpdateApplicationStatus(a.repoPath, app, newStatus)
		}
	}
	return fmt.Errorf("application with report %s not found", reportNumber)
}

// OpenURL opens a job posting in the user's default browser.
func (a *App) OpenURL(url string) {
	if url == "" {
		return
	}
	wruntime.BrowserOpenURL(a.ctx, url)
}

// Scan runs a native ATS scan (Greenhouse/Ashby/Lever) directly from the
// dashboard -- no Claude, no Terminal. It filters, dedupes, writes new roles to
// pipeline.md/scan-history.tsv, and returns a summary + the new listings.
func (a *App) Scan() (data.ScanResult, error) {
	return data.Scan(a.repoPath)
}

// Apply opens a Terminal window running the interactive evaluate+apply pipeline
// for a posting. The session stops before Submit (per modes/apply.md and
// AGENTS.md); the user reviews and submits themselves.
func (a *App) Apply(url string) error {
	prompt := fmt.Sprintf(
		"Evaluate this job posting if it isn't already evaluated, following modes/oferta.md: %s. "+
			"Then run the apply flow from modes/apply.md: curate a tailored resume, and use Chrome "+
			"browser automation to open the posting and fill in the fields you're confident about. "+
			"For anything ambiguous or requiring a judgment call, stop and ask me. "+
			"Never click Submit -- I will review and submit myself.",
		url,
	)
	return a.runInTerminal(fmt.Sprintf("cd %q && exec claude %q", a.repoPath, prompt))
}

// runInTerminal writes a one-shot launcher script and opens it in a new Terminal
// window via AppleScript. Writing the command to a temp script avoids fragile
// nested shell/AppleScript quoting for long prompts.
func (a *App) runInTerminal(bashBody string) error {
	f, err := os.CreateTemp("", "career-ops-launch-*.sh")
	if err != nil {
		return err
	}
	script := "#!/bin/bash\n" +
		"export PATH=\"/opt/homebrew/bin:/usr/local/bin:$PATH\"\n" +
		bashBody + "\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		return err
	}

	// AppleScript path is single-quoted for the shell; temp path has no quotes.
	appleScript := fmt.Sprintf(
		"tell application \"Terminal\"\nactivate\ndo script \"'%s'\"\nend tell",
		f.Name(),
	)
	return exec.Command("osascript", "-e", appleScript).Run()
}

// resolveRepoPath returns an absolute, cleaned repo path, defaulting to the
// current working directory when none is supplied.
func resolveRepoPath(p string) string {
	if p == "" {
		p = "."
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return strings.TrimSpace(p)
	}
	return abs
}
