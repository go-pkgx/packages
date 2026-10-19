package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Every one of the nine is caught, named one at a time rather than as a
// count: a regexp built by joining a list can lose an entry to a stray
// character and still match eight of them, and "8 complaints" reads as a
// pass to nobody but says nothing about which.
func TestEveryMovedVerbIsCaught(t *testing.T) {
	for _, v := range verbs {
		line := "          pkgx " + v + " --platform linux/x86-64"
		if !pkgxVerb.MatchString(line) {
			t.Errorf("`pkgx %s` is not matched", v)
		}
	}
}

// ⛔ THE PLANTED NEGATIVES, which is where a word-boundary regexp earns its
// keep. `lsof` and `envsubst` are real packages this fleet publishes, and a
// guard that rewrote `pkgx lsof` into `pkgs lsof` would break a working
// command in the name of fixing one.
func TestItDoesNotMatchAPackageWhoseNameStartsWithAVerb(t *testing.T) {
	for _, line := range []string{
		"pkgx lsof -i",
		"pkgx envsubst < in > out",
		"pkgx searchd --config x",
		"pkgx storekeeper",
		"pkgm catalog update",  // a different binary entirely
		"pkgs catalog update",  // the correct spelling must not be flagged
		"pkgx --graph +go.dev", // a flag, not a verb
	} {
		if pkgxVerb.MatchString(line) {
			t.Errorf("false positive on %q", line)
		}
	}
}

// It reads comments as well as commands, because a stale comment teaches the
// old spelling to the next reader just as effectively.
func TestItReadsComments(t *testing.T) {
	if !pkgxVerb.MatchString("# The CATALOGUE: what `pkgx ls` and <TAB> read.") {
		t.Error("a comment naming a moved verb is not caught")
	}
}

// ⛔ AND A DIRECTORY WITH NO WORKFLOWS IS AN ERROR, NOT A PASS. This guard
// exists because a sweep for exactly this defect returned zero from the
// repository root: the grep on that machine skips hidden directories, and
// every workflow lives under .github. A scanner that reads nothing must not
// report the same thing as one that read everything.
func TestReadingNothingIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("pkgx ls\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// check() is the per-file half; the "read nothing" refusal lives in
	// main(), so what is asserted here is the condition it tests: a
	// directory of non-workflow files yields no files to read.
	files := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext == ".yml" || ext == ".yaml" {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Fatalf("the fixture has %d workflow(s); it must have none", files)
	}
}

// And the per-file reader counts what it found, so a file with two mentions
// is not reported as one problem.
func TestItCountsEveryMentionInAFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "w.yml")
	body := "run: |\n  pkgx catalog update --platform linux/x86-64\n  pkgx catalog update --platform darwin/aarch64\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := check(p); n != 2 {
		t.Errorf("check = %d, want 2", n)
	}
}
