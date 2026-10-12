package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// conforming is a file that declares the policy and bootstraps a tool. The
// fixtures below are edits of this, so every test differs from the passing
// case in exactly one way.
const conforming = `name: x
on: { workflow_dispatch: {} }
env:
  GOPROXY: https://proxy.golang.org,direct
  GONOPROXY: github.com/go-pkgx/*
jobs:
  b:
    runs-on: ubuntu-latest
    steps:
      - run: go install github.com/go-pkgx/bk/cmd/bk@main
`

func complaints(t *testing.T, src string) []string {
	t.Helper()
	bad, err := Check("f.yml", []byte(src))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return bad
}

// The positive control. Without it the whole file could be passing because
// Check always complains.
func TestTheConformingFilePasses(t *testing.T) {
	if bad := complaints(t, conforming); len(bad) != 0 {
		t.Errorf("the conforming fixture should pass; got:\n%s", strings.Join(bad, "\n"))
	}
}

func TestAnInlineAssignmentIsRefused(t *testing.T) {
	src := strings.Replace(conforming,
		"- run: go install github.com/go-pkgx/bk/cmd/bk@main",
		"- run: GOPROXY=direct go install github.com/go-pkgx/bk/cmd/bk@main", 1)
	bad := complaints(t, src)
	if len(bad) != 1 {
		t.Fatalf("want exactly the inline complaint, got %d:\n%s", len(bad), strings.Join(bad, "\n"))
	}
	if !strings.Contains(bad[0], "a command line sets the module-proxy policy") {
		t.Errorf("wrong complaint: %s", bad[0])
	}
}

// The one the first run over the real tree got wrong: three of its 33
// complaints were the comment that EXPLAINS the policy. A guard that makes
// the reasoning unsayable gets the reasoning deleted.
func TestACommentExplainingThePolicyIsNotAnAssignment(t *testing.T) {
	src := strings.Replace(conforming,
		"      - run: go install",
		"      - run: |\n          # GOPROXY=direct would make this depend on every vanity host.\n          go install", 1)
	if bad := complaints(t, src); len(bad) != 0 {
		t.Errorf("a comment sets nothing; got:\n%s", strings.Join(bad, "\n"))
	}
}

