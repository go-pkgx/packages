package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSummarize(t *testing.T) {
	// Two platforms tie at 1 build so the comparator's name tie-break runs, and
	// one project (published) is absent from the recipes list while one recipe
	// (missing.org) is absent from the registry — the two directions of the
	// "how much is left" count.
	rows := []row{
		{Name: "a.org", OS: "linux", Arch: "aarch64", Version: "1.0"},
		{Name: "a.org", OS: "linux", Arch: "aarch64", Version: "1.1"},
		{Name: "a.org", OS: "darwin", Arch: "aarch64", Version: "1.0"},
		{Name: "b.org", OS: "windows", Arch: "x86-64", Version: "2.0"},
	}
	var out strings.Builder
	summarize(&out, rows, []string{"a.org", "missing.org"})

	want := "2 projects, 4 platform builds\n" +
		"  linux/aarch64      2\n" +
		"  darwin/aarch64     1\n" +
		"  windows/x86-64     1\n" +
		"recipes.txt: 1 of 2 published, 1 remaining\n"
	if got := out.String(); got != want {
		t.Fatalf("summary mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	// Nothing published: no platform lines, and every recipe still remaining.
	var out strings.Builder
	summarize(&out, nil, []string{"a.org"})
	want := "0 projects, 0 platform builds\nrecipes.txt: 0 of 1 published, 1 remaining\n"
	if got := out.String(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRunSummaryMode(t *testing.T) {
	// SUMMARY=1 reports counts instead of the JSON catalogue.
	files := map[string]string{
		"recipes.txt":               "foo\nbar\n",
		"windows/go-projects.txt":   "",
		"windows/rust-projects.txt": "",
	}
	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		case strings.Contains(u, "/token?") && strings.Contains(u, "packages/foo:pull"):
			return resp(200, `{"token":"t"}`, nil), nil
		case strings.Contains(u, "/token?"):
			return resp(403, `{"errors":[{"code":"DENIED"}]}`, nil), nil
		case strings.Contains(u, "/packages/foo/tags/list"):
			return resp(200, `{"tags":["1.0"]}`, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/1.0"):
			return resp(200, `{"manifests":[{"platform":{"os":"linux","architecture":"arm64"}}]}`, nil), nil
		}
		return resp(404, `{}`, nil), nil
	}}
	env := map[string]string{"SUMMARY": "1"}
	var out strings.Builder
	if err := run(&out, io.Discard, func(k string) string { return env[k] }, d, fakeFiles(files)); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "1 projects, 1 platform builds\n  linux/aarch64      1\nrecipes.txt: 1 of 2 published, 1 remaining\n"
	if got := out.String(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRunSummaryRecipesUnreadable(t *testing.T) {
	// The crawl succeeds from a cached candidate list, then the summary's own
	// re-read of recipes.txt fails: run must surface that, not print a count
	// computed from nothing.
	calls := 0
	rf := func(path string) ([]byte, error) {
		if path == "recipes.txt" {
			calls++
			if calls > 1 {
				return nil, io.ErrUnexpectedEOF
			}
			return []byte("foo\n"), nil
		}
		return []byte(""), nil
	}
	d := route()
	env := map[string]string{"SUMMARY": "1"}
	if err := run(io.Discard, io.Discard, func(k string) string { return env[k] }, d, rf); err == nil {
		t.Fatal("expected the second recipes.txt read to fail the run")
	}
}

// TestRunAuditUnrequestableUpstreamURL closes the audit's last error branch: the
// upstream lookup builds a URL from data the registry gave us, so a platform
// string the registry reports can be one http.NewRequest refuses. The audit must
// treat that as "upstream does not confirm it" — an ABSENCE, which heals nothing
// and fails nothing — rather than acting on a nil response.
func TestRunAuditUnrequestableUpstreamURL(t *testing.T) {
	files := map[string]string{
		"recipes.txt":               "foo\n",
		"windows/go-projects.txt":   "",
		"windows/rust-projects.txt": "",
	}
	// A DEL byte in the architecture the index reports (JSON \u007f). It survives
	// mapArch, which passes unknown values through, so it reaches the upstream
	// versions.txt URL and url.Parse rejects it as an invalid control character.
	both := `{"manifests":[
	 {"platform":{"os":"linux","architecture":"amd64"}},
	 {"platform":{"os":"linux","architecture":"a\u007fb"}}]}`
	one := `{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}`

	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		case strings.Contains(u, "/token?"):
			return resp(200, `{"token":"t"}`, nil), nil
		case strings.Contains(u, "/packages/foo/tags/list"):
			return resp(200, `{"tags":["1.0","1.1","1.2"]}`, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/1.1"):
			return resp(200, one, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/"):
			return resp(200, both, nil), nil
		}
		return resp(404, "nope", nil), nil
	}}
	env := map[string]string{"AUDIT": "1"}
	var out, errb strings.Builder
	// No LOST entry, so the audit passes: a gap upstream cannot confirm is not a
	// defect anyone can act on.
	if err := run(&out, &errb, func(k string) string { return env[k] }, d, fakeFiles(files)); err != nil {
		t.Fatalf("an unconfirmable gap must not fail the audit: %v", err)
	}
	if !strings.Contains(errb.String(), "0 lost index entr") {
		t.Errorf("want no lost entries, got %q", errb.String())
	}
	if !strings.Contains(out.String(), "foo 1.1: no linux/a") {
		t.Errorf("the gap is not reported as an absence:\n%q", out.String())
	}
}
