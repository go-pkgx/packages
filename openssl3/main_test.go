package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRecipe drops a package.yml under <pantry>/projects/<proj>/.
func writeRecipe(t *testing.T, pantry, proj, yaml string) {
	t.Helper()
	dir := filepath.Join(pantry, "projects", filepath.FromSlash(proj))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.yml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDepLine pins exactly what the rewrite claims to touch, and what it must
// leave alone. Every form below occurs in the pantry today.
func TestDepLine(t *testing.T) {
	for _, in := range []string{
		"  openssl.org: ^1.1",
		"    openssl.org: ^1.1",
		"  openssl.org: '^1.1'",
		`  openssl.org: "^1.1"`,
		"  openssl.org: ^1.1.1",
		"  openssl.org: ^1.1.1k",
		"  openssl.org: ^1.1 # as of 0.6.0",
		"  openssl.org: ^1",
	} {
		if !depLine.MatchString(in) {
			t.Errorf("must match: %q", in)
		}
	}
	for _, in := range []string{
		"  openssl.org: ^3",        // already correct
		"  openssl.org: '*'",       // unconstrained
		"  openssl.org: >=1.1",     // a different operator: not ours to guess at
		"openssl.org: ^1.1",        // top level, not a dependency entry
		"  libressl.org: ^1.1",     // another project
		"  # openssl.org: ^1.1",    // commented out
		"  openssl.org: ^1.1 junk", // unparsed trailer
	} {
		if depLine.MatchString(in) {
			t.Errorf("must NOT match: %q", in)
		}
	}
}

// TestRewritePreservesTheLine: only the constraint changes — indentation,
// quoting and any trailing comment survive, because these patches are meant to
// be proposed upstream and a gratuitous reformat is a reason to reject one.
func TestRewritePreservesTheLine(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: '^1.1' # keep me\n  zlib.net: ^1\n")
	patch, err := patchFor(p, "a.org")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "-  openssl.org: '^1.1' # keep me\n+  openssl.org: '^3' # keep me\n") {
		t.Errorf("line not preserved:\n%s", patch)
	}
	if !strings.Contains(patch, "--- a/projects/a.org/package.yml\n+++ b/projects/a.org/package.yml\n") {
		t.Errorf("paths must be relative to the pantry root:\n%s", patch)
	}
	// zlib's own ^1 is a different project and must be untouched context.
	if strings.Contains(patch, "-  zlib.net") {
		t.Errorf("touched an unrelated dependency:\n%s", patch)
	}
}

// A recipe may pin openssl twice (crates.io/sccache pins it in both its runtime
// and its build deps): both are rewritten, in as many hunks as needed.
func TestTwoPins(t *testing.T) {
	p := t.TempDir()
	var y strings.Builder
	y.WriteString("dependencies:\n  openssl.org: ^1.1\n")
	for i := 0; i < 10; i++ {
		y.WriteString("  filler" + string(rune('a'+i)) + ": '*'\n")
	}
	y.WriteString("build:\n  dependencies:\n    openssl.org: ^1.1\n")
	writeRecipe(t, p, "b.org", y.String())
	patch, err := patchFor(p, "b.org")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(patch, "+  openssl.org: ^3") + strings.Count(patch, "+    openssl.org: ^3"); n != 2 {
		t.Errorf("rewrote %d of 2 pins:\n%s", n, patch)
	}
	if n := strings.Count(patch, "@@ "); n != 2 {
		t.Errorf("want two hunks (the pins are far apart), got %d:\n%s", n, patch)
	}
}

// Adjacent pins must land in ONE hunk: a patch that lists the same context line
// twice is malformed and every applier rejects it.
func TestAdjacentPinsMergeIntoOneHunk(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "c.org", "dependencies:\n  openssl.org: ^1.1\n  zlib.net: '*'\n  openssl.org: ^1.1\n")
	patch, err := patchFor(p, "c.org")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(patch, "@@ "); n != 1 {
		t.Errorf("want one merged hunk, got %d:\n%s", n, patch)
	}
	if n := strings.Count(patch, "+  openssl.org: ^3"); n != 2 {
		t.Errorf("rewrote %d of 2 pins:\n%s", n, patch)
	}
}

func TestRanges(t *testing.T) {
	// far apart -> two spans; touching -> merged; clamped at both ends
	got := ranges([]int{0, 20}, 30)
	if len(got) != 2 || got[0] != (hunkRange{0, 4}) || got[1] != (hunkRange{17, 24}) {
		t.Errorf("ranges = %v", got)
	}
	if got = ranges([]int{5, 8}, 30); len(got) != 1 || got[0] != (hunkRange{2, 12}) {
		t.Errorf("merge = %v", got)
	}
	if got = ranges([]int{1}, 3); len(got) != 1 || got[0] != (hunkRange{0, 3}) {
		t.Errorf("clamp = %v", got)
	}
}

