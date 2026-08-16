package gui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/santifer/career-ops/dashboard/internal/data"
	"github.com/santifer/career-ops/dashboard/internal/model"
)

// ScanResult summarizes a native portal scan.
type ScanResult struct {
	CompaniesScanned int          `json:"companiesScanned"`
	Found            int          `json:"found"`
	Added            int          `json:"added"`
	Enriched         int          `json:"enriched"`
	SkippedTitle     int          `json:"skippedTitle"`
	SkippedLocation  int          `json:"skippedLocation"`
	SkippedDup       int          `json:"skippedDup"`
	Errors           []string     `json:"errors"`
	NewListings      []JobListing `json:"newListings"`
}

// listingDetail holds the enrichment attached to a listing by URL. It lives in
// data/listing-details.tsv, separate from the (possibly agent-written)
// scan-history.tsv, so enrichment is non-destructive and works for listings
// that predate the native scanner.
type listingDetail struct {
	Salary string
	YOE    int
	Fit    int
}

// portalsFile is the subset of portals.yml the native scanner needs.
type portalsFile struct {
	TitleFilter struct {
		Positive []string `yaml:"positive"`
		Negative []string `yaml:"negative"`
	} `yaml:"title_filter"`
	LocationFilter struct {
		RemoteOnly       bool     `yaml:"remote_only"`
		AllowedLocations []string `yaml:"allowed_locations"`
	} `yaml:"location_filter"`
	TrackedCompanies []portalCompany `yaml:"tracked_companies"`
}

type portalCompany struct {
	Name        string `yaml:"name"`
	CareersURL  string `yaml:"careers_url"`
	API         string `yaml:"api"`
	APIProvider string `yaml:"api_provider"`
	APICompany  string `yaml:"api_company"`
	Enabled     bool   `yaml:"enabled"`
}

var (
	reGreenhouseSlug = regexp.MustCompile(`greenhouse\.io/v1/boards/([^/]+)/jobs`)
	reLeverSlug      = regexp.MustCompile(`lever\.co/v0/postings/([^/?]+)`)
	reAshbySlug      = regexp.MustCompile(`ashbyhq\.com/([^/?]+)`)
)

var scanHTTP = &http.Client{Timeout: 20 * time.Second}

