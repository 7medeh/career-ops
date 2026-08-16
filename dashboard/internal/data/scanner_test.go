package data

import (
	"testing"

	"github.com/santifer/career-ops/dashboard/internal/model"
)

func TestTitlePasses(t *testing.T) {
	positive := []string{"AI", "Full Stack", "Backend"}
	negative := []string{"Junior", "Sales", "Java "}

	cases := []struct {
		title string
		want  bool
	}{
		{"Senior Full Stack Engineer", true},
		{"AI Engineer", true},
		{"Backend Engineer, Payments", true},
		{"Junior Backend Engineer", false}, // negative "Junior"
		{"Account Executive", false},       // no positive
		{"Java Backend Engineer", false},   // negative "Java "
		{"Sales Engineer, AI", false},      // negative "Sales" wins
	}
	for _, c := range cases {
		if got := titlePasses(c.title, positive, negative); got != c.want {
			t.Errorf("titlePasses(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}

func TestLocationPasses(t *testing.T) {
	allowed := []string{"Remote", "New York", "Arlington"}

	cases := []struct {
		name       string
		location   string
		remoteOnly bool
		allowed    []string
		want       bool
	}{
		{"remote matches", "Remote - US", false, allowed, true},
		{"nyc matches", "New York, NY", false, allowed, true},
		{"unlisted city fails", "San Francisco, CA", false, allowed, false},
		{"empty location passes (unknown)", "", false, allowed, true},
		{"remote-only drops onsite", "New York, NY", true, allowed, false},
		{"remote-only keeps remote", "Remote", true, allowed, true},
		{"no allowed list passes all", "Anywhere", false, nil, true},
	}
	for _, c := range cases {
		if got := locationPasses(c.location, c.remoteOnly, c.allowed); got != c.want {
			t.Errorf("%s: locationPasses(%q, remoteOnly=%v) = %v, want %v",
				c.name, c.location, c.remoteOnly, got, c.want)
		}
	}
}

func TestResolveProvider(t *testing.T) {
	cases := []struct {
		company      portalCompany
		wantProvider string
		wantSlug     string
	}{
		{
			portalCompany{API: "https://boards-api.greenhouse.io/v1/boards/anthropic/jobs"},
			"greenhouse", "anthropic",
		},
		{
			portalCompany{API: "https://api.lever.co/v0/postings/tinybird?mode=json"},
			"lever", "tinybird",
		},
		{
			portalCompany{APIProvider: "ashby", APICompany: "openai", API: "https://jobs.ashbyhq.com/api/non-user-graphql?op=ApiJobBoardWithTeams"},
			"ashby", "openai",
		},
		{
			portalCompany{CareersURL: "https://openai.com/careers"}, // websearch-only, no API
			"", "",
		},
	}
	for _, c := range cases {
		gotP, gotS := resolveProvider(c.company)
		if gotP != c.wantProvider || gotS != c.wantSlug {
			t.Errorf("resolveProvider(%+v) = (%q,%q), want (%q,%q)",
				c.company, gotP, gotS, c.wantProvider, c.wantSlug)
		}
	}
}

func TestExtractSalary(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"The expected salary range is $320,000 - $485,000 USD.", "$320K - $485K"},
		{"Compensation: $120,000—$150,000", "$120K - $150K"},
		{"Base pay of $145k for this role", "$145K"},
		{"No compensation mentioned here", ""},
		{"Contact us at extension 4025 for details", ""}, // not a salary
	}
	for _, c := range cases {
		if got := extractSalary(c.text); got != c.want {
			t.Errorf("extractSalary(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestExtractYOE(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"You have 5+ years of experience", 5},
		{"Minimum 3 years; ideally 8 years in the field", 3},
		{"No experience requirement listed", 0},
		{"Over 30 years ago the company was founded", 0}, // >20 ignored
	}
	for _, c := range cases {
		if got := extractYOE(c.text); got != c.want {
			t.Errorf("extractYOE(%q) = %d, want %d", c.text, got, c.want)
		}
	}
}

func TestComputeFit(t *testing.T) {
	fp := fitProfile{candidateYOE: 3, targetMin: 125000, targetTop: 170000}
	positives := []string{"AI", "Full Stack", "Engineer"}

	strong := model.JobListing{Title: "Full Stack Engineer, AI", YOE: 3, Salary: "$150K - $180K", Location: "Remote"}
	weak := model.JobListing{Title: "Staff Backend Engineer", YOE: 10, Salary: "$90K", Location: "Dallas, TX"}

	sFit := computeFit(strong, fp, positives)
	wFit := computeFit(weak, fp, positives)
	if sFit <= wFit {
		t.Errorf("expected strong match (%d) to outrank weak match (%d)", sFit, wFit)
	}
	if sFit < 70 {
		t.Errorf("expected strong match to score high, got %d", sFit)
	}
	if wFit >= 50 {
		t.Errorf("expected weak match to score low, got %d", wFit)
	}
}

func TestParseCompRange(t *testing.T) {
	cases := []struct {
		in     string
		wantLo int
		wantHi int
	}{
		{"$130K-170K", 130000, 170000},
		{"$125K", 125000, 125000},
		{"", 0, 0},
	}
	for _, c := range cases {
		lo, hi := parseCompRange(c.in)
		if lo != c.wantLo || hi != c.wantHi {
			t.Errorf("parseCompRange(%q) = (%d,%d), want (%d,%d)", c.in, lo, hi, c.wantLo, c.wantHi)
		}
	}
}

func TestIsEvaluated(t *testing.T) {
	evaluated := []model.CareerApplication{
		{Company: "Anthropic", Role: "Software Engineer, Full-stack"},
	}
	if !isEvaluated(model.JobListing{Company: "Anthropic", Title: "Software Engineer, Full-stack"}, evaluated) {
		t.Error("exact company+role should be evaluated")
	}
	if isEvaluated(model.JobListing{Company: "Anthropic", Title: "Software Engineer, Desktop"}, evaluated) {
		t.Error("different role at same company should not match (only 2-word overlap 'software engineer' but distinct)")
	}
	if isEvaluated(model.JobListing{Company: "OpenAI", Title: "Software Engineer, Full-stack"}, evaluated) {
		t.Error("same role different company should not match")
	}
}
