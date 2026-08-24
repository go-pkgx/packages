package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestNewestGaps is the cmake.org 4.4.2 shape: every 3.x carries linux/x86-64,
// the newest 4.x does not, and the interior-hole rule cannot see it because
// there is no later version to close the run.
func TestNewestGaps(t *testing.T) {
	rows := []row{
		{Name: "cmake.org", OS: "linux", Arch: "x86-64", Version: "3.31.12"},
		{Name: "cmake.org", OS: "linux", Arch: "aarch64", Version: "3.31.12"},
		{Name: "cmake.org", OS: "linux", Arch: "aarch64", Version: "4.4.2"},
		{Name: "cmake.org", OS: "darwin", Arch: "aarch64", Version: "4.4.2"},
		// A project whose newest version is complete contributes nothing.
		{Name: "lz4.org", OS: "linux", Arch: "x86-64", Version: "1.10.0"},
		{Name: "lz4.org", OS: "linux", Arch: "aarch64", Version: "1.10.0"},
	}
	got := newestGaps(rows)
	// darwin/aarch64 is missing from 3.31.12, but 3.31.12 is not the newest, so
	// only what 4.4.2 itself lacks is reported: linux/x86-64.
	if len(got) != 1 {
		t.Fatalf("got %d gaps, want 1: %+v", len(got), got)
	}
	if g := got[0]; g.project != "cmake.org" || g.version != "4.4.2" || g.platform != "linux/x86-64" {
		t.Fatalf("got %+v, want cmake.org 4.4.2 linux/x86-64", g)
	}
}

// TestNewestGapsSingleVersion: one version is its own newest and has every
// platform it has — no gap can exist.
func TestNewestGapsSingleVersion(t *testing.T) {
	rows := []row{{Name: "a.org", OS: "linux", Arch: "x86-64", Version: "1.0"}}
	if got := newestGaps(rows); len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
}

// TestRunAuditReportsTheNewestVersionBucket: the third bucket is printed and
// counted, and it does NOT fail the run — "upstream has it and we do not" is
// equally the signature of a version not yet built for that platform.
func TestRunAuditReportsTheNewestVersionBucket(t *testing.T) {
	files := map[string]string{
		"recipes.txt":               "foo\n",
		"windows/go-projects.txt":   "",
		"windows/rust-projects.txt": "",
	}
	both := `{"manifests":[
	 {"platform":{"os":"linux","architecture":"amd64"}},
	 {"platform":{"os":"linux","architecture":"arm64"}}]}`
	onlyArm := `{"manifests":[{"platform":{"os":"linux","architecture":"arm64"}}]}`

	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		case strings.Contains(u, "/token?"):
			return resp(200, `{"token":"t"}`, nil), nil
		case strings.Contains(u, "/packages/foo/tags/list"):
			return resp(200, `{"tags":["1.0","2.0"]}`, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/2.0"):
			return resp(200, onlyArm, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/"):
			return resp(200, both, nil), nil
		// Upstream carries 2.0 for x86-64, so the gap is real and worth listing.
		case strings.Contains(u, "dist.pkgx.dev/foo/linux/x86-64"):
			return resp(200, "1.0\n2.0\n", nil), nil
		}
		return resp(404, "nope", nil), nil
	}}
	env := map[string]string{"AUDIT": "1"}
	var out, errb strings.Builder
	if err := run(&out, &errb, func(k string) string { return env[k] }, d, fakeFiles(files)); err != nil {
		t.Fatalf("a newest-version gap must not fail the audit: %v", err)
	}
	if !strings.Contains(out.String(), "--- BEHIND") {
		t.Errorf("the bucket is not printed:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "foo 2.0: no linux/x86-64") {
		t.Errorf("the gap is not listed:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "1 newest-version gap(s)") {
		t.Errorf("the count is not reported: %q", errb.String())
	}
}

// TestRunAuditNewestGapUpstreamLacksItToo: upstream does not carry it either,
// so it is not even a worklist item and stays out of the bucket.
func TestRunAuditNewestGapUpstreamLacksItToo(t *testing.T) {
	files := map[string]string{
		"recipes.txt":               "foo\n",
		"windows/go-projects.txt":   "",
		"windows/rust-projects.txt": "",
	}
	both := `{"manifests":[
	 {"platform":{"os":"linux","architecture":"amd64"}},
	 {"platform":{"os":"linux","architecture":"arm64"}}]}`
	onlyArm := `{"manifests":[{"platform":{"os":"linux","architecture":"arm64"}}]}`
	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		case strings.Contains(u, "/token?"):
			return resp(200, `{"token":"t"}`, nil), nil
		case strings.Contains(u, "/packages/foo/tags/list"):
			return resp(200, `{"tags":["1.0","2.0"]}`, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/2.0"):
			return resp(200, onlyArm, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/"):
			return resp(200, both, nil), nil
		case strings.Contains(u, "dist.pkgx.dev/foo/linux/x86-64"):
			return resp(200, "1.0\n", nil), nil // no 2.0 upstream
		}
		return resp(404, "nope", nil), nil
	}}
	env := map[string]string{"AUDIT": "1"}
	var out, errb strings.Builder
	if err := run(&out, &errb, func(k string) string { return env[k] }, d, fakeFiles(files)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "foo 2.0: no linux/x86-64") {
		t.Errorf("a gap upstream does not carry must not be listed:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "0 newest-version gap(s)") {
		t.Errorf("count: %q", errb.String())
	}
}

var _ = io.Discard
