package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestWithTagsTheListingMissed is the mesonbuild.com shape, measured
// 2026-08-26: /tags/list returned 40 tags topping out at 1.11.2 while 1.12.0,
// mirrored an hour earlier, answered its manifest with all four platforms. The
// factory knew ("SKIP mesonbuild.com 1.12.0 — already published"); only this
// crawl did not, and 78 darwin recipes were counted as blocked on a project
// that was published.
func TestWithTagsTheListingMissed(t *testing.T) {
	defer restoreUpstreamURL(upstreamVersionsURL)
	upstreamVersionsURL = func(p, osn, arch string) string {
		return "https://up.test/" + p + "/" + osn + "/" + arch + "/versions.txt"
	}
	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		case strings.HasPrefix(u, "https://up.test/") && strings.Contains(u, "/linux/"):
			return resp(200, "1.11.2\n1.12.0\n", nil), nil
		case strings.HasPrefix(u, "https://up.test/"):
			return resp(404, "", nil), nil
		case strings.Contains(u, "/manifests/1.12.0"):
			return resp(200, `{"manifests":[{"platform":{"os":"darwin","architecture":"arm64"}}]}`, nil), nil
		}
		return resp(404, "", nil), nil
	}}
	var notes strings.Builder
	c := testClient(d)
	c.stderr = &notes

	got := c.withTagsTheListingMissed("mesonbuild.com", "t", []string{"1.11.2"})
	if strings.Join(got, " ") != "1.11.2 1.12.0" {
		t.Fatalf("got %v, want the listing plus the version it omitted", got)
	}
	// The omission is reported: a crawl that silently repairs itself teaches
	// nobody that the listing cannot be trusted.
	if !strings.Contains(notes.String(), "omitted 1 published version") {
		t.Errorf("the omission was not reported: %q", notes.String())
	}
}

func TestWithTagsTheListingMissedNothingToAdd(t *testing.T) {
	defer restoreUpstreamURL(upstreamVersionsURL)
	upstreamVersionsURL = func(p, osn, arch string) string { return "https://up.test/v" }
	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.String(), "https://up.test/") {
			// Everything upstream has, the listing already had; plus one entry
			// that is not a version at all, and one upstream carries but ghcr
			// does not.
			return resp(200, "1.0\nlatest\n9.9\n", nil), nil
		}
		return resp(404, "", nil), nil // 9.9's manifest: not there
	}}
	var notes strings.Builder
	c := testClient(d)
	c.stderr = &notes
	got := c.withTagsTheListingMissed("x.org", "t", []string{"1.0"})
	if strings.Join(got, " ") != "1.0" {
		t.Fatalf("got %v, want the listing unchanged", got)
	}
	if notes.String() != "" {
		t.Errorf("nothing was added, so nothing should be reported: %q", notes.String())
	}
}

// TestUpstreamVersionsIsOnlyACrossCheck: a dist that will not answer must not
// fail the crawl — the listing is still the primary source.
func TestUpstreamVersionsIsOnlyACrossCheck(t *testing.T) {
	defer restoreUpstreamURL(upstreamVersionsURL)
	upstreamVersionsURL = func(p, osn, arch string) string { return "https://up.test/v" }
	for _, tc := range []struct {
		name string
		fn   func(*http.Request) (*http.Response, error)
	}{
		{"transport error", func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF }},
		{"500", func(*http.Request) (*http.Response, error) { return resp(500, "boom", nil), nil }},
		{"empty", func(*http.Request) (*http.Response, error) { return resp(200, "\n  \n", nil), nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(mockDoer{fn: tc.fn})
			if got := c.upstreamVersions("x.org", "linux", "x86-64"); got != nil {
				t.Fatalf("got %v, want nothing", got)
			}
		})
	}
}

func restoreUpstreamURL(f func(string, string, string) string) { upstreamVersionsURL = f }

// errBody fails partway through the read, which no real dist does on purpose
// and every flaky network does by accident.
type errBody struct{ n int }

func (e *errBody) Read(p []byte) (int, error) {
	if e.n > 0 {
		e.n--
		p[0] = 'x'
		return 1, nil
	}
	return 0, io.ErrUnexpectedEOF
}
func (e *errBody) Close() error { return nil }

// TestUpstreamVersionsBodyFailsMidRead: a cross-check that cannot finish
// reading contributes nothing, and still must not fail the crawl.
func TestUpstreamVersionsBodyFailsMidRead(t *testing.T) {
	defer restoreUpstreamURL(upstreamVersionsURL)
	upstreamVersionsURL = func(p, osn, arch string) string { return "https://up.test/v" }
	c := testClient(mockDoer{fn: func(*http.Request) (*http.Response, error) {
		r := resp(200, "", nil)
		r.Body = &errBody{n: 3}
		return r, nil
	}})
	if got := c.upstreamVersions("x.org", "linux", "x86-64"); got != nil {
		t.Fatalf("got %v, want nothing", got)
	}
}
