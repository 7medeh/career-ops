# Mode: scan — Portal Scanner (Offer Discovery)

Scans the configured job portals, filters by title relevance, and adds new offers to the pipeline for later evaluation.

> **Note:** There is no `scan.mjs` script in this repo -- discovery is 100% agent-driven (run by Claude/Codex/etc.), using the 3 levels described below: direct Playwright, ATS APIs (Greenhouse/Ashby/Lever/...) via WebFetch, and WebSearch as a fallback. The Go dashboard (`dashboard/`) only reads what this flow has already written to `data/pipeline.md` / `data/scan-history.tsv` -- it does not scan anything itself.

## Recommended execution

Run it as a subagent so it doesn't consume the main context:

```
Agent(
    subagent_type="general-purpose",
    prompt="[contents of this file + specific data]",
    run_in_background=True
)
```

## Configuration

Read `portals.yml`, which contains:
- `search_queries`: list of WebSearch queries with `site:` filters per portal (broad discovery)
- `tracked_companies`: specific companies with a `careers_url` for direct navigation
- `title_filter`: positive/negative/seniority_boost keywords for title filtering
- `location_filter`: `remote_only` + `allowed_locations` for location filtering (optional -- if it isn't in `portals.yml`, skip this filter)

## Discovery strategy (3 levels)

### Level 1 — Direct Playwright (PRIMARY)

**For each company in `tracked_companies`:** navigate to its `careers_url` with Playwright (`browser_navigate` + `browser_snapshot`), read ALL visible job listings, and extract the title + URL of each. This is the most reliable method because:
- It sees the page in real time (not cached Google results)
- It works with SPAs (Ashby, Lever, Workday)
- It detects new offers instantly
- It doesn't depend on Google's indexing

**Every company MUST have a `careers_url` in portals.yml.** If it doesn't, look it up once, save it, and use it in future scans.

### Level 2 — ATS APIs / feeds (COMPLEMENTARY)

For companies with a public API or structured feed, use the JSON/XML response as a fast complement to Level 1. It's faster than Playwright and reduces visual scraping errors.

**Current support (variables between `{}`):**
- **Greenhouse**: `https://boards-api.greenhouse.io/v1/boards/{company}/jobs`
- **Ashby**: `https://jobs.ashbyhq.com/api/non-user-graphql?op=ApiJobBoardWithTeams`
- **BambooHR**: list `https://{company}.bamboohr.com/careers/list`; single-offer detail `https://{company}.bamboohr.com/careers/{id}/detail`
- **Lever**: `https://api.lever.co/v0/postings/{company}?mode=json`
- **Teamtailor**: `https://{company}.teamtailor.com/jobs.rss`
- **Workday**: `https://{company}.{shard}.myworkdayjobs.com/wday/cxs/{company}/{site}/jobs`

**Parsing convention by provider:**
- `greenhouse`: `jobs[]` → `title`, `absolute_url`
- `ashby`: GraphQL `ApiJobBoardWithTeams` with `organizationHostedJobsPageName={company}` → `jobBoard.jobPostings[]` (`title`, `id`; build the public URL if it isn't in the payload)
- `bamboohr`: list `result[]` → `jobOpeningName`, `id`; build the detail URL `https://{company}.bamboohr.com/careers/{id}/detail`; to read the full JD, GET the detail and use `result.jobOpening` (`jobOpeningName`, `description`, `datePosted`, `minimumExperience`, `compensation`, `jobOpeningShareUrl`)
- `lever`: root array `[]` → `text`, `hostedUrl` (fallback: `applyUrl`)
- `teamtailor`: RSS items → `title`, `link`
- `workday`: `jobPostings[]`/`jobPostings` (depending on tenant) → `title`, `externalPath` or a URL built from the host

### Level 3 — WebSearch queries (BROAD DISCOVERY)

The `search_queries` with `site:` filters cover portals horizontally (all of Ashby, all of Greenhouse, etc.). Useful for discovering NEW companies not yet in `tracked_companies`, but results may be stale.

**Execution priority:**
1. Level 1: Playwright → all `tracked_companies` with a `careers_url`
2. Level 2: API → all `tracked_companies` with `api:`
3. Level 3: WebSearch → all `search_queries` with `enabled: true`

The levels are additive — run all of them, then merge and deduplicate the results.

## Workflow

1. **Read the configuration**: `portals.yml`
2. **Read the history**: `data/scan-history.tsv` → URLs already seen
3. **Read the dedup sources**: `data/applications.md` + `data/pipeline.md`

4. **Level 1 — Playwright scan** (in parallel, batches of 3-5):
   For each company in `tracked_companies` with `enabled: true` and a defined `careers_url`:
   a. `browser_navigate` to the `careers_url`
   b. `browser_snapshot` to read all job listings
   c. If the page has filters/departments, navigate the relevant sections
   d. For each job listing extract: `{title, url, company}`
   e. If the page paginates results, navigate the additional pages
   f. Accumulate into the candidate list
   g. If `careers_url` fails (404, redirect), try `scan_query` as a fallback and note it so the URL can be updated

5. **Level 2 — ATS APIs / feeds** (in parallel):
   For each company in `tracked_companies` with `api:` defined and `enabled: true`:
   a. WebFetch the API/feed URL
   b. If `api_provider` is defined, use its parser; if not, infer it from the domain (`boards-api.greenhouse.io`, `jobs.ashbyhq.com`, `api.lever.co`, `*.bamboohr.com`, `*.teamtailor.com`, `*.myworkdayjobs.com`)
   c. For **Ashby**, send a POST with:
      - `operationName: ApiJobBoardWithTeams`
      - `variables.organizationHostedJobsPageName: {company}`
      - the GraphQL query for `jobBoardWithTeams` + `jobPostings { id title locationName employmentType compensationTierSummary }`
   d. For **BambooHR**, the list only returns basic metadata. For each relevant item, read `id`, GET `https://{company}.bamboohr.com/careers/{id}/detail`, and extract the full JD from `result.jobOpening`. Use `jobOpeningShareUrl` as the public URL if present; otherwise use the detail URL.
   e. For **Workday**, send a JSON POST with at least `{"appliedFacets":{},"limit":20,"offset":0,"searchText":""}` and paginate by `offset` until results are exhausted
   f. For each job, extract and normalize: `{title, url, company}`
   g. Accumulate into the candidate list (dedup against Level 1)

6. **Level 3 — WebSearch queries** (in parallel where possible):
   For each query in `search_queries` with `enabled: true`:
   a. Run WebSearch with the defined `query`
   b. From each result extract: `{title, url, company}`
      - **title**: from the result title (before the " @ " or " | ")
      - **url**: the result's URL
      - **company**: after the " @ " in the title, or extracted from the domain/path
   c. Accumulate into the candidate list (dedup against Levels 1+2)

6. **Filter by title** using `title_filter` from `portals.yml`:
   - At least 1 keyword from `positive` must appear in the title (case-insensitive)
   - 0 keywords from `negative` may appear
   - `seniority_boost` keywords grant priority but are not required

6.5. **Filter by location** using `location_filter` from `portals.yml` (if defined -- if the block is absent, skip this step without penalizing the offer):
   - If `remote_only: true` and the offer doesn't look remote (neither the title nor the visible location mentions "remote"), discard it
   - If `allowed_locations` has entries, the offer passes if its location text contains at least 1 entry (case-insensitive) OR the offer is remote
   - If no location signal is available at this stage (Level 2/API sometimes omits it), do not discard it for missing data alone -- let it through and let Block A of the later evaluation determine the real location

7. **Deduplicate** against 3 sources:
   - `scan-history.tsv` → exact URL already seen
   - `applications.md` → company + normalized role already evaluated
   - `pipeline.md` → exact URL already pending or processed

7.5. **Verify liveness of Level 3 (WebSearch) results** — BEFORE adding to the pipeline:

   WebSearch results can be stale (Google caches results for weeks or months). To avoid evaluating expired offers, verify every new Level 3 URL with Playwright. Levels 1 and 2 are inherently real-time and don't require this check.

   For each new Level 3 URL (sequentially — NEVER run Playwright in parallel):
   a. `browser_navigate` to the URL
   b. `browser_snapshot` to read the content
   c. Classify:
      - **Active**: job title visible + role description + a visible Apply/Submit control within the main content. Do not count generic header/navbar/footer text.
      - **Expired** (any of these signals):
        - The final URL contains `?error=true` (Greenhouse redirects this way when the offer is closed)
        - The page contains: "job no longer available" / "no longer open" / "position has been filled" / "this job has expired" / "page not found"
        - Only navbar and footer visible, with no JD content (content < ~300 chars)
   d. If expired: record it in `scan-history.tsv` with status `skipped_expired` and discard it
   e. If active: continue to step 8

   **Do not abort the entire scan if one URL fails.** If `browser_navigate` errors (timeout, 403, etc.), mark it `skipped_expired` and move on to the next one.

8. **For each new verified offer that passes the filters**:
   a. Add it to the "Pending" section of `pipeline.md`: `- [ ] {url} | {company} | {title}`
   b. Record it in `scan-history.tsv`: `{url}\t{date}\t{query_name}\t{title}\t{company}\tadded`

9. **Offers filtered out by title**: record in `scan-history.tsv` with status `skipped_title`
9.5. **Offers filtered out by location**: record in `scan-history.tsv` with status `skipped_location`
10. **Duplicate offers**: record with status `skipped_dup`
11. **Expired offers (Level 3)**: record with status `skipped_expired`

## Extracting title and company from WebSearch results

WebSearch results come in the format: `"Job Title @ Company"` or `"Job Title | Company"` or `"Job Title — Company"`.

Extraction patterns by portal:
- **Ashby**: `"Senior AI PM (Remote) @ EverAI"` → title: `Senior AI PM`, company: `EverAI`
- **Greenhouse**: `"AI Engineer at Anthropic"` → title: `AI Engineer`, company: `Anthropic`
- **Lever**: `"Product Manager - AI @ Temporal"` → title: `Product Manager - AI`, company: `Temporal`

Generic regex: `(.+?)(?:\s*[@|—–-]\s*|\s+at\s+)(.+?)$`

## Private URLs

If a URL isn't publicly accessible:
1. Save the JD to `jds/{company}-{role-slug}.md`
2. Add it to pipeline.md as: `- [ ] local:jds/{company}-{role-slug}.md | {company} | {title}`

## Scan History

`data/scan-history.tsv` tracks ALL URLs seen:

Format: `url	first_seen	portal	title	company	status`, with an optional 7th `salary` column (added by the dashboard's native scanner; 6-column rows have no salary).

```
url	first_seen	portal	title	company	status	salary
https://...	2026-02-10	Ashby — AI PM	PM AI	Acme	added	$120K - $150K
https://...	2026-02-10	Greenhouse — SA	Junior Dev	BigCo	skipped_title
https://...	2026-02-10	Ashby — AI PM	SA AI	OldCo	skipped_dup
https://...	2026-02-10	WebSearch — AI PM	PM AI	ClosedCo	skipped_expired
https://...	2026-02-10	Greenhouse — SA	AI Engineer	FarAwayCo	skipped_location
```

## Output summary

```
Portal Scan — {YYYY-MM-DD}
━━━━━━━━━━━━━━━━━━━━━━━━━━
Queries run: N
Offers found: N total
Passed the title filter: N relevant
Filtered by location: N (outside location_filter.allowed_locations)
Duplicates: N (already evaluated or in the pipeline)
Expired, discarded: N (dead links, Level 3)
New, added to pipeline.md: N

  + {company} | {title} | {query_name}
  ...

→ Run /career-ops pipeline to evaluate the new offers.
```

## Managing careers_url

Every company in `tracked_companies` should have a `careers_url` — the direct URL to its job listings page. This avoids looking it up every time.

**RULE: Always use the company's corporate URL; fall back to the ATS endpoint only if the company has no careers page of its own.**

The `careers_url` should point to the company's own careers page whenever one is available. Many companies use Workday, Greenhouse, or Lever underneath, but expose the job IDs only through their corporate domain. Using the direct ATS URL when a corporate page exists can cause false 410 errors, because the job IDs don't match.

| ✅ Correct (corporate) | ❌ Wrong as a first choice (direct ATS) |
|---|---|
| `https://careers.mastercard.com` | `https://mastercard.wd1.myworkdayjobs.com` |
| `https://openai.com/careers` | `https://job-boards.greenhouse.io/openai` |
| `https://stripe.com/jobs` | `https://jobs.lever.co/stripe` |

Fallback: if you only have the direct ATS URL, navigate to the company's website first and locate its corporate careers page. Use the direct ATS URL only if the company has no careers page of its own.

**Known patterns by platform:**
- **Ashby:** `https://jobs.ashbyhq.com/{slug}`
- **Greenhouse:** `https://job-boards.greenhouse.io/{slug}` or `https://job-boards.eu.greenhouse.io/{slug}`
- **Lever:** `https://jobs.lever.co/{slug}`
- **BambooHR:** list `https://{company}.bamboohr.com/careers/list`; detail `https://{company}.bamboohr.com/careers/{id}/detail`
- **Teamtailor:** `https://{company}.teamtailor.com/jobs`
- **Workday:** `https://{company}.{shard}.myworkdayjobs.com/{site}`
- **Custom:** the company's own URL (e.g. `https://openai.com/careers`)

**API/feed patterns by platform:**
- **Ashby API:** `https://jobs.ashbyhq.com/api/non-user-graphql?op=ApiJobBoardWithTeams`
- **BambooHR API:** list `https://{company}.bamboohr.com/careers/list`; detail `https://{company}.bamboohr.com/careers/{id}/detail` (`result.jobOpening`)
- **Lever API:** `https://api.lever.co/v0/postings/{company}?mode=json`
- **Teamtailor RSS:** `https://{company}.teamtailor.com/jobs.rss`
- **Workday API:** `https://{company}.{shard}.myworkdayjobs.com/wday/cxs/{company}/{site}/jobs`

**If a company has no `careers_url`:**
1. Try the pattern for its known platform
2. If that fails, run a quick WebSearch: `"{company}" careers jobs`
3. Navigate with Playwright to confirm it works
4. **Save the URL you found in portals.yml** for future scans

**If `careers_url` returns a 404 or redirect:**
1. Note it in the output summary
2. Try scan_query as a fallback
3. Flag it for manual updating

## Maintaining portals.yml

- **ALWAYS save `careers_url`** when adding a new company
- Add new queries as you discover interesting portals or roles
- Disable queries with `enabled: false` if they generate too much noise
- Adjust filter keywords as your target roles evolve
- Add companies to `tracked_companies` when you want to follow them closely
- Check `careers_url` periodically — companies change ATS platforms