func TestPinnedAndErrors(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: ^1.1\n")
	writeRecipe(t, p, "nested/b.org", "dependencies:\n  openssl.org: ^1\n")
	writeRecipe(t, p, "clean.org", "dependencies:\n  openssl.org: ^3\n")

	got, err := pinned(filepath.Join(p, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("pinned = %v, want the two stale ones", got)
	}
	// a project with no stale pin is not patchable
	if _, err := patchFor(p, "clean.org"); err == nil {
		t.Error("expected an error for a recipe with no stale pin")
	}
	// nor is one that does not exist
	if _, err := patchFor(p, "absent.org"); err == nil {
		t.Error("expected an error for a missing recipe")
	}
	// an unreadable projects dir is reported, not ignored
	if _, err := pinned(filepath.Join(p, "nope")); err == nil {
		t.Error("expected an error for a missing projects dir")
	}
}

func TestRun(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: ^1.1\n")
	writeRecipe(t, p, "nested/b.org", "dependencies:\n  openssl.org: ^1.1\n")
	out := filepath.Join(p, "overrides")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()

	// -n writes nothing
	if code := run([]string{"-pantry", p, "-overrides", out, "-n"}, null, null); code != 0 {
		t.Fatalf("dry-run code = %d", code)
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Fatalf("dry run wrote %d file(s)", len(ents))
	}
	// a real run writes one patch per project, named after it
	if code := run([]string{"-pantry", p, "-overrides", out}, null, null); code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{"a.org-openssl3.patch", "nested-b.org-openssl3.patch"} {
		if _, err := os.Stat(filepath.Join(out, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	// a bad flag is a usage error
	if code := run([]string{"-nope"}, null, null); code != 2 {
		t.Errorf("bad flag code = %d, want 2", code)
	}
	// an unreadable pantry is an error, not an empty success
	if code := run([]string{"-pantry", filepath.Join(p, "absent")}, null, null); code != 1 {
		t.Errorf("missing pantry code = %d, want 1", code)
	}
	// an unwritable output directory is an error too
	if code := run([]string{"-pantry", p, "-overrides", filepath.Join(p, "absent")}, null, null); code != 1 {
		t.Errorf("unwritable overrides code = %d, want 1", code)
	}
}

// TestRunSkipsWhatItCannotPatch: a project the rewrite cannot express must be
// reported and stepped over, never abort the other 116.
func TestRunSkipsWhatItCannotPatch(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: ^1.1\n")
	out := filepath.Join(p, "overrides")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	old := patchOne
	patchOne = func(string, string) (string, error) { return "", errBoom }
	defer func() { patchOne = old }()

	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if code := run([]string{"-pantry", p, "-overrides", out}, null, null); code != 0 {
		t.Fatalf("an unpatchable project must not fail the run, code = %d", code)
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Fatalf("wrote %d file(s) for a project it could not patch", len(ents))
	}
}

var errBoom = &boom{}

type boom struct{}

func (*boom) Error() string { return "boom" }

// A recipe the walk cannot read is an error, not a silent omission: a pin we
// never saw is a recipe that stays broken.
func TestPinnedUnreadable(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: ^1.1\n")
	file := filepath.Join(p, "projects", "a.org", "package.yml")
	if err := os.Chmod(file, 0o000); err != nil {
		t.Skip("cannot drop read permission here")
	}
	defer os.Chmod(file, 0o644)
	if _, err := pinned(filepath.Join(p, "projects")); err == nil {
		t.Error("an unreadable recipe must be reported")
	}
	// …and so is a directory the walk cannot descend into.
	dir := filepath.Join(p, "projects", "a.org")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Skip("cannot drop execute permission here")
	}
	defer os.Chmod(dir, 0o755)
	if _, err := pinned(filepath.Join(p, "projects")); err == nil {
		t.Error("an unreadable directory must be reported")
	}
}

func TestMain_(t *testing.T) {
	oldExit, oldArgs := osExit, os.Args
	code := -1
	osExit = func(c int) { code = c }
	os.Args = []string{"openssl3", "-pantry", filepath.Join(t.TempDir(), "absent")}
	defer func() { osExit, os.Args = oldExit, oldArgs }()
	main()
	if code != 1 {
		t.Errorf("main() exit = %d, want 1", code)
	}
}
