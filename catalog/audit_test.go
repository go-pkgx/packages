package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestAuditFindsASplitIndex is the shape of the real defect: one version
// carrying fewer platforms than its siblings. python.org 3.14.7 lost
// linux/arm64 to a racing publisher and every arm64 install then failed with
// "platform not in index".
func TestAuditFindsASplitIndex(t *testing.T) {
	rows := []row{
		{"python.org", "linux", "x86-64", "3.14.6"},
		{"python.org", "linux", "aarch64", "3.14.6"},
		{"python.org", "linux", "x86-64", "3.14.5"},
		{"python.org", "linux", "aarch64", "3.14.5"},
		{"python.org", "linux", "x86-64", "3.14.7"}, // aarch64 lost
	}
	var out bytes.Buffer

	if n := auditSplitIndexes(rows, &out); n != 1 {
		t.Fatalf("found %d project(s), want 1:\n%s", n, out.String())
	}
	got := out.String()
	for _, want := range []string{"python.org", "3.14.7", "linux/aarch64"} {
		if !strings.Contains(got, want) {
			t.Errorf("report omits %q:\n%s", want, got)
		}
	}
	// The widest set is printed first: a split index is a DEPARTURE from it.
	wide := strings.Index(got, "linux/aarch64,linux/x86-64")
	narrow := strings.Index(got, "[linux/x86-64]")
	if wide < 0 || narrow < 0 || wide > narrow {
		t.Errorf("the widest platform set must come first:\n%s", got)
	}
}

// TestAuditIgnoresAConsistentProject: every version agreeing is the normal case
// and must be silent, or the report is noise nobody reads.
func TestAuditIgnoresAConsistentProject(t *testing.T) {
	rows := []row{
		{"lz4.org", "linux", "x86-64", "1.10.0"},
		{"lz4.org", "linux", "aarch64", "1.10.0"},
		{"lz4.org", "linux", "x86-64", "1.9.4"},
		{"lz4.org", "linux", "aarch64", "1.9.4"},
	}
	var out bytes.Buffer

	if n := auditSplitIndexes(rows, &out); n != 0 {
		t.Fatalf("false positive:\n%s", out.String())
	}
}

// TestAuditReportsRepeatedSplits is why this reports disagreement rather than
// guessing a healthy set: when a race hits the same project twice, the majority
// is the BROKEN set, and any rule that trusted the majority would stay silent
// exactly when the damage is worst.
func TestAuditReportsRepeatedSplits(t *testing.T) {
	rows := []row{
		{"a.org", "linux", "x86-64", "1.0.0"},
		{"a.org", "linux", "aarch64", "1.0.0"},
		{"a.org", "linux", "x86-64", "2.0.0"}, // split
		{"a.org", "linux", "x86-64", "3.0.0"}, // split
	}
	var out bytes.Buffer

	if n := auditSplitIndexes(rows, &out); n != 1 {
		t.Fatalf("found %d, want the project reported:\n%s", n, out.String())
	}
	got := out.String()
	for _, v := range []string{"2.0.0", "3.0.0"} {
		if !strings.Contains(got, v) {
			t.Errorf("%s not listed:\n%s", v, got)
		}
	}
}