// And the other direction, so the comment rule is not a hole: an assignment is
// still found when a `#` appears earlier in the line inside a shell word.
//
// It must be a BLOCK SCALAR, and that is the whole lesson of this test. Written
// as a plain scalar — `- run: echo "chunk #1" && GOPROXY=direct go install …` —
// it failed, and not because the guard missed the assignment: in a plain YAML
// scalar ` #` opens a YAML comment, so the value stopped at `echo "chunk` and
// the assignment was never in the document to find. The control was hollow and
// only said so by failing.
func TestAnAssignmentAfterAQuotedHashIsStillFound(t *testing.T) {
	src := strings.Replace(conforming,
		"      - run: go install github.com/go-pkgx/bk/cmd/bk@main",
		"      - run: |\n"+
			"          echo \"chunk #1\"\n"+
			"          GOPROXY=direct go install github.com/go-pkgx/bk/cmd/bk@main", 1)
	// First: the fixture really does carry the `#` into the script, so a
	// failure below is about the guard and not about YAML again.
	var d doc
	if err := yaml.Unmarshal([]byte(src), &d); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(d.scripts(), "\n"); !strings.Contains(got, `"chunk #1"`) {
		t.Fatalf("the fixture lost its # to YAML before the guard saw it:\n%s", got)
	}
	bad := complaints(t, src)
	if len(bad) != 1 || !strings.Contains(bad[0], "a command line sets") {
		t.Errorf("want the inline complaint, got %d:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}

func TestAMissingPolicyIsRefused(t *testing.T) {
	src := strings.Replace(conforming,
		"env:\n  GOPROXY: https://proxy.golang.org,direct\n  GONOPROXY: github.com/go-pkgx/*\n", "", 1)
	bad := complaints(t, src)
	if len(bad) != 2 {
		t.Fatalf("want a complaint per missing key, got %d:\n%s", len(bad), strings.Join(bad, "\n"))
	}
	// Both named, not just a count: a guard that said "the policy is wrong"
	// without saying which half would send the reader back to the file.
	joined := strings.Join(bad, "\n")
	for _, k := range []string{"GOPROXY", "GONOPROXY"} {
		if !strings.Contains(joined, "does not set "+k) {
			t.Errorf("no complaint names %s:\n%s", k, joined)
		}
	}
}

// Half a policy is the dangerous case, and the one a count-only check would
// let through: GOPROXY alone sends OUR modules through the proxy too, which is
// the staleness the `direct` was there to prevent.
func TestGOPROXYWithoutGONOPROXYIsRefused(t *testing.T) {
	src := strings.Replace(conforming, "  GONOPROXY: github.com/go-pkgx/*\n", "", 1)
	bad := complaints(t, src)
	if len(bad) != 1 || !strings.Contains(bad[0], "does not set GONOPROXY") {
		t.Fatalf("want the GONOPROXY complaint, got %d:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}

func TestAWrongValueIsRefusedAndQuoted(t *testing.T) {
	src := strings.Replace(conforming, "GOPROXY: https://proxy.golang.org,direct", "GOPROXY: direct", 1)
	bad := complaints(t, src)
	if len(bad) != 1 {
		t.Fatalf("want one complaint, got %d:\n%s", len(bad), strings.Join(bad, "\n"))
	}
	// The value it found AND the value it wants. "GOPROXY is wrong" would
	// require a trip to the source to act on.
	if !strings.Contains(bad[0], `"direct"`) || !strings.Contains(bad[0], WantGOPROXY) {
		t.Errorf("complaint names neither what it found nor what it wants: %s", bad[0])
	}
}

// A file that fetches nothing by version has no use for the policy, and a
// declaration nobody needs is one nobody keeps right.
func TestAFileThatBootstrapsNothingNeedsNoPolicy(t *testing.T) {
	src := strings.Replace(conforming,
		"env:\n  GOPROXY: https://proxy.golang.org,direct\n  GONOPROXY: github.com/go-pkgx/*\n", "", 1)
	src = strings.Replace(src, "go install github.com/go-pkgx/bk/cmd/bk@main", "go test ./...", 1)
	if bad := complaints(t, src); len(bad) != 0 {
		t.Errorf("`go test ./...` is not a bootstrap; got:\n%s", strings.Join(bad, "\n"))
	}
}

// `go run x@v` is how ci.yml bootstraps bk, and a guard that only knew
// `go install` would have passed three of the fourteen sites.
func TestGoRunOfARemoteModuleCounts(t *testing.T) {
	src := strings.Replace(conforming,
		"env:\n  GOPROXY: https://proxy.golang.org,direct\n  GONOPROXY: github.com/go-pkgx/*\n", "", 1)
	src = strings.Replace(src, "go install github.com/go-pkgx/bk/cmd/bk@main", "go run github.com/go-pkgx/bk/cmd/bk@main overrides", 1)
	if bad := complaints(t, src); len(bad) != 2 {
		t.Errorf("`go run x@main` is a bootstrap; got %d complaints:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}

// A composite action keeps its steps under `runs:`, not `jobs:`. The guard
// reads both, so putting an install in a local action cannot escape it.
func TestACompositeActionIsReadToo(t *testing.T) {
	src := `name: a
description: d
runs:
  using: composite
  steps:
    - shell: bash
      run: GOPROXY=direct go install github.com/go-pkgx/pkgm@main
`
	bad := complaints(t, src)
	if len(bad) == 0 {
		t.Fatal("an install inside a composite action was not seen at all")
	}
	if !strings.Contains(strings.Join(bad, "\n"), "a command line sets") {
		t.Errorf("wrong complaint for the action shape:\n%s", strings.Join(bad, "\n"))
	}
}

// exercise runs the command the way main() does and returns its status and
// both streams, so a test can say WHICH stream a message went to.
func exercise(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// ⛔ A walk that reads nothing must not pass. It returns 2, not 0, for an
// empty directory — a scan that could not reach its input and reported no
// faults is indistinguishable from a clean tree, and that has been shipped
// here before.
func TestAnEmptyDirectoryIsAnError(t *testing.T) {
	code, out, errb := exercise(t, "-dir", t.TempDir())
	if code != 2 {
		t.Errorf("empty directory: exit %d, want 2 (%s%s)", code, out, errb)
	}
	// On stderr, not stdout: the findings are stdout's and a reader piping
	// them somewhere must not have this land in the middle of them.
	if !strings.Contains(errb, "nothing was checked") {
		t.Errorf("it does not say on stderr that it checked nothing: out=%q err=%q", out, errb)
	}
}

func TestUnparsableYAMLIsAnErrorNotAPass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.yml"), []byte("env: [this is not a mapping\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := exercise(t, "-dir", dir)
	if code != 2 {
		t.Errorf("unparsable YAML: exit %d, want 2 (%s%s)", code, out, errb)
	}
	if !strings.Contains(errb, "x.yml") {
		t.Errorf("the error does not name the file that could not be parsed: %q", errb)
	}
}

// A conforming directory exits 0 and says what it read. The count is part of
// the contract: "0 complaints" over an unstated number of files is the shape
// of a scan that read one file and passed.
func TestAConformingDirectoryExitsZeroAndSaysWhatItRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.yml"), []byte(conforming), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := exercise(t, "-dir", dir)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (%s%s)", code, out, errb)
	}
	if !strings.Contains(out, "1 file(s), 0 complaint(s)") {
		t.Errorf("summary does not say what it read: %q", out)
	}
}

// A complaining directory exits 1 — distinct from the 2 the tool uses for "I
// could not look". A caller that cannot tell those apart cannot tell a dirty
// tree from a broken checkout.
func TestAComplainingDirectoryExitsOneAndPrintsToStdout(t *testing.T) {
	dir := t.TempDir()
	bad := strings.Replace(conforming, "- run: go install", "- run: GOPROXY=direct go install", 1)
	if err := os.WriteFile(filepath.Join(dir, "bad.yml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := exercise(t, "-dir", dir)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(out, "a command line sets") || !strings.Contains(out, "1 complaint(s)") {
		t.Errorf("the complaint is not on stdout: %q", out)
	}
}

// An unreadable file is an error, not a silent skip: a guard that quietly
// passes over what it could not open reports a clean tree it never saw.
func TestAnUnreadableFileIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file, so this cannot be staged as root")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "locked.yml")
	if err := os.WriteFile(p, []byte(conforming), 0o000); err != nil {
		t.Fatal(err)
	}
	// Restored so t.TempDir()'s cleanup can remove it.
	defer func() { _ = os.Chmod(p, 0o644) }()
	code, out, errb := exercise(t, "-dir", dir)
	if code != 2 {
		t.Errorf("unreadable file: exit %d, want 2 (%s%s)", code, out, errb)
	}
}

// A bad flag comes back as a status. With flag.ExitOnError it would have
// killed the test process instead, which is why run takes its args.
func TestAnUnknownFlagIsAStatusNotAnExit(t *testing.T) {
	code, _, errb := exercise(t, "-nope")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(errb, "nope") {
		t.Errorf("usage went nowhere useful: %q", errb)
	}
}

// main() itself, through the osExit seam — so the wiring from os.Args to the
// status is exercised and not assumed.
func TestMain_(t *testing.T) {
	oldExit, oldArgs, oldStdout := osExit, os.Args, os.Stdout
	got := -1
	osExit = func(code int) { got = code }
	os.Args = []string{"goproxycheck", "-dir", t.TempDir()}
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = devnull
	defer func() {
		osExit, os.Args, os.Stdout = oldExit, oldArgs, oldStdout
		_ = devnull.Close()
	}()
	main()
	if got != 2 {
		t.Errorf("main() exited %d for an empty -dir, want 2", got)
	}
}

// The walk's own error arm: a directory it cannot enter. Distinct from the
// unreadable FILE above, and reached through a different branch.
func TestADirectoryThatCannotBeEnteredIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root enters a 0000 directory")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "shut")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.yml"), []byte(conforming), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(sub, 0o755) }()
	code, out, errb := exercise(t, "-dir", dir)
	if code != 2 {
		t.Errorf("unenterable directory: exit %d, want 2 (%s%s)", code, out, errb)
	}
}

// The guard against the whole point: this repository's own workflows. The
// fixtures prove the rule, this proves the tree obeys it — and it is the
// assertion that fails the day somebody adds a fifteenth site.
func TestThisRepositorysWorkflowsConform(t *testing.T) {
	bad, seen, err := CheckDir(filepath.Join("..", ".github"))
	if err != nil {
		t.Fatal(err)
	}
	if seen < 11 {
		t.Fatalf("only %d file(s) read under ../.github — this test stopped finding the workflows rather than the workflows going away", seen)
	}
	if len(bad) != 0 {
		t.Errorf("%d complaint(s) about this repository's own workflows:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}
