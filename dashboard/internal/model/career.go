package model

// CareerApplication represents a single job application from the tracker.
type CareerApplication struct {
	Number       int
	Date         string
	Company      string
	Role         string
	Status       string
	Score        float64
	ScoreRaw     string
	HasPDF       bool
	ReportPath   string
	ReportNumber string
	Notes        string
	JobURL       string // URL of the original job posting
	// Enrichment (lazy loaded from report)
	Archetype    string
	TlDr         string
	Remote       string
	CompEstimate string
}

// PipelineMetrics holds aggregate stats for the pipeline dashboard.
type PipelineMetrics struct {
	Total      int
	ByStatus   map[string]int
	AvgScore   float64
	TopScore   float64
	WithPDF    int
	Actionable int
}

// ProgressMetrics holds job search progress analytics.
type ProgressMetrics struct {
	// Funnel
	FunnelStages []FunnelStage

	// Score distribution
	ScoreBuckets []ScoreBucket

	// Timeline (weekly activity)
	WeeklyActivity []WeekActivity

	// Rates
	ResponseRate  float64 // Responded / Applied
	InterviewRate float64 // Interview / Applied
	OfferRate     float64 // Offer / Applied

	// Averages
	AvgScore    float64
	TopScore    float64
	TotalOffers int
	ActiveApps  int // not skip/rejected/discarded
}

// FunnelStage represents one stage of the application funnel.
type FunnelStage struct {
	Label string
	Count int
	Pct   float64 // percentage of total
}

// ScoreBucket represents a score range and its count.
type ScoreBucket struct {
	Label string // e.g., "4.5-5.0", "4.0-4.4", "3.5-3.9", "3.0-3.4", "<3.0"
	Count int
}

// WeekActivity represents application activity for a given ISO week.
type WeekActivity struct {
	Week  string // e.g., "2026-W14", "2026-W13"
	Count int
}

// Preferences holds the salary/location/position targeting fields sourced
// from config/profile.yml and portals.yml's title_filter.positive list.
type Preferences struct {
	SalaryTarget        string
	SalaryMinimum       string
	LocationFlexibility string
	PositionKeywords    []string
}

// JobListing represents a discovered-but-not-yet-evaluated job posting,
// sourced from data/pipeline.md and data/scan-history.tsv.
type JobListing struct {
	URL       string
	Company   string
	Title     string
	Location  string // best-effort location text from the ATS
	Salary    string // best-effort; "" if not captured at scan time
	YOE       int    // required years of experience parsed from the JD; 0 = unknown
	Fit       int    // heuristic fit score 0-100; 0 = not computed
	FirstSeen string
	Portal    string
	Source    string // "pipeline" or "scan-history"

	// Company context, overlaid from data/company-profiles.tsv (Levels.fyi).
	Stage           CompanyStage // funding stage / maturity; StageUnknown if not resolved
	StageInferred   bool         // true when Stage was derived from headcount+age, not reported
	Headcount       int          // employee count; 0 = unknown
	HeadcountRange  string       // e.g. "51-200"; "" = unknown
	SalaryEstimated bool         // true when Salary came from Levels.fyi medians, not the posting
}

// CompanyStage is a company's funding/maturity stage, used to flag startups.
type CompanyStage int

const (
	StageUnknown CompanyStage = iota
	StageSeed
	StageSeriesA
	StageSeriesB
	StageSeriesC
	StageGrowth // series D/E+, late-stage private
	StagePublic
)

// IsStartup reports whether the stage is one a candidate would consider a startup.
// Public companies never qualify; unknown stages are not guessed at here.
func (s CompanyStage) IsStartup() bool {
	return s >= StageSeed && s <= StageSeriesC
}

// String renders the stage for display. StageUnknown renders empty so callers
// can substitute their own placeholder.
func (s CompanyStage) String() string {
	switch s {
	case StageSeed:
		return "Seed"
	case StageSeriesA:
		return "Series A"
	case StageSeriesB:
		return "Series B"
	case StageSeriesC:
		return "Series C"
	case StageGrowth:
		return "Growth"
	case StagePublic:
		return "Public"
	default:
		return ""
	}
}

// CompanyProfile is company-level context sourced from Levels.fyi and cached in
// data/company-profiles.tsv. One row per company name.
type CompanyProfile struct {
	Company        string
	Slug           string // Levels.fyi slug; "" when the company could not be resolved
	Stage          CompanyStage
	StageInferred  bool
	Headcount      int
	HeadcountRange string
	YearFounded    int
	MedianComp     string // company-wide median total comp, e.g. "$147K"
	HQ             string // "City, ST"
	Fetched        string // YYYY-MM-DD, for cache staleness
}

// Contact is a person at a target company, stored in data/contacts.tsv.
// Populated by the candidate; never scraped.
type Contact struct {
	Company string
	Name    string
	Title   string
	Email   string
	Source  string // where the candidate found it, e.g. "company site", "referral"
}
