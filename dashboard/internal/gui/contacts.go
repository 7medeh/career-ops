package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Contacts live in data/contacts.tsv and are maintained by the candidate.
// Nothing here discovers or guesses addresses -- the file only holds what the
// candidate has already found and chosen to record.
const contactsFile = "contacts.tsv"

const contactsHeader = "company\tname\ttitle\temail\tsource\n"

// contactsTemplate seeds a new file with the header and a commented example so
// the expected shape is obvious on first open.
const contactsTemplate = contactsHeader +
	"# One row per person, tab-separated. Fill these in as you find them.\n" +
	"# Example:\n" +
	"# Attio\tJane Doe\tVP Engineering\tjane@attio.com\tcompany about page\n"

// LoadContacts reads data/contacts.tsv keyed by lowercased company name. A
// company with several contacts keeps them in file order. A missing file yields
// an empty map rather than an error -- it is optional.
func LoadContacts(careerOpsPath string) map[string][]Contact {
	out := make(map[string][]Contact)
	b, err := os.ReadFile(filepath.Join(careerOpsPath, "data", contactsFile))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(line, "company\t") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 4 {
			continue
		}
		company := strings.TrimSpace(f[0])
		email := strings.TrimSpace(f[3])
		if company == "" || email == "" {
			continue
		}
		c := Contact{
			Company: company,
			Name:    strings.TrimSpace(f[1]),
			Title:   strings.TrimSpace(f[2]),
			Email:   email,
		}
		if len(f) >= 5 {
			c.Source = strings.TrimSpace(f[4])
		}
		key := strings.ToLower(company)
		out[key] = append(out[key], c)
	}
	return out
}

// ContactsFor returns the recorded contacts for a company, nil when there are none.
func ContactsFor(contacts map[string][]Contact, company string) []Contact {
	return contacts[strings.ToLower(strings.TrimSpace(company))]
}

// EnsureContactsFile creates data/contacts.tsv with its header and example rows
// if it does not exist yet. Existing files are left untouched.
func EnsureContactsFile(careerOpsPath string) (string, error) {
	dir := filepath.Join(careerOpsPath, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating data dir: %w", err)
	}
	path := filepath.Join(dir, contactsFile)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(contactsTemplate), 0o644); err != nil {
		return "", fmt.Errorf("creating contacts.tsv: %w", err)
	}
	return path, nil
}
