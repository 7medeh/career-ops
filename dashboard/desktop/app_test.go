package main

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestValidateJobURL(t *testing.T) {
	t.Parallel()

	valid := []struct{ name, in, want string }{
		{"greenhouse posting", "https://job-boards.greenhouse.io/twilio/jobs/7946610", "https://job-boards.greenhouse.io/twilio/jobs/7946610"},
		{"surrounding whitespace trimmed", "  https://example.com/jobs/1  ", "https://example.com/jobs/1"},
		{"plain http allowed", "http://example.com/jobs/1", "http://example.com/jobs/1"},
		{"query string preserved", "https://boards.eu.lever.co/acme/abc?src=li", "https://boards.eu.lever.co/acme/abc?src=li"},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateJobURL(tc.in)
			if err != nil {
				t.Fatalf("validateJobURL(%q) returned error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("validateJobURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	invalid := []struct{ name, in string }{
		{"empty", ""},
		{"whitespace only", "   "},
		{"no scheme", "job-boards.greenhouse.io/twilio/jobs/7946610"},
		{"wrong scheme", "ftp://example.com/jobs/1"},
		{"file scheme", "file:///etc/passwd"},
		{"javascript scheme", "javascript:alert(1)"},
		{"no host", "https://"},
		{"pasted with trailing prose", "https://example.com/jobs/1 and here is the role"},
		{"embedded newline", "https://example.com/jobs/1\nrm -rf ~"},
		{"embedded tab", "https://example.com/jobs/1\tfoo"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := validateJobURL(tc.in); err == nil {
				t.Errorf("validateJobURL(%q) = %q, want an error", tc.in, got)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	cases := []struct{ in, want string }{
		{"plain", `'plain'`},
		{"with space", `'with space'`},
		{"it's", `'it'\''s'`},
		{"$HOME", `'$HOME'`},
		{"`id`", "'`id`'"},
		{"", `''`},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// hostileArgs are strings bash would act on if they reached it unquoted.
// Each is chosen so that executing it yields text different from the literal,
// which is what makes the round-trip assertion meaningful.
var hostileArgs = []string{
	"https://example.com/$(echo oops)",
	"https://example.com/`echo oops`",
	"https://example.com/$HOME",
	"https://example.com/it's",
}

// TestShellQuoteSuppressesExpansion runs each quoted string through the real
// bash and requires it to come back byte-identical. fmt's %q -- what Apply used
// before -- leaves $ and backticks live inside double quotes, so a pasted URL
// could inject a command into the launcher script.
func TestShellQuoteSuppressesExpansion(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	for _, in := range hostileArgs {
		out, err := exec.Command(bash, "-c", "printf %s "+shellQuote(in)).Output()
		if err != nil {
			t.Fatalf("bash failed for %q: %v", in, err)
		}
		if string(out) != in {
			t.Errorf("bash altered %q into %q -- quoting is not airtight", in, string(out))
		}
	}
}

// TestFmtQuoteWouldHaveExpanded is the positive control for the test above: it
// pins the actual bug that motivated shellQuote, so the round-trip assertion
// cannot quietly become a tautology that passes with any implementation.
func TestFmtQuoteWouldHaveExpanded(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	expanded := 0
	for _, in := range hostileArgs {
		out, err := exec.Command(bash, "-c", "printf %s "+fmt.Sprintf("%q", in)).Output()
		if err != nil {
			continue // %q can also produce something bash rejects outright
		}
		if string(out) != in {
			expanded++
		}
	}
	if expanded == 0 {
		t.Error("Go-style quoting no longer differs from shellQuote here -- the control has lost its teeth")
	}
}

// TestApplyRejectsBadURLBeforeLaunching guards the ordering: validation must
// run before runInTerminal, so a bad paste never opens a Terminal window.
func TestApplyRejectsBadURLBeforeLaunching(t *testing.T) {
	t.Parallel()

	a := NewApp(t.TempDir())
	for _, in := range []string{"", "not a url", "javascript:alert(1)", "ftp://example.com/x"} {
		if err := a.Apply(in); err == nil {
			t.Errorf("Apply(%q) = nil, want an error", in)
		}
	}
}