// Scan reads portals.yml, fetches open roles directly from ATS APIs
// (Greenhouse / Ashby / Lever), filters by title + location, dedupes against
// the tracker/pipeline/history, appends new roles to data/pipeline.md and
// data/scan-history.tsv, and returns the newly added listings.
func Scan(careerOpsPath string) (ScanResult, error) {
	var result ScanResult

	cfg, err := loadPortals(careerOpsPath)
	if err != nil {
		return result, err
	}

	// Fetch all enabled companies with a resolvable ATS API, concurrently.
	type companyJobs struct {
		jobs []JobListing
		err  string
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []companyJobs
		sem     = make(chan struct{}, 8) // bound concurrency
	)

	for _, c := range cfg.TrackedCompanies {
		if !c.Enabled {
			continue
		}
		provider, slug := resolveProvider(c)
		if provider == "" || slug == "" {
			continue
		}
		result.CompaniesScanned++
		wg.Add(1)
		go func(c portalCompany, provider, slug string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			jobs, ferr := fetchCompany(c.Name, provider, slug)
			mu.Lock()
			if ferr != nil {
				results = append(results, companyJobs{err: fmt.Sprintf("%s: %v", c.Name, ferr)})
			} else {
				results = append(results, companyJobs{jobs: jobs})
			}
			mu.Unlock()
		}(c, provider, slug)
	}
	wg.Wait()

	var all []JobListing
	for _, r := range results {
		if r.err != "" {
			result.Errors = append(result.Errors, r.err)
			continue
		}
		all = append(all, r.jobs...)
	}
	result.Found = len(all)

	// Dedup sources.
	seenURLs := loadSeenURLs(careerOpsPath)
	evaluated := data.ParseApplications(careerOpsPath)

	today := time.Now().Format("2006-01-02")
	var toAdd []JobListing
	dedupInBatch := make(map[string]bool)

	for _, job := range all {
		if job.URL == "" || dedupInBatch[job.URL] {
			continue
		}
		// Title filter.
		if !titlePasses(job.Title, cfg.TitleFilter.Positive, cfg.TitleFilter.Negative) {
			result.SkippedTitle++
			continue
		}
		// Location filter.
		if !locationPasses(job.Location, cfg.LocationFilter.RemoteOnly, cfg.LocationFilter.AllowedLocations) {
			result.SkippedLocation++
			continue
		}
		// Dedup against history/pipeline (URL) + tracker (company+role).
		if seenURLs[job.URL] || isEvaluated(job, evaluated) {
			result.SkippedDup++
			continue
		}
		dedupInBatch[job.URL] = true
		job.FirstSeen = today
		toAdd = append(toAdd, job)
	}

	// Enrich the new roles with salary + YOE from each job's detail page
	// (bounded concurrency), then score fit. Only the post-filter set is
	// enriched, so a scan makes at most one extra request per newly-added role.
	enrichDetails(toAdd)
	fit := loadFitProfile(careerOpsPath)
	for i := range toAdd {
		toAdd[i].Fit = computeFit(toAdd[i], fit, cfg.TitleFilter.Positive)
	}

	sort.SliceStable(toAdd, func(i, j int) bool {
		if toAdd[i].Fit != toAdd[j].Fit {
			return toAdd[i].Fit > toAdd[j].Fit // best fit first
		}
		if toAdd[i].Company != toAdd[j].Company {
			return strings.ToLower(toAdd[i].Company) < strings.ToLower(toAdd[j].Company)
		}
		return toAdd[i].Title < toAdd[j].Title
	})

	if err := appendToPipeline(careerOpsPath, toAdd); err != nil {
		return result, err
	}
	if err := appendToScanHistory(careerOpsPath, toAdd, today); err != nil {
		return result, err
	}

	// Persist enrichment for the newly-added roles, then backfill any existing
	// pending listings that still lack a fit score (e.g. rows written by an
	// earlier agent-style scan before the native scanner existed).
	details := loadListingDetails(careerOpsPath)
	for _, l := range toAdd {
		details[l.URL] = listingDetail{Salary: l.Salary, YOE: l.YOE, Fit: l.Fit}
	}
	result.Enriched = backfillExisting(careerOpsPath, details, fit, cfg.TitleFilter.Positive)
	if err := saveListingDetails(careerOpsPath, details); err != nil {
		return result, err
	}

	result.Added = len(toAdd)
	result.NewListings = toAdd
	if result.NewListings == nil {
		result.NewListings = []JobListing{}
	}
	return result, nil
}

// backfillExisting enriches every current pending listing that has no fit score
// yet, fetching salary/YOE from the job detail where possible and computing fit.
// It mutates details in place and returns how many listings were enriched.
func backfillExisting(careerOpsPath string, details map[string]listingDetail, fp fitProfile, positives []string) int {
	listings := ParseListings(careerOpsPath)

	type job struct {
		idx     int
		listing JobListing
	}
	var todo []job
	for i, l := range listings {
		if _, ok := details[l.URL]; ok {
			continue // already enriched
		}
		todo = append(todo, job{i, l})
	}
	if len(todo) == 0 {
		return 0
	}

	results := make([]listingDetail, len(todo))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for k := range todo {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			l := todo[k].listing
			salary, yoe := enrichFromDetail(l.URL)
			if salary == "" {
				salary = l.Salary
			}
			if yoe == 0 {
				yoe = l.YOE
			}
			l.Salary, l.YOE = salary, yoe
			results[k] = listingDetail{Salary: salary, YOE: yoe, Fit: computeFit(l, fp, positives)}
		}(k)
	}
	wg.Wait()

	for k, t := range todo {
		details[t.listing.URL] = results[k]
	}
	return len(todo)
}

// enrichFromDetail fetches salary + YOE from a job's detail page, dispatching on
// the URL's ATS host. Ashby detail isn't fetched (no per-post description in the
// public API path we use), so those rely on any list-level comp only.
func enrichFromDetail(jobURL string) (salary string, yoe int) {
	switch {
	case strings.Contains(jobURL, "greenhouse.io"):
		return fetchGreenhouseDetail(jobURL)
	case strings.Contains(jobURL, "lever.co"):
		return fetchLeverDetail(jobURL)
	}
	return "", 0
}

