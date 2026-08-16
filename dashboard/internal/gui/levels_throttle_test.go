package gui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubLevels points the client at a local server for the duration of a test.
func stubLevels(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	origURL, origPolite := levelsBaseURL, levelsPolite
	levelsBaseURL = srv.URL
	levelsPolite = 0 // no need to pace a local server
	t.Cleanup(func() {
		levelsBaseURL, levelsPolite = origURL, origPolite
		srv.Close()
	})
}

func TestLevelsGetTreatsEmpty200AsThrottled(t *testing.T) {
	// Levels.fyi soft-throttles with 200 + empty body rather than 429.
	stubLevels(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_, st := levelsGet(http.DefaultClient, levelsBaseURL+"/companies/anything")
	if st != fetchThrottled {
		t.Errorf("empty 200 should be fetchThrottled, got %v", st)
	}
}

func TestLevelsGetStatusMapping(t *testing.T) {
	tests := []struct {
		name string
		code int
		body string
		want fetchStatus
	}{
		{"ok", 200, "<html>hi</html>", fetchOK},
		{"not found is a miss", 404, "nope", fetchMiss},
		{"too many requests is throttled", 429, "slow down", fetchThrottled},
		{"server error is throttled", 503, "unavailable", fetchThrottled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubLevels(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.code)
				_, _ = w.Write([]byte(tt.body))
			})
			_, st := levelsGet(http.DefaultClient, levelsBaseURL+"/companies/x")
			if st != tt.want {
				t.Errorf("status %d -> %v, want %v", tt.code, st, tt.want)
			}
		})
	}
}

func TestRefreshDoesNotCacheThrottledCompanies(t *testing.T) {
	dir := t.TempDir()

	// Every request throttles, so nothing should be written to the cache.
	stubLevels(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // empty body == throttled
	})

	res, err := RefreshCompanyProfiles(dir, []string{"Alpha", "Beta", "Gamma"})
	if err != nil {
		t.Fatalf("RefreshCompanyProfiles: %v", err)
	}
	if !res.Throttled {
		t.Error("expected Throttled to be reported")
	}
	if res.Resolved != 0 || res.Missed != 0 {
		t.Errorf("nothing should be resolved or missed, got %+v", res)
	}
	if res.Remaining != 3 {
		t.Errorf("Remaining = %d, want 3", res.Remaining)
	}

	// The critical property: a throttled run must leave the cache empty so a
	// later refresh retries, rather than recording 3 false misses.
	if got := LoadCompanyProfiles(dir); len(got) != 0 {
		t.Errorf("throttled run wrote %d cache entries, want 0", len(got))
	}
}

func TestRefreshCachesGenuineMisses(t *testing.T) {
	dir := t.TempDir()

	// 404 for everything: these are real misses and should be cached so they
	// are not retried on every refresh.
	stubLevels(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	res, err := RefreshCompanyProfiles(dir, []string{"Nonexistent Co"})
	if err != nil {
		t.Fatalf("RefreshCompanyProfiles: %v", err)
	}
	if res.Throttled {
		t.Error("404s should not be reported as throttling")
	}
	if res.Missed != 1 || res.Resolved != 0 {
		t.Errorf("expected 1 miss, got %+v", res)
	}
	cached := LoadCompanyProfiles(dir)
	if len(cached) != 1 {
		t.Fatalf("genuine miss should be cached, got %d entries", len(cached))
	}
	if p := cached["nonexistent co"]; p.Slug != "" || p.Stage != 0 {
		t.Errorf("miss should be cached as an empty profile, got %+v", p)
	}
}

func TestRefreshStopsAtFirstThrottle(t *testing.T) {
	dir := t.TempDir()

	// First company resolves, then the server starts throttling.
	var n int
	stubLevels(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			_, _ = w.Write([]byte(`<script id="__NEXT_DATA__" type="application/json">` +
				`{"props":{"pageProps":{"company":{"name":"Alpha","slug":"alpha",` +
				`"company_type":"private","funding_stage":"series_a","emp_count":40,` +
				`"hq_city":"Austin","hq_state_code":"TX"}}}}</script>`))
			return
		}
		w.WriteHeader(http.StatusOK) // throttled
	})

	res, err := RefreshCompanyProfiles(dir, []string{"Alpha", "Beta", "Gamma"})
	if err != nil {
		t.Fatalf("RefreshCompanyProfiles: %v", err)
	}
	if !res.Throttled {
		t.Error("expected Throttled")
	}
	if res.Resolved != 1 {
		t.Errorf("Resolved = %d, want 1", res.Resolved)
	}
	if res.Remaining != 2 {
		t.Errorf("Remaining = %d, want 2", res.Remaining)
	}

	// The one success must survive; the throttled two must not be cached.
	cached := LoadCompanyProfiles(dir)
	if len(cached) != 1 {
		t.Fatalf("expected only the resolved company cached, got %d", len(cached))
	}
	alpha := cached["alpha"]
	if alpha.Stage.String() != "Series A" {
		t.Errorf("alpha stage = %q, want Series A", alpha.Stage.String())
	}
	if alpha.HQ != "Austin, TX" {
		t.Errorf("alpha HQ = %q, want Austin, TX", alpha.HQ)
	}
}
