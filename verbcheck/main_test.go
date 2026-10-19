package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// A clean tree passes and says how much it read, so a reader can tell "12
// files, nothing wrong" from "nothing to read".
func TestACleanTreePasses(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ok.yml", "run: pkgs catalog update\n")
	var out, errb bytes.Buffer
	if code := run([]string{dir}, &out, &errb); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "1 file(s), 0 complaint(s)") {
		t.Errorf("output = %q", out.String())
	}
}

// Every mention is counted and located, so a file with two of them is not
// reported as one problem.
func TestItCountsAndLocatesEveryMention(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "w.yml", "run: |\n  pkgx catalog update --platform linux/x86-64\n  pkgx catalog update --platform darwin/aarch64\n")
	var out, errb bytes.Buffer
	if code := run([]string{dir}, &out, &errb); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	for _, want := range []string{"w.yml:2:", "w.yml:3:", "write `pkgs catalog`"} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("stderr %q does not contain %q", errb.String(), want)
		}
	}
	if !strings.Contains(out.String(), "2 complaint(s)") {
		t.Errorf("output = %q", out.String())
	}
}

// ⛔ READING NOTHING IS EXIT 2, NOT SUCCESS. This guard exists because a
// sweep for exactly this defect returned zero from a repository root: the
// grep on that machine skips hidden directories, and every workflow lives
// under .github. A scanner that read nothing must not report what one that
// read everything reports.
func TestReadingNoWorkflowIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "notes.txt", "pkgx ls\n") // not a workflow: not read
	var out, errb bytes.Buffer
	if code := run([]string{dir}, &out, &errb); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "read no workflow at all") {
		t.Errorf("stderr = %q", errb.String())
	}
}

// A directory that is not there is an error with its name in it, not a
// silent zero — the same failure one layer out.
func TestAMissingDirectoryIsAnError(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{filepath.Join(t.TempDir(), "nope")}, &out, &errb); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "nope") {
		t.Errorf("the error does not name the directory: %q", errb.String())
	}
}

// An unreadable file counts as a complaint rather than passing: "could not
// look" and "looked and found nothing" must not share an exit code.
func TestAnUnreadableFileIsAComplaint(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "w.yml")
	if err := os.Mkdir(p, 0o755); err != nil { // a directory named *.yml
		t.Fatal(err)
	}
	var errb bytes.Buffer
	if n := check(p, &errb); n != 1 {
		t.Errorf("check = %d, want 1", n)
	}
}

// .yaml is read as well as .yml, since both spellings are in this tree.
func TestBothExtensionsAreRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.yaml", "run: pkgx why a b\n")
	var out, errb bytes.Buffer
	if code := run([]string{dir}, &out, &errb); code != 1 {
		t.Fatalf("code = %d, want 1 (stderr %q)", code, errb.String())
	}
}

// With no argument it looks where the workflows are.
func TestTheDefaultDirectoryIsTheWorkflows(t *testing.T) {
	var out, errb bytes.Buffer
	// From the repository root the default path exists and is clean; this
	// asserts the default is WIRED, not that the tree is clean, so it
	// accepts any outcome but "read nothing".
	if code := run(nil, &out, &errb); code == 2 {
		t.Errorf("the default path read no workflow: %q", errb.String())
	}
}

// osExit is replaced in main so the binary can exit; naming it here keeps
// the coverage gate honest about the one line main() holds.
func TestMainExitsWithTheCode(t *testing.T) {
	prev := osExit
	defer func() { osExit = prev }()
	got := -1
	osExit = func(code int) { got = code }
	old := os.Args
	os.Args = []string{"verbcheck", filepath.Join(t.TempDir())}
	defer func() { os.Args = old }()
	main()
	if got != 2 {
		t.Errorf("main exited %d, want 2 (an empty directory reads no workflow)", got)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
