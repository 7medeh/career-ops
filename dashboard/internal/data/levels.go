package data

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/santifer/career-ops/dashboard/internal/model"
)

// Company context is sourced from Levels.fyi. Their robots.txt explicitly
// permits user-initiated agents and the default user-agent group, and asks that
// displayed figures be attributed back to Levels.fyi -- the listings screen
// carries that attribution in its footer.
const (
	levelsUserAgent = "career-ops-dashboard/1.0 (+https://github.com/santifer/career-ops)"
	levelsTimeout   = 12 * time.Second
	// profileTTLDays is how long a cached profile is trusted before refetch.
	profileTTLDays = 30
)

// Overridable so tests can point at a local server and skip the pacing delay.
var (
	levelsBaseURL = "https://www.levels.fyi"
	// levelsPolite is the pause between requests. Levels.fyi began soft-throttling
	// (200 with an empty body) at roughly one request per second during testing,
	// so this is deliberately conservative -- a refresh is a rare, cached operation.
	levelsPolite = 2500 * time.Millisecond
)

// reNextData pulls the Next.js data blob out of a Levels.fyi company page.
var reNextData = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)

// reMedianComp matches the median line in /companies/{slug}/salaries.md, e.g.
// "- Median Total Compensation (All Roles): $114,559".
var reMedianComp = regexp.MustCompile(`Median Total Compensation[^:]*:\s*\$([0-9,]+)`)

// levelsCompany mirrors the subset of the Levels.fyi company payload we use.
type levelsCompany struct {
	Name               string `json:"name"`
	Slug               string `json:"slug"`
	CompanyType        string `json:"company_type"`
	FundingStage       string `json:"funding_stage"`
	EmpCount           int    `json:"emp_count"`
	EmployeeCountRange string `json:"employee_count_range"`
	YearFounded        int    `json:"year_founded"`
	Ticker             string `json:"ticker"`
	HQCity             string `json:"hq_city"`
	HQStateCode        string `json:"hq_state_code"`
}

type levelsNextData struct {
	Props struct {
		PageProps struct {
			Company levelsCompany `json:"company"`
		} `json:"pageProps"`
	} `json:"props"`
}

// FetchCompanyProfile resolves a company name on Levels.fyi and returns its
// profile. It returns ok=false when the company cannot be resolved, so callers
// can cache the miss and stop retrying it every refresh.
func FetchCompanyProfile(client *http.Client, company string) (model.CompanyProfile, fetchStatus) {
	slug, c, st := resolveLevelsCompany(client, company)
	if st != fetchOK {
		return model.CompanyProfile{
			Company: company,
			Fetched: nowStamp(),
		}, st
	}

	stage, inferred := deriveStage(c)
	p := model.CompanyProfile{
		Company:        company,
		Slug:           slug,
		Stage:          stage,
		StageInferred:  inferred,
		Headcount:      c.EmpCount,
		HeadcountRange: c.EmployeeCountRange,
		YearFounded:    c.YearFounded,
		HQ:             formatHQ(c),
		MedianComp:     fetchMedianComp(client, slug),
		Fetched:        nowStamp(),
	}
	return p, fetchOK
}

// resolveLevelsCompany probes slug variants until one returns a company payload.
// Levels.fyi slugs are not a pure slugification of the name ("Bland AI" lives at
// /companies/bland, not /companies/bland-ai), so a handful of variants are tried.
func resolveLevelsCompany(client *http.Client, company string) (string, levelsCompany, fetchStatus) {
	for _, slug := range slugCandidates(company) {
		c, st := fetchLevelsCompany(client, slug)
		switch st {
		case fetchOK:
			return slug, c, fetchOK
		case fetchThrottled:
			// Stop immediately: further probes would also be throttled and
			// would only deepen the rate limit.
			return "", levelsCompany{}, fetchThrottled
		}
		time.Sleep(levelsPolite)
	}
	return "", levelsCompany{}, fetchMiss
}

