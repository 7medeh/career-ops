package gui

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/santifer/career-ops/dashboard/internal/data"
	"github.com/santifer/career-ops/dashboard/internal/model"
)

var rePipelineEntry = regexp.MustCompile(`^-\s*\[ \]\s*(.+)$`)

// ParseListings reads data/pipeline.md (Pendientes section) and
// data/scan-history.tsv (status=added rows), merges and deduplicates them,
// and excludes anything already evaluated in applications.md.
func ParseListings(careerOpsPath string) []JobListing {
	fromPipeline := parsePipelineListings(careerOpsPath)
	fromScanHistory := parseScanHistoryListings(careerOpsPath)

	// Merge by URL. pipeline.md is the source of truth for what's pending, but
	// scan-history.tsv carries richer metadata (salary, portal, first_seen), so
	// when the same URL appears in both, fill any field the pipeline entry lacks.
	index := make(map[string]int)
	var merged []JobListing

	for _, l := range fromPipeline {
		if _, ok := index[l.URL]; ok {
			continue
		}
		index[l.URL] = len(merged)
		merged = append(merged, l)
	}
	for _, l := range fromScanHistory {
		if i, ok := index[l.URL]; ok {
			enrichListing(&merged[i], l)
			continue
		}
		index[l.URL] = len(merged)
		merged = append(merged, l)
	}

	// Overlay enrichment (salary/YOE/fit) from the separate details store.
	details := loadListingDetails(careerOpsPath)
	if len(details) > 0 {
		for i := range merged {
			if d, ok := details[merged[i].URL]; ok {
				if merged[i].Salary == "" {
					merged[i].Salary = d.Salary
				}
				if merged[i].YOE == 0 {
					merged[i].YOE = d.YOE
				}
				if merged[i].Fit == 0 {
					merged[i].Fit = d.Fit
				}
			}
		}
	}

	// Overlay company context (stage, headcount, HQ, median comp) from the
	// Levels.fyi-backed profile cache.
	applyCompanyProfiles(merged, LoadCompanyProfiles(careerOpsPath))

	evaluated := data.ParseApplications(careerOpsPath)
	return excludeEvaluated(merged, evaluated)
}

// applyCompanyProfiles overlays cached company context onto listings, filling
// only fields the listing lacks. Salary from Levels.fyi is a company-wide median
// rather than the posting's range, so it is flagged as an estimate.
func applyCompanyProfiles(listings []JobListing, profiles map[string]CompanyProfile) {
	if len(profiles) == 0 {
		return
	}
	for i := range listings {
		p, ok := profiles[strings.ToLower(strings.TrimSpace(listings[i].Company))]
		if !ok {
			continue
		}
		listings[i].Stage = p.Stage
		listings[i].StageInferred = p.StageInferred
		listings[i].Headcount = p.Headcount
		listings[i].HeadcountRange = p.HeadcountRange
		if listings[i].Location == "" {
			listings[i].Location = p.HQ
		}
		if listings[i].Salary == "" && p.MedianComp != "" {
			listings[i].Salary = p.MedianComp
			listings[i].SalaryEstimated = true
		}
	}
}

// ListingCompanies returns the distinct company names across listings, in first
// -seen order. Used to drive the company-profile refresh.
func ListingCompanies(listings []JobListing) []string {
	seen := make(map[string]bool, len(listings))
	var out []string
	for _, l := range listings {
		name := strings.TrimSpace(l.Company)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// enrichListing fills empty fields of dst from src (used to layer scan-history
// metadata onto a pipeline.md entry with the same URL).
func enrichListing(dst *JobListing, src JobListing) {
	if dst.Salary == "" {
		dst.Salary = src.Salary
	}
	if dst.Portal == "" {
		dst.Portal = src.Portal
	}
	if dst.FirstSeen == "" {
		dst.FirstSeen = src.FirstSeen
	}
	if dst.Location == "" {
		dst.Location = src.Location
	}
	if dst.YOE == 0 {
		dst.YOE = src.YOE
	}
	if dst.Fit == 0 {
		dst.Fit = src.Fit
	}
	if dst.Company == "" {
		dst.Company = src.Company
	}
	if dst.Title == "" {
		dst.Title = src.Title
	}
}

// parsePipelineListings parses the "## Pendientes" section of data/pipeline.md.
// Entries look like: "- [ ] {url} | {company} | {title}"
func parsePipelineListings(careerOpsPath string) []JobListing {
	path := filepath.Join(careerOpsPath, "data", "pipeline.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var listings []JobListing
	inPendientes := false

	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			inPendientes = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(trimmed, "##")), "Pendientes")
			continue
		}
		if !inPendientes {
			continue
		}

		m := rePipelineEntry.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}

		parts := strings.Split(m[1], "|")
		listing := JobListing{Source: "pipeline"}
		if len(parts) > 0 {
			listing.URL = strings.TrimSpace(parts[0])
		}
		if len(parts) > 1 {
			listing.Company = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			listing.Title = strings.TrimSpace(parts[2])
		}
		if listing.URL == "" {
			continue
		}
		listings = append(listings, listing)
	}

	return listings
}