func loadPortals(careerOpsPath string) (portalsFile, error) {
	var cfg portalsFile
	b, err := os.ReadFile(filepath.Join(careerOpsPath, "portals.yml"))
	if err != nil {
		return cfg, fmt.Errorf("reading portals.yml: %w", err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing portals.yml: %w", err)
	}
	return cfg, nil
}

// resolveProvider determines the ATS provider and company slug for a company,
// using api_provider when set and otherwise inferring from the api/careers URL.
// Returns ("", "") when no direct JSON API can be derived (e.g. websearch-only).
func resolveProvider(c portalCompany) (provider, slug string) {
	switch strings.ToLower(c.APIProvider) {
	case "greenhouse":
		if m := reGreenhouseSlug.FindStringSubmatch(c.API); m != nil {
			return "greenhouse", m[1]
		}
	case "lever":
		if m := reLeverSlug.FindStringSubmatch(c.API); m != nil {
			return "lever", m[1]
		}
	case "ashby":
		if c.APICompany != "" {
			return "ashby", c.APICompany
		}
		if m := reAshbySlug.FindStringSubmatch(c.CareersURL); m != nil {
			return "ashby", m[1]
		}
	}

	// Infer from api URL when provider unset.
	if m := reGreenhouseSlug.FindStringSubmatch(c.API); m != nil {
		return "greenhouse", m[1]
	}
	if m := reLeverSlug.FindStringSubmatch(c.API); m != nil {
		return "lever", m[1]
	}
	if c.APICompany != "" && strings.Contains(c.API, "ashbyhq.com") {
		return "ashby", c.APICompany
	}
	return "", ""
}

func fetchCompany(company, provider, slug string) ([]JobListing, error) {
	switch provider {
	case "greenhouse":
		return fetchGreenhouse(company, slug)
	case "lever":
		return fetchLever(company, slug)
	case "ashby":
		return fetchAshby(company, slug)
	}
	return nil, fmt.Errorf("unknown provider %q", provider)
}

func fetchGreenhouse(company, slug string) ([]JobListing, error) {
	url := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs", slug)
	var payload struct {
		Jobs []struct {
			AbsoluteURL string `json:"absolute_url"`
			Title       string `json:"title"`
			Location    struct {
				Name string `json:"name"`
			} `json:"location"`
		} `json:"jobs"`
	}
	if err := getJSON(url, &payload); err != nil {
		return nil, err
	}
	jobs := make([]JobListing, 0, len(payload.Jobs))
	for _, j := range payload.Jobs {
		jobs = append(jobs, JobListing{
			URL:      j.AbsoluteURL,
			Company:  company,
			Title:    j.Title,
			Portal:   "Greenhouse",
			Source:   "scan-history",
			Location: j.Location.Name,
		})
	}
	return jobs, nil
}

func fetchLever(company, slug string) ([]JobListing, error) {
	url := fmt.Sprintf("https://api.lever.co/v0/postings/%s?mode=json", slug)
	var postings []struct {
		Text             string `json:"text"`
		HostedURL        string `json:"hostedUrl"`
		ApplyURL         string `json:"applyUrl"`
		DescriptionPlain string `json:"descriptionPlain"`
		Categories       struct {
			Location string `json:"location"`
		} `json:"categories"`
		SalaryRange *struct {
			Min      float64 `json:"min"`
			Max      float64 `json:"max"`
			Currency string  `json:"currency"`
		} `json:"salaryRange"`
	}
	if err := getJSON(url, &postings); err != nil {
		return nil, err
	}
	jobs := make([]JobListing, 0, len(postings))
	for _, p := range postings {
		link := p.HostedURL
		if link == "" {
			link = p.ApplyURL
		}
		// Lever's list already carries the full description, so salary/YOE come
		// from it directly -- no per-job detail fetch needed.
		salary := ""
		if p.SalaryRange != nil && (p.SalaryRange.Min > 0 || p.SalaryRange.Max > 0) {
			salary = formatSalary(p.SalaryRange.Min, p.SalaryRange.Max, p.SalaryRange.Currency)
		}
		if salary == "" {
			salary = extractSalary(p.DescriptionPlain)
		}
		jobs = append(jobs, JobListing{
			URL:      link,
			Company:  company,
			Title:    p.Text,
			Salary:   salary,
			YOE:      extractYOE(p.DescriptionPlain),
			Portal:   "Lever",
			Source:   "scan-history",
			Location: p.Categories.Location,
		})
	}
	return jobs, nil
}

func fetchAshby(company, slug string) ([]JobListing, error) {
	const query = "query ApiJobBoardWithTeams($organizationHostedJobsPageName: String!) " +
		"{ jobBoard: jobBoardWithTeams(organizationHostedJobsPageName: $organizationHostedJobsPageName) " +
		"{ jobPostings { id title locationName employmentType compensationTierSummary } } }"
	body, _ := json.Marshal(map[string]interface{}{
		"operationName": "ApiJobBoardWithTeams",
		"variables":     map[string]string{"organizationHostedJobsPageName": slug},
		"query":         query,
	})

	req, err := http.NewRequest(http.MethodPost,
		"https://jobs.ashbyhq.com/api/non-user-graphql?op=ApiJobBoardWithTeams",
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := scanHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ashby http %d", resp.StatusCode)
	}

	var payload struct {
		Data struct {
			JobBoard struct {
				JobPostings []struct {
					ID                      string `json:"id"`
					Title                   string `json:"title"`
					LocationName            string `json:"locationName"`
					CompensationTierSummary string `json:"compensationTierSummary"`
				} `json:"jobPostings"`
			} `json:"jobBoard"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	jobs := make([]JobListing, 0, len(payload.Data.JobBoard.JobPostings))
	for _, p := range payload.Data.JobBoard.JobPostings {
		jobs = append(jobs, JobListing{
			URL:      fmt.Sprintf("https://jobs.ashbyhq.com/%s/%s", slug, p.ID),
			Company:  company,
			Title:    p.Title,
			Salary:   p.CompensationTierSummary,
			Portal:   "Ashby",
			Source:   "scan-history",
			Location: p.LocationName,
		})
	}
	return jobs, nil
}

func getJSON(url string, out interface{}) error {
	resp, err := scanHTTP.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func formatSalary(min, max float64, currency string) string {
	sym := "$"
	if currency != "" && currency != "USD" {
		sym = currency + " "
	}
	k := func(v float64) string { return fmt.Sprintf("%s%.0fK", sym, v/1000) }
	if min > 0 && max > 0 {
		return k(min) + " - " + k(max)
	}
	if max > 0 {
		return k(max)
	}
	return k(min)
}

// titlePasses reports whether a title has at least one positive keyword and no
// negative keyword (case-insensitive substring match), matching modes/scan.md.
func titlePasses(title string, positive, negative []string) bool {
	t := strings.ToLower(title)
	for _, n := range negative {
		if n != "" && strings.Contains(t, strings.ToLower(n)) {
			return false
		}
	}
	if len(positive) == 0 {
		return true
	}
	for _, p := range positive {
		if p != "" && strings.Contains(t, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// locationPasses applies the optional location_filter. Missing location data is
// not a reason to drop a role (per modes/scan.md).
func locationPasses(location string, remoteOnly bool, allowed []string) bool {
	loc := strings.ToLower(strings.TrimSpace(location))
	isRemote := strings.Contains(loc, "remote")

	if remoteOnly && !isRemote {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	if loc == "" || isRemote {
		return true
	}
	for _, a := range allowed {
		if a != "" && strings.Contains(loc, strings.ToLower(a)) {
			return true
		}
	}
	return false
}

// loadSeenURLs collects every URL already present in scan-history.tsv and
// pipeline.md so a scan never re-adds a known posting.
func loadSeenURLs(careerOpsPath string) map[string]bool {
	seen := make(map[string]bool)

	if b, err := os.ReadFile(filepath.Join(careerOpsPath, "data", "scan-history.tsv")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "url\t") || strings.TrimSpace(line) == "" {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) > 0 && strings.HasPrefix(fields[0], "http") {
				seen[strings.TrimSpace(fields[0])] = true
			}
		}
	}

	if b, err := os.ReadFile(filepath.Join(careerOpsPath, "data", "pipeline.md")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			m := rePipelineEntry.FindStringSubmatch(strings.TrimSpace(line))
			if m == nil {
				continue
			}
			parts := strings.Split(m[1], "|")
			if len(parts) > 0 {
				u := strings.TrimSpace(parts[0])
				if strings.HasPrefix(u, "http") {
					seen[u] = true
				}
			}
		}
	}

	return seen
}

// isEvaluated reports whether a job matches an already-evaluated application by
// normalized company + role, reusing the same matching used by the Listings view.
func isEvaluated(job JobListing, evaluated []model.CareerApplication) bool {
	jc := normalizeCompany(job.Company)
	if jc == "" {
		return false
	}
	jTitle := strings.ToLower(strings.TrimSpace(job.Title))
	jSet := roleWordSet(job.Title)
	for _, app := range evaluated {
		if normalizeCompany(app.Company) != jc {
			continue
		}
		if jTitle != "" && jTitle == strings.ToLower(strings.TrimSpace(app.Role)) {
			return true
		}
		if roleWordOverlap(jSet, roleWordSet(app.Role)) {
			return true
		}
	}
	return false
}

// appendToPipeline inserts new roles under the "## Pendientes" section of
// data/pipeline.md as `- [ ] {url} | {company} | {title}` lines.
func appendToPipeline(careerOpsPath string, listings []JobListing) error {
	if len(listings) == 0 {
		return nil
	}
	path := filepath.Join(careerOpsPath, "data", "pipeline.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading pipeline.md: %w", err)
	}
	lines := strings.Split(string(b), "\n")

	var entries []string
	for _, l := range listings {
		entries = append(entries, fmt.Sprintf("- [ ] %s | %s | %s", l.URL, l.Company, l.Title))
	}

	insertAt := -1
	for i, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), "## Pendientes") {
			insertAt = i + 1
			break
		}
	}
	if insertAt == -1 {
		// No Pendientes section: append one.
		lines = append(lines, "", "## Pendientes", "")
		lines = append(lines, entries...)
	} else {
		// Skip a single blank line immediately after the header for tidiness.
		if insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) == "" {
			insertAt++
		}
		merged := make([]string, 0, len(lines)+len(entries))
		merged = append(merged, lines[:insertAt]...)
		merged = append(merged, entries...)
		merged = append(merged, lines[insertAt:]...)
		lines = merged
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// appendToScanHistory appends the standard 6-column rows to data/scan-history.tsv
// (url, first_seen, portal, title, company, status). Enrichment (salary/YOE/fit)
// is kept separately in data/listing-details.tsv, so this file stays compatible
// with agent-written history.
func appendToScanHistory(careerOpsPath string, listings []JobListing, date string) error {
	if len(listings) == 0 {
		return nil
	}
	path := filepath.Join(careerOpsPath, "data", "scan-history.tsv")

	var buf bytes.Buffer
	if _, err := os.Stat(path); os.IsNotExist(err) {
		buf.WriteString("url\tfirst_seen\tportal\ttitle\tcompany\tstatus\n")
	}
	for _, l := range listings {
		fmt.Fprintf(&buf, "%s\t%s\t%s\t%s\t%s\t%s\n",
			l.URL, date, l.Portal, l.Title, l.Company, "added")
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening scan-history.tsv: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("writing scan-history.tsv: %w", err)
	}
	return nil
}

// loadListingDetails reads data/listing-details.tsv (url, salary, yoe, fit) into
// a URL-keyed map. Missing file yields an empty map.
func loadListingDetails(careerOpsPath string) map[string]listingDetail {
	out := make(map[string]listingDetail)
	b, err := os.ReadFile(filepath.Join(careerOpsPath, "data", "listing-details.tsv"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "url\t") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 4 || !strings.HasPrefix(f[0], "http") {
			continue
		}
		yoe, _ := strconv.Atoi(strings.TrimSpace(f[2]))
		fit, _ := strconv.Atoi(strings.TrimSpace(f[3]))
		out[strings.TrimSpace(f[0])] = listingDetail{
			Salary: strings.TrimSpace(f[1]),
			YOE:    yoe,
			Fit:    fit,
		}
	}
	return out
}

// saveListingDetails writes the full enrichment map back to disk.
func saveListingDetails(careerOpsPath string, details map[string]listingDetail) error {
	path := filepath.Join(careerOpsPath, "data", "listing-details.tsv")
	var buf bytes.Buffer
	buf.WriteString("url\tsalary\tyoe\tfit\n")
	for url, d := range details {
		yoe := ""
		if d.YOE > 0 {
			yoe = strconv.Itoa(d.YOE)
		}
		fit := ""
		if d.Fit > 0 {
			fit = strconv.Itoa(d.Fit)
		}
		fmt.Fprintf(&buf, "%s\t%s\t%s\t%s\n", url, d.Salary, yoe, fit)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// fetchLeverDetail pulls a single Lever posting and extracts salary + YOE from
// its description.
func fetchLeverDetail(jobURL string) (string, int) {
	m := regexp.MustCompile(`lever\.co/([^/]+)/([0-9a-f-]+)`).FindStringSubmatch(jobURL)
	if m == nil {
		return "", 0
	}
	api := fmt.Sprintf("https://api.lever.co/v0/postings/%s/%s", m[1], m[2])
	var p struct {
		DescriptionPlain string `json:"descriptionPlain"`
		SalaryRange      *struct {
			Min      float64 `json:"min"`
			Max      float64 `json:"max"`
			Currency string  `json:"currency"`
		} `json:"salaryRange"`
	}
	if err := getJSON(api, &p); err != nil {
		return "", 0
	}
	salary := ""
	if p.SalaryRange != nil && (p.SalaryRange.Min > 0 || p.SalaryRange.Max > 0) {
		salary = formatSalary(p.SalaryRange.Min, p.SalaryRange.Max, p.SalaryRange.Currency)
	}
	if salary == "" {
		salary = extractSalary(p.DescriptionPlain)
	}
	return salary, extractYOE(p.DescriptionPlain)
}

// --- Detail enrichment: salary + YOE ---

var (
	reHTMLTag  = regexp.MustCompile(`<[^>]+>`)
	reSalary   = regexp.MustCompile(`\$\s?([\d]{2,3}(?:,\d{3})+|\d{2,3}(?:\.\d+)?[kK])`)
	reYOEyears = regexp.MustCompile(`(\d{1,2})\s*\+?\s*years`)
	reGHDetail = regexp.MustCompile(`(job-boards\.(?:eu\.)?greenhouse\.io)/([^/]+)/jobs/(\d+)`)
)

// enrichDetails fills Salary and YOE for each listing that still lacks them by
// fetching the job's detail page, with bounded concurrency.
func enrichDetails(listings []JobListing) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range listings {
		if listings[i].Portal != "Greenhouse" {
			continue // Lever/Ashby already enriched from their list responses
		}
		if listings[i].Salary != "" && listings[i].YOE != 0 {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			salary, yoe := fetchGreenhouseDetail(listings[idx].URL)
			if listings[idx].Salary == "" {
				listings[idx].Salary = salary
			}
			if listings[idx].YOE == 0 {
				listings[idx].YOE = yoe
			}
		}(i)
	}
	wg.Wait()
}

// fetchGreenhouseDetail pulls a Greenhouse job's detail page and extracts a
// disclosed salary range and required years of experience from its content.
func fetchGreenhouseDetail(jobURL string) (salary string, yoe int) {
	m := reGHDetail.FindStringSubmatch(jobURL)
	if m == nil {
		return "", 0
	}
	host, slug, id := m[1], m[2], m[3]
	apiHost := "boards-api.greenhouse.io"
	if strings.Contains(host, "eu.greenhouse.io") {
		apiHost = "boards-api.eu.greenhouse.io"
	}
	api := fmt.Sprintf("https://%s/v1/boards/%s/jobs/%s", apiHost, slug, id)

	var detail struct {
		Content string `json:"content"`
	}
	if err := getJSON(api, &detail); err != nil {
		return "", 0
	}
	text := htmlToText(detail.Content)
	return extractSalary(text), extractYOE(text)
}

func htmlToText(s string) string {
	s = html.UnescapeString(s)
	s = reHTMLTag.ReplaceAllString(s, " ")
	return s
}

// extractSalary returns a normalized "$120K - $150K" from the first salary
// range in text, or "" when none is disclosed.
func extractSalary(text string) string {
	matches := reSalary.FindAllStringSubmatch(text, -1)
	var vals []int
	for _, m := range matches {
		if v := parseMoney(m[1]); v > 0 {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return ""
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	// Only treat as a comp range when the numbers look like salaries (>= $30k).
	if hi < 30000 {
		return ""
	}
	if lo == hi {
		return fmt.Sprintf("$%dK", hi/1000)
	}
	return fmt.Sprintf("$%dK - $%dK", lo/1000, hi/1000)
}

// parseMoney converts "150,000" or "150k" into an integer dollar amount.
func parseMoney(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	mult := 1
	if strings.HasSuffix(s, "k") {
		mult = 1000
		s = strings.TrimSuffix(s, "k")
	}
	s = strings.ReplaceAll(s, ",", "")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(f) * mult
}

// extractYOE returns the smallest plausible "N years" requirement in text.
func extractYOE(text string) int {
	matches := reYOEyears.FindAllStringSubmatch(text, -1)
	min := 0
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > 20 {
			continue
		}
		if min == 0 || n < min {
			min = n
		}
	}
	return min
}

// --- Fit scoring ---

// fitProfile holds the candidate signals a fit score is computed against.
type fitProfile struct {
	candidateYOE int
	targetMin    int // salary floor in dollars
	targetTop    int // salary target ceiling in dollars
}

// loadFitProfile reads comp targets from config/profile.yml (via ReadPreferences)
// and pairs them with the candidate's years of experience. YOE has no profile
// field yet, so it defaults to 3 (Walid's ~current level); ask to change it.
func loadFitProfile(careerOpsPath string) fitProfile {
	fp := fitProfile{candidateYOE: 3}
	prefs, err := ReadPreferences(careerOpsPath)
	if err == nil {
		lo, hi := parseCompRange(prefs.SalaryTarget)
		if minLo, _ := parseCompRange(prefs.SalaryMinimum); minLo > 0 {
			lo = minLo
		}
		fp.targetMin = lo
		fp.targetTop = hi
	}
	return fp
}

// parseCompRange parses "$130K-170K" or "$125K" into (min, max) dollars.
func parseCompRange(s string) (int, int) {
	nums := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*[kK]?`).FindAllStringSubmatch(s, -1)
	var vals []int
	kSuffix := strings.Contains(strings.ToLower(s), "k")
	for _, m := range nums {
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		v := int(f)
		if kSuffix && v < 1000 {
			v *= 1000
		}
		vals = append(vals, v)
	}
	if len(vals) == 0 {
		return 0, 0
	}
	if len(vals) == 1 {
		return vals[0], vals[0]
	}
	return vals[0], vals[len(vals)-1]
}

// computeFit is a transparent, deterministic 0-100 heuristic combining title
// relevance, YOE requirement vs the candidate, disclosed salary vs target, and
// location. It is intentionally simple -- deep fit is the evaluate/apply flow.
func computeFit(job JobListing, fp fitProfile, positives []string) int {
	score := 50
	title := strings.ToLower(job.Title)

	// Title relevance: reward positive-keyword hits.
	hits := 0
	for _, p := range positives {
		if p != "" && strings.Contains(title, strings.ToLower(p)) {
			hits++
		}
	}
	switch {
	case hits >= 3:
		score += 20
	case hits == 2:
		score += 14
	case hits == 1:
		score += 8
	default:
		score -= 10
	}
	// Seniority stretch: senior+ titles are a reach at ~3 YOE.
	for _, kw := range []string{"staff", "principal", "director", "head of", "vp "} {
		if strings.Contains(title, kw) {
			score -= 10
			break
		}
	}

	// YOE requirement vs candidate.
	if job.YOE > 0 {
		switch {
		case job.YOE <= fp.candidateYOE:
			score += 15
		case job.YOE <= fp.candidateYOE+2:
			score += 5
		default:
			score -= 15
		}
	}

	// Salary vs target.
	if smax := parseSalaryMax(job.Salary); smax > 0 && fp.targetMin > 0 {
		switch {
		case smax >= fp.targetTop:
			score += 15
		case smax >= fp.targetMin:
			score += 5
		default:
			score -= 10
		}
	}

	// Location.
	loc := strings.ToLower(job.Location)
	if strings.Contains(loc, "remote") || loc == "" {
		score += 5
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// parseSalaryMax returns the largest dollar figure in a salary string.
func parseSalaryMax(s string) int {
	matches := regexp.MustCompile(`\$?\s?(\d+(?:\.\d+)?)[kK]?`).FindAllStringSubmatch(s, -1)
	max := 0
	kSuffix := strings.Contains(strings.ToLower(s), "k")
	for _, m := range matches {
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		v := int(f)
		if kSuffix && v < 1000 {
			v *= 1000
		}
		if v > max {
			max = v
		}
	}
	return max
}
