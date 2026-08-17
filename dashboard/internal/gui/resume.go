package gui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/santifer/career-ops/dashboard/internal/model"
)

// ResumeDoc is one generated PDF belonging to an application -- a tailored CV
// or its companion cover letter.
//
// Paths are always repo-relative and forward-slashed, matching how
// data/pdf-index.tsv and the report headers record them. AbsPath is resolved
// once here so the frontend never has to join paths itself; every binding that
// accepts a path re-validates it anyway (see ResolveResumeFile).
type ResumeDoc struct {
	Kind      string // "CV" or "Cover letter"
	Path      string // repo-relative, forward slashes
	AbsPath   string
	FileName  string
	SizeBytes int64
	Modified  string // YYYY-MM-DD
	Source    string // how it was found: "report", "manifest", or "filename"
}

// rePDFHeader captures the path on a report's `**PDF:**` header line.
//
// The line is free text in practice: it carries a real path, a ❌, a "pendiente",
// or a path followed by a parenthetical note. Anchoring on a non-space run that
// ends in .pdf rejects the placeholder forms without needing to enumerate them,
// and stops at the first token so a trailing note is dropped.
var rePDFHeader = regexp.MustCompile(`(?m)^\*\*PDF:\*\*\s+(\S+\.pdf)`)

// ResolveResumes returns every generated PDF that belongs to app, best match
// first, deduplicated by path.
//
// Three sources are consulted in descending order of confidence:
//
//  1. The report's own `**PDF:**` header. This is an explicit statement of
//     linkage written when the PDF was generated, so it outranks any inference.
//  2. data/pdf-index.tsv rows whose filename carries the company slug. The
//     manifest records a report number only when generate-pdf.mjs was given
//     --report, which in practice it usually is not, so the rows are matched by
//     name rather than keyed by report.
//  3. A company-slug match over output/*.pdf. This is the only source that
//     finds companion files nobody recorded anywhere -- cover letters in
//     particular are generated alongside a CV and never make it into a header.
//
// Upstream's data.ResolvePDFs is deliberately not used: it globs output/cv-*.pdf
// only, which predates the Walid-Hamade-{Company}-{Role}.pdf naming this repo
// now generates, and it returns a single best path rather than the full set the
// inspector wants to offer.
func ResolveResumes(repoPath string, app model.CareerApplication) []ResumeDoc {
	var docs []ResumeDoc
	seen := make(map[string]bool)

	add := func(rel, source string) {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" || seen[rel] || !isSafeRepoRelative(rel) {
			return
		}
		abs := filepath.Join(repoPath, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return
		}
		seen[rel] = true
		docs = append(docs, ResumeDoc{
			Kind:      classifyDoc(rel),
			Path:      rel,
			AbsPath:   abs,
			FileName:  filepath.Base(rel),
			SizeBytes: info.Size(),
			Modified:  info.ModTime().Format("2006-01-02"),
			Source:    source,
		})
	}

	// 1. The report header.
	if app.ReportPath != "" {
		reportAbs := filepath.Join(repoPath, filepath.FromSlash(app.ReportPath))
		if raw, err := os.ReadFile(reportAbs); err == nil {
			for _, m := range rePDFHeader.FindAllStringSubmatch(string(raw), -1) {
				add(m[1], "report")
			}
		}
	}

	slug := slugify(app.Company)
	if slug == "" {
		sortResumeDocs(docs)
		return docs
	}

	// 2. Manifest rows, matched by filename rather than report number.
	for path := range loadManifestPaths(repoPath) {
		if matchesSlug(strings.ToLower(filepath.Base(path)), slug) {
			add(path, "manifest")
		}
	}

	// 3. Anything else in output/ carrying the company slug.
	globbed, err := filepath.Glob(filepath.Join(repoPath, "output", "*.pdf"))
	if err == nil {
		for _, abs := range globbed {
			if !matchesSlug(strings.ToLower(filepath.Base(abs)), slug) {
				continue
			}
			if rel, err := filepath.Rel(repoPath, abs); err == nil {
				add(filepath.ToSlash(rel), "filename")
			}
		}
	}

	sortResumeDocs(docs)
	return docs
}

// sortResumeDocs puts CVs ahead of cover letters, then newest first. The
// inspector opens on docs[0], and the CV is what the user means by "the
// resume" even when a cover letter was generated more recently.
func sortResumeDocs(docs []ResumeDoc) {
	sort.SliceStable(docs, func(i, j int) bool {
		if (docs[i].Kind == "CV") != (docs[j].Kind == "CV") {
			return docs[i].Kind == "CV"
		}
		return docs[i].Modified > docs[j].Modified
	})
}

// loadManifestPaths returns the set of PDF paths recorded in
// data/pdf-index.tsv. A missing file is not an error -- the manifest only
// exists after the first generate-pdf.mjs run.
//
// Only the PDF column is read; the manifest's other columns describe how the
// file was produced, which the inspector does not care about.
func loadManifestPaths(repoPath string) map[string]bool {
	paths := make(map[string]bool)
	raw, err := os.ReadFile(filepath.Join(repoPath, "data", "pdf-index.tsv"))
	if err != nil {
		return paths
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		if p := strings.TrimSpace(fields[1]); p != "" && isSafeRepoRelative(p) {
			paths[filepath.ToSlash(p)] = true
		}
	}
	return paths
}

