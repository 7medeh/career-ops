package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santifer/career-ops/dashboard/internal/model"
)

// newResumeRepo builds a throwaway career-ops tree. files maps repo-relative
// paths to contents; parent directories are created as needed.
func newResumeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func paths(docs []ResumeDoc) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.Path)
	}
	return out
}

func TestResolveResumesPrefersReportHeader(t *testing.T) {
	root := newResumeRepo(t, map[string]string{
		"reports/022-luma-ai-2026-08-17.md": "**Score:** 4.2/5\n" +
			"**PDF:** output/Walid-Hamade-Luma-AI-Forward-Deployed-Engineer.pdf\n",
		"output/Walid-Hamade-Luma-AI-Forward-Deployed-Engineer.pdf": "%PDF-1.4",
	})

	docs := ResolveResumes(root, model.CareerApplication{
		Company:    "Luma AI",
		ReportPath: "reports/022-luma-ai-2026-08-17.md",
	})

	if len(docs) != 1 {
		t.Fatalf("want 1 doc, got %d (%v)", len(docs), paths(docs))
	}
	if docs[0].Source != "report" {
		t.Errorf("want source %q, got %q", "report", docs[0].Source)
	}
	if docs[0].Kind != "CV" {
		t.Errorf("want kind CV, got %q", docs[0].Kind)
	}
	if docs[0].FileName != "Walid-Hamade-Luma-AI-Forward-Deployed-Engineer.pdf" {
		t.Errorf("unexpected filename %q", docs[0].FileName)
	}
}

// A cover letter is generated alongside the CV but never recorded in the
// report header, so only the filename glob can find it.
func TestResolveResumesFindsUnrecordedCoverLetter(t *testing.T) {
	root := newResumeRepo(t, map[string]string{
		"reports/021-stable-2026-08-16.md":                "**PDF:** output/Walid-Hamade-Stable-Product-Engineer.pdf\n",
		"output/Walid-Hamade-Stable-Product-Engineer.pdf": "%PDF-1.4",
		"output/Walid-Hamade-Stable-Cover-Letter.pdf":     "%PDF-1.4",
	})

	docs := ResolveResumes(root, model.CareerApplication{
		Company:    "Stable",
		ReportPath: "reports/021-stable-2026-08-16.md",
	})

	if len(docs) != 2 {
		t.Fatalf("want 2 docs, got %d (%v)", len(docs), paths(docs))
	}
	// CV sorts ahead of the cover letter: it is what "the resume" means.
	if docs[0].Kind != "CV" || docs[1].Kind != "Cover letter" {
		t.Errorf("want CV then Cover letter, got %q then %q", docs[0].Kind, docs[1].Kind)
	}
}

// Placeholder header lines are the common case for low-scoring roles and must
// never be mistaken for paths.
func TestResolveResumesIgnoresPlaceholderHeaders(t *testing.T) {
	for _, header := range []string{
		"**PDF:** ❌\n",
		"**PDF:** ❌ (not generated — score below threshold)\n",
		"**PDF:** pendiente\n",
		"**PDF:** Not generated — see recommendation below\n",
	} {
		root := newResumeRepo(t, map[string]string{
			"reports/003-acme-2026-01-01.md": header,
		})
		docs := ResolveResumes(root, model.CareerApplication{
			Company:    "Acme",
			ReportPath: "reports/003-acme-2026-01-01.md",
		})
		if len(docs) != 0 {
			t.Errorf("header %q: want no docs, got %v", strings.TrimSpace(header), paths(docs))
		}
	}
}

// A header may carry a trailing parenthetical note after the path.
func TestResolveResumesTrimsTrailingHeaderNote(t *testing.T) {
	root := newResumeRepo(t, map[string]string{
		"reports/018-plaid-2026-08-16.md": "**PDF:** output/Walid-Hamade-Plaid-FullStack.pdf " +
			"(regenerated 2026-08-16, reference B&W style)\n",
		"output/Walid-Hamade-Plaid-FullStack.pdf": "%PDF-1.4",
	})

	docs := ResolveResumes(root, model.CareerApplication{
		Company:    "Plaid",
		ReportPath: "reports/018-plaid-2026-08-16.md",
	})

	if len(docs) != 1 || docs[0].Path != "output/Walid-Hamade-Plaid-FullStack.pdf" {
		t.Fatalf("want the bare path, got %v", paths(docs))
	}
}

