package gui

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ApplyHandoffDoneMsg is emitted after an interactive claude session exits, so
// both the listings view and the underlying application tracker (applications.md
// may have gained a new entry) get refreshed.
type ApplyHandoffDoneMsg struct{}

// EnrichDoneMsg signals that the Levels.fyi company-profile refresh finished.
type EnrichDoneMsg struct{ Summary string }

// ScanHandoff hands off to an interactive `/career-ops scan` session.
func ScanHandoff(careerOpsPath string, done func() tea.Msg) tea.Cmd {
	cmd := exec.Command("claude", "/career-ops scan")
	cmd.Dir = careerOpsPath
	return tea.ExecProcess(cmd, func(error) tea.Msg { return done() })
}

// ApplyHandoff hands off to an interactive claude session that evaluates the
// posting and drives the application form. It never submits -- the candidate
// reviews and submits themselves.
func ApplyHandoff(careerOpsPath, url string) tea.Cmd {
	prompt := fmt.Sprintf(
		"Evaluate %s if not already evaluated (mode: oferta), then run the apply pipeline: "+
			"curate a tailored resume, then use Chrome browser automation to open the posting "+
			"and fill in the fields you're confident about. For anything ambiguous, or requiring "+
			"a judgment call, stop and ask me. Never click Submit -- I will review and submit myself.",
		url,
	)
	cmd := exec.Command("claude", prompt)
	cmd.Dir = careerOpsPath
	return tea.ExecProcess(cmd, func(error) tea.Msg { return ApplyHandoffDoneMsg{} })
}

// OutreachHandoff drafts an outreach email for a listing by handing off to an
// interactive claude session. The draft is always left for review -- the
// dashboard never sends mail itself.
func OutreachHandoff(careerOpsPath, company, title, url string, contacts []Contact) tea.Cmd {
	// Contacts come from data/contacts.tsv, which the candidate maintains.
	// When a company has none, the draft is still produced with the recipient
	// left blank so it can be addressed once a contact is found.
	recipient := "No contact on file for this company in data/contacts.tsv. " +
		"Draft the email with the To: line left blank, and note which role at the " +
		"company would be the right person to reach (for example the hiring manager " +
		"named in the posting, or a VP of Engineering). Do not search for, guess, or " +
		"construct anyone's email address."
	if len(contacts) > 0 {
		var b strings.Builder
		b.WriteString("Contacts on file for this company (from data/contacts.tsv):\n")
		for _, c := range contacts {
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
		title, company, url, recipient, slugifyCompany(company),
	)

	cmd := exec.Command("claude", prompt)
	cmd.Dir = careerOpsPath
	return tea.ExecProcess(cmd, func(error) tea.Msg { return ApplyHandoffDoneMsg{} })
}

// EnrichCompaniesCmd refreshes cached Levels.fyi profiles for every company in
// the given listings. Network work runs off the UI thread.
func EnrichCompaniesCmd(careerOpsPath string, listings []JobListing) tea.Cmd {
	companies := ListingCompanies(listings)
	return func() tea.Msg {
		res, err := RefreshCompanyProfiles(careerOpsPath, companies)
		return EnrichDoneMsg{Summary: enrichSummary(res, err)}
	}
}

// enrichSummary turns a refresh result into a one-line header message. Being
// rate-limited is called out explicitly so a partial refresh is not mistaken
// for missing data.
func enrichSummary(res RefreshResult, err error) string {
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