// slugCandidates returns slug guesses in decreasing order of likelihood,
// deduplicated and capped so an unresolvable company costs a bounded number of
// requests.
func slugCandidates(company string) []string {
	base := slugify(company)
	if base == "" {
		return nil
	}

	cands := []string{base}
	// "Bland AI" -> "bland"; "Arize AI" -> "arize"
	if trimmed := strings.TrimSuffix(base, "-ai"); trimmed != base && trimmed != "" {
		cands = append(cands, trimmed)
	}
	// "Bland" -> "bland-ai"
	if !strings.HasSuffix(base, "-ai") {
		cands = append(cands, base+"-ai")
	}
	// "Arize AI" -> "arizeai"
	if collapsed := strings.ReplaceAll(base, "-", ""); collapsed != base {
		cands = append(cands, collapsed)
	}

	seen := make(map[string]bool, len(cands))
	var out []string
	for _, c := range cands {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

var reNonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = reNonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// fetchLevelsCompany GETs a company page and extracts the embedded payload.
func fetchLevelsCompany(client *http.Client, slug string) (levelsCompany, fetchStatus) {
	body, st := levelsGet(client, fmt.Sprintf("%s/companies/%s", levelsBaseURL, slug))
	if st != fetchOK {
		return levelsCompany{}, st
	}
	m := reNextData.FindSubmatch(body)
	if m == nil {
		return levelsCompany{}, fetchMiss
	}
	var nd levelsNextData
	if err := json.Unmarshal(m[1], &nd); err != nil {
		return levelsCompany{}, fetchMiss
	}
	c := nd.Props.PageProps.Company
	if c.Name == "" {
		return levelsCompany{}, fetchMiss
	}
	return c, fetchOK
}

// fetchMedianComp reads the LLM-readable salaries markdown Levels.fyi publishes
// and pulls out the all-roles median. Returns "" when unavailable.
func fetchMedianComp(client *http.Client, slug string) string {
	body, st := levelsGet(client, fmt.Sprintf("%s/companies/%s/salaries.md", levelsBaseURL, slug))
	if st != fetchOK {
		return ""
	}
	m := reMedianComp.FindSubmatch(body)
	if m == nil {
		return ""
	}
	n, err := strconv.Atoi(strings.ReplaceAll(string(m[1]), ",", ""))
	if err != nil || n <= 0 {
		return ""
	}
	return fmt.Sprintf("~$%dK", n/1000)
}

// fetchStatus distinguishes a genuine "no such company" from being rate-limited,
// which matters because only the former should be cached as a miss.
type fetchStatus int

const (
	fetchOK fetchStatus = iota
	fetchMiss
	fetchThrottled
)

func levelsGet(client *http.Client, url string) ([]byte, fetchStatus) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fetchMiss
	}
	req.Header.Set("User-Agent", levelsUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		// A transport error is transient, not evidence the company is absent.
		return nil, fetchThrottled
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return nil, fetchThrottled
	case resp.StatusCode != http.StatusOK:
		return nil, fetchMiss
	}

	// Company pages are ~300KB; cap the read so a surprise response cannot
	// balloon memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fetchThrottled
	}
	// Levels.fyi soft-throttles by answering 200 with an empty body rather than
	// sending 429. Treating that as a miss would poison the cache with false
	// negatives for every company in a throttled run.
	if len(body) == 0 {
		return nil, fetchThrottled
	}
	return body, fetchOK
}

// deriveStage maps a Levels.fyi company onto a stage. funding_stage is only
// populated for roughly 60% of companies, so when it is missing the stage is
// inferred from headcount and company age -- inferred=true marks that in the UI.
func deriveStage(c levelsCompany) (model.CompanyStage, bool) {
	// Public listing is decisive and always reported.
	if strings.EqualFold(c.CompanyType, "public") || c.Ticker != "" {
		return model.StagePublic, false
	}

	switch normalizeFundingStage(c.FundingStage) {
	case "seed", "pre_seed", "angel":
		return model.StageSeed, false
	case "series_a":
		return model.StageSeriesA, false
	case "series_b":
		return model.StageSeriesB, false
	case "series_c":
		return model.StageSeriesC, false
	case "series_d", "series_e_plus", "late_stage", "private_equity":
		return model.StageGrowth, false
	case "post_ipo", "ipo":
		return model.StagePublic, false
	}

	// No reported stage: infer from size, then age.
	if s, ok := inferStageFromSize(c); ok {
		return s, true
	}
	return model.StageUnknown, false
}

func normalizeFundingStage(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.ReplaceAll(s, "-", "_")
}

// inferStageFromSize approximates a stage from headcount, falling back to the
// reported range when the exact count is missing or zero.
func inferStageFromSize(c levelsCompany) (model.CompanyStage, bool) {
	n := c.EmpCount
	if n <= 0 {
		n = midpointOfRange(c.EmployeeCountRange)
	}
	if n <= 0 {
		return model.StageUnknown, false
	}

	switch {
	case n < 25:
		return model.StageSeed, true
	case n < 75:
		return model.StageSeriesA, true
	case n < 200:
		return model.StageSeriesB, true
	case n < 500:
		return model.StageSeriesC, true
	default:
		return model.StageGrowth, true
	}
}

var reRangeNum = regexp.MustCompile(`[0-9,]+`)

// midpointOfRange turns "51-200" or "10,001+" into a representative headcount.
func midpointOfRange(r string) int {
	nums := reRangeNum.FindAllString(r, -1)
	if len(nums) == 0 {
		return 0
	}
	toInt := func(s string) int {
		n, _ := strconv.Atoi(strings.ReplaceAll(s, ",", ""))
		return n
	}
	lo := toInt(nums[0])
	if len(nums) == 1 {
		return lo
	}
	return (lo + toInt(nums[1])) / 2
}

func formatHQ(c levelsCompany) string {
	city := strings.TrimSpace(c.HQCity)
	st := strings.TrimSpace(c.HQStateCode)
	switch {
	case city != "" && st != "":
		return city + ", " + st
	case city != "":
		return city
	default:
		return st
	}
}

// -- cache --

const companyProfilesFile = "company-profiles.tsv"

const companyProfilesHeader = "company\tslug\tstage\tinferred\theadcount\theadcount_range\tfounded\tmedian_comp\thq\tfetched\n"