// A header pointing at a deleted file must not produce a doc the inspector
// would then fail to open.
func TestResolveResumesSkipsMissingFiles(t *testing.T) {
	root := newResumeRepo(t, map[string]string{
		"reports/009-gone-2026-01-01.md": "**PDF:** output/Walid-Hamade-Gone.pdf\n",
	})
	docs := ResolveResumes(root, model.CareerApplication{
		Company:    "Gone",
		ReportPath: "reports/009-gone-2026-01-01.md",
	})
	if len(docs) != 0 {
		t.Fatalf("want no docs for a deleted PDF, got %v", paths(docs))
	}
}

// Report headers are file-sourced text; a traversal path in one must not
// address anything outside the repo.
func TestResolveResumesRejectsTraversalInHeader(t *testing.T) {
	root := newResumeRepo(t, map[string]string{
		"reports/004-evil-2026-01-01.md": "**PDF:** ../../../etc/passwd.pdf\n",
	})
	docs := ResolveResumes(root, model.CareerApplication{
		Company:    "Evil",
		ReportPath: "reports/004-evil-2026-01-01.md",
	})
	if len(docs) != 0 {
		t.Fatalf("want traversal rejected, got %v", paths(docs))
	}
}

// A short company slug must require a whole "-slug-" segment, or it claims
// every generated PDF in output/.
func TestResolveResumesShortSlugNeedsFullSegment(t *testing.T) {
	root := newResumeRepo(t, map[string]string{
		"output/Walid-Hamade-Stable-Product-Engineer.pdf": "%PDF-1.4",
		"output/Walid-Hamade-X-Platform-Engineer.pdf":     "%PDF-1.4",
	})
	docs := ResolveResumes(root, model.CareerApplication{Company: "X"})
	if len(docs) != 1 || docs[0].FileName != "Walid-Hamade-X-Platform-Engineer.pdf" {
		t.Fatalf("short slug over-matched: %v", paths(docs))
	}
}

func TestResolveResumeFileRejectsUnsafePaths(t *testing.T) {
	root := newResumeRepo(t, map[string]string{
		"output/Walid-Hamade-Stable-Product-Engineer.pdf": "%PDF-1.4",
		"cv.md": "# Walid Hamade",
	})

	cases := map[string]string{
		"empty":          "",
		"traversal":      "output/../../etc/passwd.pdf",
		"absolute":       "/etc/passwd.pdf",
		"outside output": "cv.md",
		"not a pdf":      "output/notes.txt",
		"does not exist": "output/Walid-Hamade-Missing.pdf",
	}
	for name, rel := range cases {
		if _, err := ResolveResumeFile(root, rel); err == nil {
			t.Errorf("%s: want error for %q, got none", name, rel)
		}
	}

	if _, err := ResolveResumeFile(root, "output/Walid-Hamade-Stable-Product-Engineer.pdf"); err != nil {
		t.Errorf("valid path rejected: %v", err)
	}
}

func TestReadResumeBase64RoundTrips(t *testing.T) {
	root := newResumeRepo(t, map[string]string{
		"output/Walid-Hamade-Stable-Product-Engineer.pdf": "%PDF-1.4 body",
	})
	enc, err := ReadResumeBase64(root, "output/Walid-Hamade-Stable-Product-Engineer.pdf")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if enc == "" {
		t.Fatal("want base64 payload, got empty string")
	}
	if _, err := ReadResumeBase64(root, "output/../cv.md"); err == nil {
		t.Error("want traversal rejected")
	}
}

// A cancelled or interrupted save must not leave a truncated PDF at the
// destination the user picked.
func TestCopyResumeToWritesCompleteFile(t *testing.T) {
	const body = "%PDF-1.4 complete body"
	root := newResumeRepo(t, map[string]string{
		"output/Walid-Hamade-Stable-Product-Engineer.pdf": body,
	})
	dest := filepath.Join(t.TempDir(), "resume.pdf")

	if err := CopyResumeTo(root, "output/Walid-Hamade-Stable-Product-Engineer.pdf", dest); err != nil {
		t.Fatalf("copy: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != body {
		t.Errorf("want %q, got %q", body, string(got))
	}

	if err := CopyResumeTo(root, "output/Walid-Hamade-Stable-Product-Engineer.pdf", ""); err == nil {
		t.Error("want error on empty destination")
	}
}
