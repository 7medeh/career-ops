package gui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugCandidates(t *testing.T) {
	tests := []struct {
		name    string
		company string
		want    []string
	}{
		{"plain name", "Retool", []string{"retool", "retool-ai"}},
		// "Bland AI" lives at /companies/bland, so the -ai-stripped variant must
		// be tried; this is the case that motivated variant probing.
		{"ai suffix", "Bland AI", []string{"bland-ai", "bland", "blandai"}},
		{"spaces", "Arize AI", []string{"arize-ai", "arize", "arizeai"}},
		{"punctuation", "Attio, Inc.", []string{"attio-inc", "attio-inc-ai", "attioinc"}},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slugCandidates(tt.company)
			if len(got) != len(tt.want) {
				t.Fatalf("slugCandidates(%q) = %v, want %v", tt.company, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("candidate %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDeriveStage(t *testing.T) {
	tests := []struct {
		name         string
		company      levelsCompany
		wantStage    CompanyStage
		wantInferred bool
	}{
		{
			name:      "public by company_type",
			company:   levelsCompany{CompanyType: "public", EmpCount: 14000},
			wantStage: StagePublic,
		},
		{
			// A ticker means public even if company_type is stale.
			name:      "public by ticker",
			company:   levelsCompany{CompanyType: "private", Ticker: "NFLX"},
			wantStage: StagePublic,
		},
		{
			name:      "reported series b",
			company:   levelsCompany{CompanyType: "private", FundingStage: "series_b", EmpCount: 150},
			wantStage: StageSeriesB,
		},
		{
			name:      "series e plus maps to growth",
			company:   levelsCompany{CompanyType: "private", FundingStage: "series_e_plus", EmpCount: 580},
			wantStage: StageGrowth,
		},
		{
			name:      "post_ipo maps to public",
			company:   levelsCompany{CompanyType: "private", FundingStage: "post_ipo"},
			wantStage: StagePublic,
		},
		{
			// Arize: private, no funding_stage reported, 90 employees.
			name:         "inferred from headcount",
			company:      levelsCompany{CompanyType: "private", EmpCount: 90},
			wantStage:    StageSeriesB,
			wantInferred: true,
		},
		{
			// Notion reports emp_count 0, so the range must carry the inference.
			name:         "inferred from range when count is zero",
			company:      levelsCompany{CompanyType: "private", EmpCount: 0, EmployeeCountRange: "201-1,000"},
			wantStage:    StageGrowth,
			wantInferred: true,
		},
		{
			name:         "tiny company infers seed",
			company:      levelsCompany{CompanyType: "private", EmpCount: 12},
			wantStage:    StageSeed,
			wantInferred: true,
		},
		{
			name:      "no signal stays unknown",
			company:   levelsCompany{CompanyType: "private"},
			wantStage: StageUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, inferred := deriveStage(tt.company)
			if stage != tt.wantStage {
				t.Errorf("stage = %v, want %v", stage, tt.wantStage)
			}
			if inferred != tt.wantInferred {
				t.Errorf("inferred = %v, want %v", inferred, tt.wantInferred)
			}
		})
	}
}

func TestIsStartup(t *testing.T) {
	startups := []CompanyStage{StageSeed, StageSeriesA, StageSeriesB, StageSeriesC}
	for _, s := range startups {
		if !s.IsStartup() {
			t.Errorf("%v should count as a startup", s)
		}
	}
	for _, s := range []CompanyStage{StageUnknown, StageGrowth, StagePublic} {
		if s.IsStartup() {
			t.Errorf("%v should not count as a startup", s)
		}
	}
}

func TestMidpointOfRange(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"51-200", 125},
		{"10,001+", 10001},
		{"1,001-10,000", 5500},
		{"", 0},
		{"n/a", 0},
	}
	for _, tt := range tests {
		if got := midpointOfRange(tt.in); got != tt.want {
			t.Errorf("midpointOfRange(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCompanyProfilesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	profiles := map[string]CompanyProfile{
		"arize ai": {
			Company: "Arize AI", Slug: "arize-ai", Stage: StageSeriesB,
			StageInferred: true, Headcount: 90, HeadcountRange: "51-200",
			YearFounded: 0, MedianComp: "~$147K", HQ: "Berkeley, CA", Fetched: "2026-08-15",
		},
		"netflix": {
			Company: "Netflix", Slug: "netflix", Stage: StagePublic,
			Headcount: 14000, HeadcountRange: "10,001+", YearFounded: 1997,
			MedianComp: "~$450K", HQ: "Los Gatos, CA", Fetched: "2026-08-15",
		},
	}

	if err := SaveCompanyProfiles(dir, profiles); err != nil {
		t.Fatalf("SaveCompanyProfiles: %v", err)
	}
	got := LoadCompanyProfiles(dir)
	if len(got) != 2 {
		t.Fatalf("loaded %d profiles, want 2", len(got))
	}

	arize := got["arize ai"]
	if arize.Stage != StageSeriesB || !arize.StageInferred {
		t.Errorf("arize stage = %v inferred=%v, want SeriesB inferred=true", arize.Stage, arize.StageInferred)
	}
	if arize.Headcount != 90 || arize.MedianComp != "~$147K" || arize.HQ != "Berkeley, CA" {
		t.Errorf("arize round-trip mismatch: %+v", arize)
	}

	nf := got["netflix"]
	if nf.Stage != StagePublic || nf.StageInferred {
		t.Errorf("netflix stage = %v inferred=%v, want Public inferred=false", nf.Stage, nf.StageInferred)
	}
	if nf.YearFounded != 1997 {
		t.Errorf("netflix YearFounded = %d, want 1997", nf.YearFounded)
	}
}

func TestLoadCompanyProfilesMissingFile(t *testing.T) {
	if got := LoadCompanyProfiles(t.TempDir()); len(got) != 0 {
		t.Errorf("expected empty map for missing file, got %d entries", len(got))
	}
}

func TestApplyCompanyProfiles(t *testing.T) {
	listings := []JobListing{
		{Company: "Arize AI", Title: "Engineer"},
		// Salary and location already present: the overlay must not clobber them.
		{Company: "Netflix", Title: "SWE", Salary: "$225K-$360K", Location: "Remote"},
		{Company: "Unknown Co", Title: "Dev"},
	}
	profiles := map[string]CompanyProfile{
		"arize ai": {Stage: StageSeriesB, StageInferred: true, Headcount: 90, MedianComp: "~$147K", HQ: "Berkeley, CA"},
		"netflix":  {Stage: StagePublic, Headcount: 14000, MedianComp: "~$450K", HQ: "Los Gatos, CA"},
	}

	applyCompanyProfiles(listings, profiles)

	if listings[0].Stage != StageSeriesB || !listings[0].StageInferred {
		t.Errorf("arize stage not applied: %+v", listings[0])
	}
	if listings[0].Salary != "~$147K" || !listings[0].SalaryEstimated {
		t.Errorf("arize salary should be filled and flagged estimated, got %q est=%v", listings[0].Salary, listings[0].SalaryEstimated)
	}
	if listings[0].Location != "Berkeley, CA" {
		t.Errorf("arize location = %q, want Berkeley, CA", listings[0].Location)
	}

	if listings[1].Salary != "$225K-$360K" || listings[1].SalaryEstimated {
		t.Errorf("posted salary must win over the median: %+v", listings[1])
	}
	if listings[1].Location != "Remote" {
		t.Errorf("posted location must win over HQ, got %q", listings[1].Location)
	}

	if listings[2].Stage != StageUnknown {
		t.Errorf("unmatched company should stay unknown, got %v", listings[2].Stage)
	}
}

func TestListingCompanies(t *testing.T) {
	listings := []JobListing{
		{Company: "Arize AI"}, {Company: "Netflix"}, {Company: "arize ai"},
		{Company: ""}, {Company: "Netflix"},
	}
	got := ListingCompanies(listings)
	want := []string{"Arize AI", "Netflix"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("company %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestProfileStale(t *testing.T) {
	if !profileStale(CompanyProfile{Fetched: "2020-01-01"}) {
		t.Error("old profile should be stale")
	}
	if !profileStale(CompanyProfile{Fetched: ""}) {
		t.Error("unparseable date should be treated as stale")
	}
	if profileStale(CompanyProfile{Fetched: nowStamp()}) {
		t.Error("profile fetched today should be fresh")
	}
}

func TestFetchMedianCompParsing(t *testing.T) {
	// Exercises the regex against the shape Levels.fyi's salaries.md emits.
	md := "## Aggregate Highlights\n- Median Total Compensation (All Roles): $114,559  \n- Last Updated: August 16, 2026\n"
	m := reMedianComp.FindStringSubmatch(md)
	if m == nil {
		t.Fatal("median regex did not match the documented format")
	}
	if m[1] != "114,559" {
		t.Errorf("captured %q, want 114,559", m[1])
	}
}

func TestEnsureContactsFile(t *testing.T) {
	dir := t.TempDir()

	path, err := EnsureContactsFile(dir)
	if err != nil {
		t.Fatalf("EnsureContactsFile: %v", err)
	}
	if filepath.Base(path) != "contacts.tsv" {
		t.Errorf("path = %q, want it to end in contacts.tsv", path)
	}

	// A second call must not overwrite candidate-entered rows.
	custom := contactsHeader + "Attio\tJane Doe\tVP Engineering\tjane@attio.com\tcompany site\n"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureContactsFile(dir); err != nil {
		t.Fatalf("second EnsureContactsFile: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != custom {
		t.Error("EnsureContactsFile overwrote an existing contacts file")
	}
}

func TestLoadContacts(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := contactsHeader +
		"# a comment line\n" +
		"Attio\tJane Doe\tVP Engineering\tjane@attio.com\tcompany site\n" +
		"Attio\tSam Roe\tCTO\tsam@attio.com\treferral\n" +
		"Netflix\tPat Lee\tDirector\tpat@netflix.com\n" +
		"NoEmail\tNo One\tCEO\t\tnowhere\n" +
		"\n"
	if err := os.WriteFile(filepath.Join(dir, "data", "contacts.tsv"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	contacts := LoadContacts(dir)

	attio := ContactsFor(contacts, "attio")
	if len(attio) != 2 {
		t.Fatalf("got %d Attio contacts, want 2", len(attio))
	}
	if attio[0].Name != "Jane Doe" || attio[0].Title != "VP Engineering" || attio[0].Source != "company site" {
		t.Errorf("first Attio contact mismatch: %+v", attio[0])
	}

	// Lookup is case-insensitive on company name.
	if len(ContactsFor(contacts, "NETFLIX")) != 1 {
		t.Error("expected case-insensitive company lookup")
	}
	// A row with no email is unusable for outreach and must be dropped.
	if len(ContactsFor(contacts, "NoEmail")) != 0 {
		t.Error("rows without an email should be skipped")
	}
	if len(ContactsFor(contacts, "Nobody")) != 0 {
		t.Error("unknown company should return no contacts")
	}
}

func TestLoadContactsMissingFile(t *testing.T) {
	if got := LoadContacts(t.TempDir()); len(got) != 0 {
		t.Errorf("expected empty map for missing file, got %d", len(got))
	}
}