// LoadCompanyProfiles reads data/company-profiles.tsv keyed by lowercased
// company name. A missing file yields an empty map.
func LoadCompanyProfiles(careerOpsPath string) map[string]model.CompanyProfile {
	out := make(map[string]model.CompanyProfile)
	b, err := os.ReadFile(filepath.Join(careerOpsPath, "data", companyProfilesFile))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "company\t") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 10 {
			continue
		}
		atoi := func(s string) int {
			n, _ := strconv.Atoi(strings.TrimSpace(s))
			return n
		}
		company := strings.TrimSpace(f[0])
		if company == "" {
			continue
		}
		out[strings.ToLower(company)] = model.CompanyProfile{
			Company:        company,
			Slug:           strings.TrimSpace(f[1]),
			Stage:          parseStage(f[2]),
			StageInferred:  strings.TrimSpace(f[3]) == "1",
			Headcount:      atoi(f[4]),
			HeadcountRange: strings.TrimSpace(f[5]),
			YearFounded:    atoi(f[6]),
			MedianComp:     strings.TrimSpace(f[7]),
			HQ:             strings.TrimSpace(f[8]),
			Fetched:        strings.TrimSpace(f[9]),
		}
	}
	return out
}

// SaveCompanyProfiles writes the profile cache back to disk, sorted by company
// so the file stays diff-friendly.
func SaveCompanyProfiles(careerOpsPath string, profiles map[string]model.CompanyProfile) error {
	dir := filepath.Join(careerOpsPath, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}

	var b strings.Builder
	b.WriteString(companyProfilesHeader)
	for _, k := range sortedKeys(profiles) {
		p := profiles[k]
		inferred := "0"
		if p.StageInferred {
			inferred = "1"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			p.Company, p.Slug, stageToken(p.Stage), inferred,
			blankZero(p.Headcount), p.HeadcountRange, blankZero(p.YearFounded),
			p.MedianComp, p.HQ, p.Fetched,
		)
	}
	return os.WriteFile(filepath.Join(dir, companyProfilesFile), []byte(b.String()), 0o644)
}

func blankZero(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func sortedKeys(m map[string]model.CompanyProfile) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort keeps this dependency-free and the map is small.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

var stageTokens = map[model.CompanyStage]string{
	model.StageUnknown: "unknown",
	model.StageSeed:    "seed",
	model.StageSeriesA: "series_a",
	model.StageSeriesB: "series_b",
	model.StageSeriesC: "series_c",
	model.StageGrowth:  "growth",
	model.StagePublic:  "public",
}

func stageToken(s model.CompanyStage) string {
	if t, ok := stageTokens[s]; ok {
		return t
	}
	return "unknown"
}

func parseStage(s string) model.CompanyStage {
	s = strings.ToLower(strings.TrimSpace(s))
	for stage, token := range stageTokens {
		if token == s {
			return stage
		}
	}
	return model.StageUnknown
}

// RefreshResult reports what a company-profile refresh accomplished.
type RefreshResult struct {
	Resolved  int  // companies successfully fetched
	Missed    int  // companies Levels.fyi has no page for
	Remaining int  // companies not attempted, because the run stopped early
	Throttled bool // true when Levels.fyi started rate-limiting us
}

// RefreshCompanyProfiles fetches profiles for every company missing from the
// cache or whose entry is older than profileTTLDays, and writes the cache back.
//
// Companies Levels.fyi genuinely has no page for are cached as misses so they
// are not retried every refresh. Rate limiting is handled differently: the run
// stops at the first throttled response and those companies are left absent
// from the cache, so a later refresh retries them. Caching them as misses would
// silently blank the stage column for 30 days.
func RefreshCompanyProfiles(careerOpsPath string, companies []string) (RefreshResult, error) {
	profiles := LoadCompanyProfiles(careerOpsPath)
	client := &http.Client{Timeout: levelsTimeout}

	var res RefreshResult
	var pending []string
	for _, company := range companies {
		company = strings.TrimSpace(company)
		if company == "" {
			continue
		}
		if existing, ok := profiles[strings.ToLower(company)]; ok && !profileStale(existing) {
			continue
		}
		pending = append(pending, company)
	}

	for i, company := range pending {
		p, st := FetchCompanyProfile(client, company)
		if st == fetchThrottled {
			res.Throttled = true
			res.Remaining = len(pending) - i
			break
		}
		profiles[strings.ToLower(company)] = p
		if st == fetchOK {
			res.Resolved++
		} else {
			res.Missed++
		}
		time.Sleep(levelsPolite)
	}

	if res.Resolved == 0 && res.Missed == 0 {
		return res, nil
	}
	return res, SaveCompanyProfiles(careerOpsPath, profiles)
}

// nowStamp is the cache date format, factored out so tests can build fresh rows.
func nowStamp() string { return time.Now().Format("2006-01-02") }

func profileStale(p model.CompanyProfile) bool {
	t, err := time.Parse("2006-01-02", p.Fetched)
	if err != nil {
		return true
	}
	return time.Since(t) > profileTTLDays*24*time.Hour
}
