package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func upstreamFor(body map[string]string) func(string) (*http.Response, error) {
	return func(url string) (*http.Response, error) {
		for frag, b := range body {
			if strings.Contains(url, frag) {
				return resp(200, b, nil), nil
			}
		}
		return resp(404, "nope", nil), nil
	}
}

// TestClassifyGapsSeparatesLostFromAbsent is the correction this exists for.
// gnu.org/glibc 2.28.0 is published for linux/aarch64 and has NEVER existed for
// linux/x86-64 — not upstream, not anywhere. Its "gap" looked exactly like a
// lost index write, and no re-dispatch could ever fill it: two mirrors and a
// glibc rebuild were spent before the version lists said so.
func TestClassifyGapsSeparatesLostFromAbsent(t *testing.T) {
	u := newUpstreamIndex(upstreamFor(map[string]string{
		"gnu.org/glibc/linux/aarch64": "2.27.0\n2.28.0\n",
		"gnu.org/glibc/linux/x86-64":  "2.24.0\n2.27.0\n", // no 2.28.0, ever
		"python.org/linux/aarch64":    "3.14.6\n3.14.7\n", // upstream HAS it
	}))

	lost, absent := classifyGaps([]gap{
		{project: "gnu.org/glibc", version: "2.28.0", platform: "linux/x86-64"},
		{project: "python.org", version: "3.14.7", platform: "linux/aarch64"},
	}, u)

	if len(lost) != 1 || lost[0].project != "python.org" {
		t.Errorf("lost = %+v, want only python.org", lost)
	}
	if len(absent) != 1 || absent[0].project != "gnu.org/glibc" {
		t.Errorf("absent = %+v, want only gnu.org/glibc", absent)
	}
}

// TestUpstreamIndexFetchesEachPlatformOnce: the audit asks about hundreds of
// gaps and a versions.txt per platform answers all of a project's.
func TestUpstreamIndexFetchesEachPlatformOnce(t *testing.T) {
	calls := 0
	u := newUpstreamIndex(func(url string) (*http.Response, error) {
		calls++
		return resp(200, "1.0\n2.0\n", nil), nil
	})

	for i := 0; i < 5; i++ {
		if !u.has("x.org", "linux", "x86-64", "1.0") {
			t.Fatal("should be present")
		}
	}
	if calls != 1 {
		t.Errorf("fetched %d times, want 1", calls)
	}
	if u.has("x.org", "linux", "aarch64", "1.0"); calls != 2 {
		t.Errorf("a second platform must be its own fetch (%d)", calls)
	}
}

// TestUpstreamIndexTreatsFailureAsAbsent... deliberately: a network hiccup must
// not reclassify a real defect as "nothing to do". Absent is the conservative
// bucket — it says "we could not confirm upstream has it", and the operator
// re-runs rather than acting on a false all-clear.
func TestUpstreamIndexTreatsFailureAsAbsent(t *testing.T) {
	u := newUpstreamIndex(func(string) (*http.Response, error) { return nil, errors.New("dns") })
	if u.has("x.org", "linux", "x86-64", "1.0") {
		t.Error("a failed fetch must not claim upstream has the version")
	}

	u404 := newUpstreamIndex(func(string) (*http.Response, error) { return resp(404, "no", nil), nil })
	if u404.has("x.org", "linux", "x86-64", "1.0") {
		t.Error("a 404 must not claim upstream has the version")
	}
}

// TestUpstreamIndexIgnoresBlankLines keeps a trailing newline from becoming a
// version.
func TestUpstreamIndexIgnoresBlankLines(t *testing.T) {
	u := newUpstreamIndex(upstreamFor(map[string]string{"x.org": "1.0\n\n  \n2.0\n"}))
	if !u.has("x.org", "linux", "x86-64", "1.0") || !u.has("x.org", "linux", "x86-64", "2.0") {
		t.Error("real versions missing")
	}
	if u.has("x.org", "linux", "x86-64", "") {
		t.Error("a blank line became a version")
	}
}

// TestCollectPlatformGapsMatchesTheReport: the classified path and the printed
// one must see the same gaps, or the two halves of the audit disagree.
func TestCollectPlatformGapsMatchesTheReport(t *testing.T) {
	rows := []row{
		{"a.org", "linux", "x86-64", "1.0.0"},
		{"a.org", "linux", "aarch64", "1.0.0"},
		{"a.org", "linux", "aarch64", "1.2.0"}, // gap
		{"a.org", "linux", "x86-64", "2.0.0"},
		{"a.org", "linux", "aarch64", "2.0.0"},
	}
	var printed strings.Builder

	n := auditPlatformGaps(rows, &printed)
	got := collectPlatformGaps(rows)

	if n != len(got) {
		t.Fatalf("printed %d gaps, collected %d", n, len(got))
	}
	if len(got) != 1 || got[0].version != "1.2.0" || got[0].platform != "linux/x86-64" {
		t.Errorf("collected %+v", got)
	}
}

// TestSortGapsIsStableAndOrdered: the report is read as a work list and diffed
// between audits, so its order must not wander — and 1.10.0 must come after
// 1.9.0.
func TestSortGapsIsStableAndOrdered(t *testing.T) {
	in := []gap{
		{project: "b.org", version: "1.0.0", platform: "linux/x86-64"},
		{project: "a.org", version: "1.10.0", platform: "linux/x86-64"},
		{project: "a.org", version: "1.9.0", platform: "linux/x86-64"},
		{project: "a.org", version: "1.9.0", platform: "linux/aarch64"},
	}
	sortGaps(in)

	want := []string{
		"a.org 1.9.0 linux/aarch64",
		"a.org 1.9.0 linux/x86-64",
		"a.org 1.10.0 linux/x86-64",
		"b.org 1.0.0 linux/x86-64",
	}
	for i, g := range in {
		if got := g.project + " " + g.version + " " + g.platform; got != want[i] {
			t.Errorf("position %d = %q, want %q", i, got, want[i])
		}
	}
}