// TestAuditReportsTheDarwinCase: gnu.org/gperf 3.3 kept only its darwin
// platforms, the linux pair having been clobbered by the darwin workflow. The
// sets do not overlap at all, which must still read as one project in
// disagreement rather than two healthy ones.
func TestAuditReportsTheDarwinCase(t *testing.T) {
	rows := []row{
		{"gnu.org/gperf", "linux", "x86-64", "3.2"},
		{"gnu.org/gperf", "linux", "aarch64", "3.2"},
		{"gnu.org/gperf", "darwin", "aarch64", "3.2"},
		{"gnu.org/gperf", "darwin", "aarch64", "3.3"},
		{"gnu.org/gperf", "darwin", "x86-64", "3.3"},
	}
	var out bytes.Buffer

	if n := auditSplitIndexes(rows, &out); n != 1 {
		t.Fatalf("found %d, want 1:\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), "3.3") {
		t.Errorf("the affected version is not named:\n%s", out.String())
	}
}

// TestAuditIsSilentOnASingleVersion: one version cannot disagree with itself.
func TestAuditIsSilentOnASingleVersion(t *testing.T) {
	var out bytes.Buffer
	rows := []row{{"solo.org", "linux", "x86-64", "1.0.0"}}

	if n := auditSplitIndexes(rows, &out); n != 0 {
		t.Fatalf("found %d:\n%s", n, out.String())
	}
}

// TestAuditPlatformGapsFindsAHole is the high-confidence signal: a platform
// present before AND after the affected version. Nothing about building a
// package produces that shape; losing an index write does.
func TestAuditPlatformGapsFindsAHole(t *testing.T) {
	rows := []row{
		{"kernel.org/linux-headers", "linux", "x86-64", "7.1.2"},
		{"kernel.org/linux-headers", "linux", "aarch64", "7.1.2"},
		{"kernel.org/linux-headers", "linux", "aarch64", "7.1.3"}, // x86-64 lost
		{"kernel.org/linux-headers", "linux", "x86-64", "7.1.4"},
		{"kernel.org/linux-headers", "linux", "aarch64", "7.1.4"},
	}
	var out bytes.Buffer

	if n := auditPlatformGaps(rows, &out); n != 1 {
		t.Fatalf("found %d, want 1:\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), "7.1.3") || !strings.Contains(out.String(), "linux/x86-64") {
		t.Errorf("report does not name the hole:\n%s", out.String())
	}
}

// TestAuditPlatformGapsIgnoresGrowth: a project that GAINS a platform leaves
// every earlier version legitimately without it — a step, not a hole. This is
// the difference that takes the report from 420 projects to 137 versions.
func TestAuditPlatformGapsIgnoresGrowth(t *testing.T) {
	rows := []row{
		{"alacritty.org", "darwin", "x86-64", "0.10.0"},
		{"alacritty.org", "darwin", "x86-64", "0.12.0"},
		{"alacritty.org", "linux", "x86-64", "0.12.0"},
		{"alacritty.org", "darwin", "x86-64", "0.15.1"},
		{"alacritty.org", "linux", "x86-64", "0.15.1"},
		{"alacritty.org", "linux", "aarch64", "0.15.1"},
	}
	var out bytes.Buffer

	if n := auditPlatformGaps(rows, &out); n != 0 {
		t.Fatalf("growth reported as a gap:\n%s", out.String())
	}
}

// TestLessVersionIsNumeric: 1.10.0 comes AFTER 1.9.0. Ordering versions as text
// would put 1.10.0 between 1.1 and 1.2 and invent holes that are not there.
func TestLessVersionIsNumeric(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"1.9.0", "1.10.0", true},
		{"1.10.0", "1.9.0", false},
		{"2.0", "10.0", true},
		{"1.2", "1.2.1", true},
		{"0.94l", "0.95", true},   // non-numeric component falls back to text
		{"1.0.0", "1.0.0", false}, // equal is not less
	} {
		if got := lessVersion(tc.a, tc.b); got != tc.want {
			t.Errorf("lessVersion(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestAuditPlatformGapsIsSilentOnTheEdges: a platform missing only at the very
// start or the very end of a project's history is growth or a build not yet
// done, not a hole.
func TestAuditPlatformGapsIsSilentOnTheEdges(t *testing.T) {
	rows := []row{
		{"x.org", "linux", "x86-64", "1.0.0"},
		{"x.org", "linux", "x86-64", "2.0.0"},
		{"x.org", "linux", "aarch64", "2.0.0"},
		{"x.org", "linux", "x86-64", "3.0.0"}, // newest lacks aarch64: not yet built
	}
	var out bytes.Buffer

	if n := auditPlatformGaps(rows, &out); n != 0 {
		t.Fatalf("an edge was reported as a gap:\n%s", out.String())
	}
}

// TestAuditPlatformGapsReportsEveryHole: one race can split several versions of
// the same project, and a report that stops at the first is a sweep that
// samples.
func TestAuditPlatformGapsReportsEveryHole(t *testing.T) {
	rows := []row{
		{"z.org", "linux", "x86-64", "1.0.0"},
		{"z.org", "linux", "aarch64", "1.0.0"},
		{"z.org", "linux", "aarch64", "1.2.0"},  // hole
		{"z.org", "linux", "aarch64", "1.10.0"}, // hole, and sorts AFTER 1.2.0
		{"z.org", "linux", "x86-64", "2.0.0"},
		{"z.org", "linux", "aarch64", "2.0.0"},
	}
	var out bytes.Buffer

	if n := auditPlatformGaps(rows, &out); n != 2 {
		t.Fatalf("found %d, want 2:\n%s", n, out.String())
	}
	// Numeric order: 1.2.0 before 1.10.0.
	got := out.String()
	if strings.Index(got, "1.2.0") > strings.Index(got, "1.10.0") {
		t.Errorf("versions are not in numeric order:\n%s", got)
	}
}

// TestAuditSplitIndexesOrdersEqualWidthSetsStably keeps the report
// deterministic when two platform sets are the same size — otherwise the output
// changes between runs and a diff of two audits is unreadable.
func TestAuditSplitIndexesOrdersEqualWidthSetsStably(t *testing.T) {
	rows := []row{
		{"e.org", "linux", "x86-64", "1.0.0"},
		{"e.org", "darwin", "x86-64", "2.0.0"},
	}
	var first bytes.Buffer
	auditSplitIndexes(rows, &first)
	for i := 0; i < 3; i++ {
		var again bytes.Buffer
		auditSplitIndexes(rows, &again)
		if again.String() != first.String() {
			t.Fatalf("unstable output:\n%s\nvs\n%s", first.String(), again.String())
		}
	}
	if !strings.Contains(first.String(), "darwin/x86-64") {
		t.Errorf("both sets must be listed:\n%s", first.String())
	}
}