// classifyDoc labels a generated PDF from its filename. Cover letters are the
// only companion document the generator currently produces, so anything that
// does not announce itself as one is treated as the CV.
func classifyDoc(path string) string {
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(base, "cover") {
		return "Cover letter"
	}
	return "CV"
}

// Company names are slugified with the package's existing slugify (levels.go),
// which lowercases and collapses every non-alphanumeric run into one hyphen:
// "Luma AI" -> "luma-ai". That mirrors the kebab-casing the PDF generator
// applies to company names, which is what makes filename matching work.

// matchesSlug reports whether a PDF filename refers to the company.
//
// Slugs shorter than three runes must appear as a whole "-slug-" segment, so a
// company like "X" cannot claim every file in output/. Longer slugs match on
// containment, which is what lets one company's slug find both
// Walid-Hamade-Stable-Product-Engineer.pdf and its cover letter.
func matchesSlug(base, slug string) bool {
	if len([]rune(slug)) < 3 {
		return strings.Contains(base, "-"+slug+"-")
	}
	return strings.Contains(base, slug)
}

// isSafeRepoRelative reports whether p is a non-empty relative path with no
// ".." traversal. Manifest rows and report headers are both file-sourced text,
// so a crafted entry must not be able to address anything outside the repo.
func isSafeRepoRelative(p string) bool {
	p = filepath.Clean(filepath.FromSlash(strings.TrimSpace(p)))
	if p == "" || p == "." {
		return false
	}
	if filepath.IsAbs(p) {
		return false
	}
	return p != ".." && !strings.HasPrefix(p, ".."+string(filepath.Separator))
}

// ResolveResumeFile validates a repo-relative PDF path coming back from the
// frontend and returns its absolute location.
//
// The frontend only ever sends paths this package handed it, but the binding is
// reachable regardless of what the UI does, so the check is repeated here
// rather than trusted from the round trip: relative, no traversal, inside
// output/, and a .pdf that actually exists.
func ResolveResumeFile(repoPath, rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" {
		return "", fmt.Errorf("no document selected")
	}
	if !isSafeRepoRelative(rel) {
		return "", fmt.Errorf("refusing to read %q: not a repo-relative path", rel)
	}
	if !strings.HasPrefix(rel, "output/") {
		return "", fmt.Errorf("refusing to read %q: generated documents live in output/", rel)
	}
	if !strings.EqualFold(filepath.Ext(rel), ".pdf") {
		return "", fmt.Errorf("refusing to read %q: not a PDF", rel)
	}
	abs := filepath.Join(repoPath, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("that document is no longer on disk: %s", rel)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", rel)
	}
	return abs, nil
}

// maxInlinePDFBytes caps what ReadResumeBase64 will return.
//
// The bytes cross the Wails bridge as a JSON string and are held in the
// webview as a base64 string plus a decoded blob, so a runaway file costs
// roughly three copies in memory. Generated CVs run well under a megabyte;
// 32 MB is far above anything this pipeline produces and still bounded.
const maxInlinePDFBytes = 32 << 20

// ReadResumeBase64 returns a generated PDF encoded for inline display in the
// webview. base64 rather than raw bytes because the Wails v2 bridge is JSON,
// which has no byte-array representation.
func ReadResumeBase64(repoPath, rel string) (string, error) {
	abs, err := ResolveResumeFile(repoPath, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.Size() > maxInlinePDFBytes {
		return "", fmt.Errorf("%s is %d MB, too large to preview inline -- open it externally instead",
			filepath.Base(rel), info.Size()>>20)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// CopyResumeTo copies a generated PDF to dest, which is an absolute path the
// user chose in a native save dialog.
//
// The copy is written to a temporary file in the destination directory and then
// renamed, so an interrupted write cannot leave a truncated PDF sitting at the
// path the user picked -- they would have no way to tell it apart from a good
// one until something failed to open it.
func CopyResumeTo(repoPath, rel, dest string) error {
	abs, err := ResolveResumeFile(repoPath, rel)
	if err != nil {
		return err
	}
	if strings.TrimSpace(dest) == "" {
		return fmt.Errorf("no destination chosen")
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".career-ops-save-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// RevealResume opens the platform file manager with the PDF selected.
//
// This is also the supported way to drag the file out of the dashboard: Wails
// v2 exposes OnFileDrop for files dropped *into* the window but has no
// drag-source API, and the macOS WKWebView backing the app ignores the
// DataTransfer "DownloadURL" convention that makes HTML5 drag-to-desktop work
// in a normal browser. Handing the user the file already selected in Finder
// puts them one drag away without pretending the webview can do it.
func RevealResume(repoPath, rel string) error {
	abs, err := ResolveResumeFile(repoPath, rel)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", abs).Run()
	case "windows":
		// explorer exits non-zero even when it succeeds, so its status is
		// deliberately not checked.
		_ = exec.Command("explorer", "/select,"+abs).Run()
		return nil
	case "linux":
		// No portable "select this file" verb exists across Linux file
		// managers, so the containing directory is opened instead.
		return exec.Command("xdg-open", filepath.Dir(abs)).Run()
	default:
		return fmt.Errorf("revealing files is not supported on %s", runtime.GOOS)
	}
}