// parseScanHistoryListings parses data/scan-history.tsv rows with status=added.
// Schema: url\tfirst_seen\tportal\ttitle\tcompany\tstatus
func parseScanHistoryListings(careerOpsPath string) []JobListing {
	path := filepath.Join(careerOpsPath, "data", "scan-history.tsv")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var listings []JobListing
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "url\t") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 6 {
			continue
		}
		if fields[5] != "added" {
			continue
		}
		// salary/yoe/fit are optional columns 7-9 added by the native scanner;
		// older 6-column rows simply lack them.
		salary := ""
		if len(fields) >= 7 {
			salary = strings.TrimSpace(fields[6])
		}
		yoe := 0
		if len(fields) >= 8 {
			yoe, _ = strconv.Atoi(strings.TrimSpace(fields[7]))
		}
		fit := 0
		if len(fields) >= 9 {
			fit, _ = strconv.Atoi(strings.TrimSpace(fields[8]))
		}
		listings = append(listings, JobListing{
			URL:       strings.TrimSpace(fields[0]),
			FirstSeen: strings.TrimSpace(fields[1]),
			Portal:    strings.TrimSpace(fields[2]),
			Title:     strings.TrimSpace(fields[3]),
			Company:   strings.TrimSpace(fields[4]),
			Salary:    salary,
			YOE:       yoe,
			Fit:       fit,
			Source:    "scan-history",
		})
	}

	return listings
}

// excludeEvaluated drops listings that match an already-evaluated application,
// either by exact URL or by normalized company + overlapping role words.
func excludeEvaluated(listings []JobListing, evaluated []model.CareerApplication) []JobListing {
	if len(evaluated) == 0 {
		return listings
	}

	type evalKey struct {
		company   string
		titleNorm string
		roleSet   map[string]bool
	}
	var keys []evalKey
	evaluatedURLs := make(map[string]bool)
	for _, app := range evaluated {
		if app.JobURL != "" {
			evaluatedURLs[app.JobURL] = true
		}
		keys = append(keys, evalKey{
			company:   normalizeCompany(app.Company),
			titleNorm: strings.ToLower(strings.TrimSpace(app.Role)),
			roleSet:   roleWordSet(app.Role),
		})
	}

	var result []JobListing
	for _, l := range listings {
		if evaluatedURLs[l.URL] {
			continue
		}
		lCompany := normalizeCompany(l.Company)
		lTitleNorm := strings.ToLower(strings.TrimSpace(l.Title))
		lRoleSet := roleWordSet(l.Title)
		duplicate := false
		for _, k := range keys {
			if k.company != lCompany || k.company == "" {
				continue
			}
			if lTitleNorm != "" && lTitleNorm == k.titleNorm {
				duplicate = true
				break
			}
			if roleWordOverlap(lRoleSet, k.roleSet) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, l)
		}
	}
	return result
}

// genericRoleWords are tokens too common to signal that two roles are the same.
// Without excluding these, "Software Engineer, Desktop" would falsely match
// "Software Engineer, Full-stack" on the shared "software engineer" (the same
// class of false positive that skipped report #016 in merge-tracker.mjs).
var genericRoleWords = map[string]bool{
	"software": true, "engineer": true, "engineering": true, "developer": true,
	"senior": true, "staff": true, "principal": true, "lead": true, "junior": true,
	"associate": true, "manager": true, "specialist": true, "analyst": true,
	"consultant": true, "remote": true, "hybrid": true, "onsite": true,
}

// roleWordSet extracts significant (len > 3, non-generic) lowercase words from a
// role title, used only as a secondary dedup signal after exact title match.
func roleWordSet(role string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(role)) {
		w = strings.Trim(w, ",.()-")
		if len(w) > 3 && !genericRoleWords[w] {
			set[w] = true
		}
	}
	return set
}

// roleWordOverlap reports whether two role word sets share enough words to be
// considered the same role. Used as a secondary check after exact title match
// fails, for cases like "Software Engineer, Full-stack" vs "Software Engineer,
// Full Stack". Requires overlap >= 2 so two-word titles like "AI Engineer"
// can't false-match against an unrelated "AI Product Manager" on "AI" alone;
// short-title duplicates are instead caught by the exact-match check above.
func roleWordOverlap(a, b map[string]bool) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	overlap := 0
	for w := range a {
		if b[w] {
			overlap++
		}
	}
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	return overlap >= 2 && float64(overlap)/float64(minLen) >= 0.6
}
