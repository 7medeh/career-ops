package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/santifer/career-ops/dashboard/internal/data"
	"github.com/santifer/career-ops/dashboard/internal/gui"
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
func (a *App) GetListings() []gui.JobListing {
	listings := gui.ParseListings(a.repoPath)
	if listings == nil {
		return []gui.JobListing{}
	}
	return listings
}

// GetPreferences returns the current salary/location/position preferences.
func (a *App) GetPreferences() (gui.Preferences, error) {
	return gui.ReadPreferences(a.repoPath)
}

// SavePreferences persists edited preferences to profile.yml/portals.yml.
func (a *App) SavePreferences(prefs gui.Preferences) error {
	return gui.WritePreferences(a.repoPath, prefs)
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
// Parameter is rawURL, not url, so it does not shadow the net/url package.
func (a *App) OpenURL(rawURL string) {
	if rawURL == "" {
		return
	}
	wruntime.BrowserOpenURL(a.ctx, rawURL)
}

// Scan runs a native ATS scan (Greenhouse/Ashby/Lever) directly from the
// dashboard -- no Claude, no Terminal. It filters, dedupes, writes new roles to
// pipeline.md/scan-history.tsv, and returns a summary + the new listings.
func (a *App) Scan() (gui.ScanResult, error) {
	return gui.Scan(a.repoPath)
}

// ClearListings empties the listing stores so the next scan repopulates them
// under the current portals.yml filter. See gui.ClearListings for why retuning
// title_filter does nothing without this.
func (a *App) ClearListings() (gui.ClearResult, error) {
	return gui.ClearListings(a.repoPath)
}

// Apply opens a Terminal window running the interactive evaluate+apply pipeline
// for a posting. The session stops before Submit (per modes/apply.md and
// AGENTS.md); the user reviews and submits themselves.
//
// The URL may come from a scanned listing or be pasted by hand into the
// Listings tab, so it is validated here rather than only in the frontend --
// the binding is reachable regardless of what the UI does.
func (a *App) Apply(rawURL string) error {
	jobURL, err := validateJobURL(rawURL)
	if err != nil {
		return err
	}
	prompt := fmt.Sprintf(
		"Evaluate this job posting if it isn't already evaluated, following modes/oferta.md: %s. "+
			"Then run the apply flow from modes/apply.md: curate a tailored resume, and use Chrome "+
			"browser automation to open the posting and fill in the fields you're confident about. "+
			"For anything ambiguous or requiring a judgment call, stop and ask me. "+
			"Never click Submit -- I will review and submit myself.",
		jobURL,
	)
	return a.runInTerminal(fmt.Sprintf("cd %s && exec claude %s",
		shellQuote(a.repoPath), shellQuote(prompt)))
}

// validateJobURL normalizes a job posting URL and rejects anything that is not
// a plain http(s) link.
//
// This is a correctness gate before it is a convenience one: the URL is
// interpolated into a bash command in runInTerminal, and bash still expands
// $(...), backticks and $VAR inside double quotes. shellQuote closes that hole
// on the quoting side; this closes it on the input side, and also spares the
// user a Terminal window that opens only to fail on a typo.
func validateJobURL(rawURL string) (string, error) {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return "", fmt.Errorf("paste a job posting URL first")
	}
	// A pasted link should be a single token. Embedded whitespace means the
	// paste picked up surrounding text (or is deliberately malformed).
	if strings.ContainsAny(s, " \t\r\n") {
		return "", fmt.Errorf("that looks like more than a URL -- paste just the link")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("that does not parse as a URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		if u.Scheme == "" {
			return "", fmt.Errorf("missing http:// or https:// on that link")
		}
		return "", fmt.Errorf("only http and https links are supported (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("that link has no site in it")
	}
	return u.String(), nil
}

// shellQuote wraps s in single quotes for safe use in a bash command line.
//
// Single quotes suppress every bash expansion, so the only character needing
// care is the single quote itself, which is closed, escaped, and reopened.
// fmt's %q is Go quoting, not shell quoting: it escapes " and \ but leaves $
// and backticks intact, which bash would then expand inside double quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
