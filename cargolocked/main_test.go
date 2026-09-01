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

// TestInstallLine pins exactly what the rewrite claims to touch and what it
// must leave alone. Every YAML shape below occurs in the pantry today.
func TestInstallLine(t *testing.T) {
	for _, in := range []string{
		"  script: cargo install --path . --root {{prefix}}",
		"    - cargo install --path . --root {{prefix}}",
		"    - run: cargo install --path crates/cli --root {{prefix}}",
		"  script: cd cli && cargo install --path . --root {{prefix}}",
		"  script: cargo  install --path .", // two spaces: still the verb
	} {
		if !rewritable(in) {
			t.Errorf("must be rewritten: %q", in)
		}
	}
	for _, in := range []string{
		"  script: cargo install --locked --path . --root {{prefix}}", // already correct
		"  script: cargo install --path . --locked",                   // correct, flag last
		"  script: cargo build --release",                             // a different verb: ignores the lock too, but a different fix
		"  script: cargo-install --path .",                            // a different program
		"  # cargo install bpb does not work because ...",             // prose, not a command
		"  script: cargo install $CARGO_ARGS",                         // flags live in a variable — qsv already passes --locked there
	} {
		if rewritable(in) {
			t.Errorf("must NOT be rewritten: %q", in)
		}
	}
}

// TestRewritePreservesTheLine: only the flag is inserted — indentation, YAML
// shape and every other argument survive, because these patches are meant to be
// proposed upstream and a gratuitous reformat is a reason to reject one.
func TestRewritePreservesTheLine(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/pqrs", "build:\n  dependencies:\n    rust-lang.org: \">=1.65\"\n  script: cargo install --path . --root {{prefix}}\n")
	patch, err := patchFor(p, "crates.io/pqrs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "-  script: cargo install --path . --root {{prefix}}\n+  script: cargo install --locked --path . --root {{prefix}}\n") {
		t.Errorf("line not preserved:\n%s", patch)
	}
	if !strings.Contains(patch, "--- a/projects/crates.io/pqrs/package.yml\n+++ b/projects/crates.io/pqrs/package.yml\n") {
		t.Errorf("paths must be relative to the pantry root:\n%s", patch)
	}
	if strings.Contains(patch, "-    rust-lang.org") {
		t.Errorf("touched an unrelated line:\n%s", patch)
	}
}

// A workspace can install twice, and a recipe can mix a locked line with an
// unlocked one. Both unlocked lines are rewritten; the locked one stays context.
func TestTwoInstallsOneAlreadyLocked(t *testing.T) {
	p := t.TempDir()
	var y strings.Builder
	y.WriteString("build:\n  script:\n    - cargo install --path cli --root {{prefix}}\n")
	for i := 0; i < 10; i++ {
		y.WriteString("    - echo filler" + string(rune('a'+i)) + "\n")
	}
	y.WriteString("    - cargo install --locked --path gui --root {{prefix}}\n")
	y.WriteString("    - cargo install --path tui --root {{prefix}}\n")
	writeRecipe(t, p, "crates.io/two", y.String())
	patch, err := patchFor(p, "crates.io/two")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(patch, "+    - cargo install --locked --path"); n != 2 {
		t.Errorf("want 2 rewritten lines, got %d:\n%s", n, patch)
	}
	if strings.Contains(patch, "-    - cargo install --locked --path gui") {
		t.Errorf("rewrote an already-locked line:\n%s", patch)
	}
	if strings.Contains(patch, "--locked --locked") {
		t.Errorf("double flag:\n%s", patch)
	}
	// The far-apart hits must not land in one hunk that repeats context.
	// A hunk header opens with "@@ -" and closes with a bare "@@", so count the
	// opening only — counting "@@" reports twice as many hunks as there are.
	if n := strings.Count(patch, "@@ -"); n != 2 {
		t.Errorf("want 2 hunks, got %d:\n%s", n, patch)
	}
}

// A recipe with nothing to fix is not an error the run should carry: patchFor
// declines it, and unlocked never offers it in the first place.
func TestAlreadyLockedIsNotOffered(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/ok", "build:\n  script: cargo install --locked --path .\n")
	if _, err := patchFor(p, "crates.io/ok"); err == nil {
		t.Error("want an error for a recipe with nothing to rewrite")
	}
	got, _, err := unlocked(filepath.Join(p, "projects"), "crates.io/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no candidates, got %v", got)
	}
}

// The default scope is crates.io/ because that is where the precondition —
// the release ships a Cargo.lock — was actually measured. A recipe outside it
// is left alone until someone measures it too.
func TestPrefixScopesTheSweep(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/in", "build:\n  script: cargo install --path .\n")
	writeRecipe(t, p, "elsewhere.org/out", "build:\n  script: cargo install --path .\n")
	got, _, err := unlocked(filepath.Join(p, "projects"), "crates.io/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "crates.io/in" {
		t.Errorf("want only crates.io/in, got %v", got)
	}
	all, _, err := unlocked(filepath.Join(p, "projects"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("an empty prefix must widen the sweep, got %v", all)
	}
}

// -n writes nothing. A generator that reports and edits in the same breath is
// one nobody can inspect before it runs.
func TestDryRunWritesNothing(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/x", "build:\n  script: cargo install --path .\n")
	out := t.TempDir()
	if rc := run([]string{"-pantry", p, "-overrides", out, "-n"}, devnull(t), devnull(t)); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	ents, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("-n wrote %d file(s)", len(ents))
	}
	if rc := run([]string{"-pantry", p, "-overrides", out}, devnull(t), devnull(t)); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if _, err := os.Stat(filepath.Join(out, "crates.io-x-cargo-locked.patch")); err != nil {
		t.Errorf("a real run must write the patch: %v", err)
	}
}

// One unpatchable project must not abort the sweep: the others still get their
// patch, and the failure is named on stderr.
func TestOneFailureDoesNotStopTheRun(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/a", "build:\n  script: cargo install --path .\n")
	writeRecipe(t, p, "crates.io/b", "build:\n  script: cargo install --path .\n")
	out := t.TempDir()
	orig := patchOne
	defer func() { patchOne = orig }()
	patchOne = func(pantry, proj string) (string, error) {
		if proj == "crates.io/a" {
			return "", os.ErrNotExist
		}
		return orig(pantry, proj)
	}
	if rc := run([]string{"-pantry", p, "-overrides", out}, devnull(t), devnull(t)); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if _, err := os.Stat(filepath.Join(out, "crates.io-b-cargo-locked.patch")); err != nil {
		t.Errorf("the healthy project must still be patched: %v", err)
	}
}

// A recipe whose install flags come from a shell variable is REPORTED, never
// rewritten: crates.io/qsv sets --locked inside CARGO_ARGS, and patching the
// line would have passed the flag twice.
func TestVariableArgumentsAreDeferredNotPatched(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/qsv", "build:\n  script: cargo install $CARGO_ARGS\n  env:\n    CARGO_ARGS:\n      - --locked\n")
	got, deferred, err := unlocked(filepath.Join(p, "projects"), "crates.io/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("must not be offered for rewriting: %v", got)
	}
	if len(deferred) != 1 || deferred[0] != "crates.io/qsv" {
		t.Errorf("must be reported for a person to read: %v", deferred)
	}
}

// Prose that happens to quote the command is not the command. crates.io/bpb
// explains why `cargo install bpb` does not work; the explanation must survive.
func TestACommentIsNotACommand(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/bpb", "# cargo install bpb does not work because cargo does not require correct\n# metadata\nbuild:\n  script: cargo install --path . --root {{prefix}}\n")
	patch, err := patchFor(p, "crates.io/bpb")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(patch, "-# cargo install") {
		t.Errorf("rewrote a comment:\n%s", patch)
	}
	if !strings.Contains(patch, "+  script: cargo install --locked --path .") {
		t.Errorf("did not rewrite the command:\n%s", patch)
	}
}

// devnull is where the tests send output they do not assert on: a generator
// that prints twenty file names is unreadable test noise.
func devnull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// The three ways a run can fail, each with its own exit code, because a
// generator that answers 0 to a mistyped flag writes nothing and says so in a
// way no script can see.
func TestRunReportsItsFailures(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/a", "build:\n  script: cargo install --path .\n")
	null := devnull(t)
	if code := run([]string{"-nope"}, null, null); code != 2 {
		t.Errorf("bad flag code = %d, want 2", code)
	}
	if code := run([]string{"-pantry", filepath.Join(p, "absent")}, null, null); code != 1 {
		t.Errorf("missing pantry code = %d, want 1", code)
	}
	if code := run([]string{"-pantry", p, "-overrides", filepath.Join(p, "absent")}, null, null); code != 1 {
		t.Errorf("unwritable overrides code = %d, want 1", code)
	}
}

// A deferred project is named on the run's stderr, not just returned: it is the
// only trace a person gets that two recipes were left alone on purpose.
func TestRunNamesTheDeferred(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/qsv", "build:\n  script: cargo install $CARGO_ARGS\n")
	out := t.TempDir()
	errf, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errf.Close()
	if code := run([]string{"-pantry", p, "-overrides", out}, devnull(t), errf); code != 0 {
		t.Fatalf("code = %d", code)
	}
	b, err := os.ReadFile(errf.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "crates.io/qsv installs with arguments from a variable") {
		t.Errorf("stderr does not name the deferred project:\n%s", b)
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Errorf("wrote %d patch(es) for a project it deferred", len(ents))
	}
}

// A recipe the walk cannot read is an error, not a silent omission: a straggler
// we never saw is a build that stays unreproducible.
func TestUnlockedReportsWhatItCannotRead(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/a", "build:\n  script: cargo install --path .\n")
	file := filepath.Join(p, "projects", "crates.io", "a", "package.yml")
	if err := os.Chmod(file, 0o000); err != nil {
		t.Skip("cannot drop read permission here")
	}
	defer os.Chmod(file, 0o644)
	if _, _, err := unlocked(filepath.Join(p, "projects"), "crates.io/"); err == nil {
		t.Error("an unreadable recipe must be reported")
	}
}

// patchFor is also reached for a project that is gone (the pantry moved between
// the walk and the write); it must say so rather than emit an empty patch.
func TestPatchForAbsentRecipe(t *testing.T) {
	if _, err := patchFor(t.TempDir(), "crates.io/ghost"); err == nil {
		t.Error("want an error for a recipe that is not there")
	}
}

// Two installs three lines apart share their context. They must come out as ONE
// hunk: a patch that lists the same context line twice is rejected by git.
func TestAdjacentInstallsMergeIntoOneHunk(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/near", "build:\n  script:\n    - cargo install --path cli --root {{prefix}}\n    - echo between\n    - cargo install --path tui --root {{prefix}}\n")
	patch, err := patchFor(p, "crates.io/near")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(patch, "@@ -"); n != 1 {
		t.Errorf("want 1 merged hunk, got %d:\n%s", n, patch)
	}
	if n := strings.Count(patch, "    - echo between"); n != 1 {
		t.Errorf("context line repeated %d times:\n%s", n, patch)
	}
}

func TestMain_(t *testing.T) {
	oldExit, oldArgs := osExit, os.Args
	code := -1
	osExit = func(c int) { code = c }
	os.Args = []string{"cargolocked", "-pantry", filepath.Join(t.TempDir(), "absent")}
	defer func() { osExit, os.Args = oldExit, oldArgs }()
	main()
	if code != 1 {
		t.Errorf("main() exit = %d, want 1", code)
	}
}

// A project on the exclusion list is named on stderr and gets no patch: the
// list exists because `--locked` was MEASURED to be wrong there, and a silent
// skip would leave nobody able to tell that from a bug in the sweep.
func TestExcludedProjectIsNamedAndNotPatched(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/pueue", "build:\n  script: cargo install --path pueue --root {{prefix}}\n")
	out := t.TempDir()
	errf, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errf.Close()
	if code := run([]string{"-pantry", p, "-overrides", out}, devnull(t), errf); code != 0 {
		t.Fatalf("code = %d", code)
	}
	b, _ := os.ReadFile(errf.Name())
	if !strings.Contains(string(b), "crates.io/pueue excluded — its lock pins time 0.3.31") {
		t.Errorf("stderr does not explain the exclusion:\n%s", b)
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Errorf("wrote a patch for an excluded project")
	}
}

// Two patches on one file are fine; two whose HUNKS overlap are not — the first
// to apply changes a line the second carries as context, and the second is then
// skipped, so the build reads the un-patched value and fails somewhere else.
func TestOverlappingHunkIsRefused(t *testing.T) {
	p := t.TempDir()
	// `openssl.org` sits three lines above the install line, so a 3-line context
	// window puts them in one span — zellij's exact shape.
	writeRecipe(t, p, "crates.io/zellij", "build:\n  dependencies:\n    rust-lang.org: '*'\n    openssl.org: ^1.1\n    perl.org: ^5\n  script: cargo install --path . --root {{prefix}}\n")
	out := t.TempDir()
	other := "diff --git a/projects/crates.io/zellij/package.yml b/projects/crates.io/zellij/package.yml\n" +
		"--- a/projects/crates.io/zellij/package.yml\n+++ b/projects/crates.io/zellij/package.yml\n" +
		"@@ -1,6 @@\n"
	if err := os.WriteFile(filepath.Join(out, "crates.io-zellij-openssl3.patch"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	errf, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errf.Close()
	if code := run([]string{"-pantry", p, "-overrides", out}, devnull(t), errf); code != 0 {
		t.Fatalf("code = %d", code)
	}
	b, _ := os.ReadFile(errf.Name())
	if !strings.Contains(string(b), "overlaps another override patch") {
		t.Errorf("stderr does not report the overlap:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(out, "crates.io-zellij"+suffix)); err == nil {
		t.Error("wrote a patch that would silence the other one")
	}
	// A patch far from ours is NOT a collision: excluding by FILE would drop
	// four projects that are fine today.
	far := strings.Replace(other, "@@ -1,6 @@", "@@ -1,2 @@\n", 1)
	if err := os.WriteFile(filepath.Join(out, "crates.io-zellij-openssl3.patch"), []byte(far), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-pantry", p, "-overrides", out}, devnull(t), devnull(t)); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if _, err := os.Stat(filepath.Join(out, "crates.io-zellij"+suffix)); err != nil {
		t.Errorf("a non-overlapping patch must not block ours: %v", err)
	}
}

// An unreadable overrides directory is an error, not an empty set of spans:
// treating it as empty would emit exactly the patches this check exists to stop.
func TestUnreadableOverridesDirIsAnError(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/a", "build:\n  script: cargo install --path .\n")
	out := t.TempDir()
	bad := filepath.Join(out, "x.patch")
	if err := os.WriteFile(bad, []byte("--- a/f\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(bad, 0o644)
	if _, err := hunkRangesByFile(out); err == nil {
		t.Skip("cannot drop read permission here")
	}
	if code := run([]string{"-pantry", p, "-overrides", out}, devnull(t), devnull(t)); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

// Re-running must not read this tool's OWN previous output as a rival patch:
// every project would then look like it collides with itself and the sweep
// would empty the directory it just filled.
func TestOurOwnPatchesAreNotRivals(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/a", "build:\n  script: cargo install --path . --root {{prefix}}\n")
	out := t.TempDir()
	if code := run([]string{"-pantry", p, "-overrides", out}, devnull(t), devnull(t)); code != 0 {
		t.Fatalf("first run code = %d", code)
	}
	want := filepath.Join(out, "crates.io-a"+suffix)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("first run wrote nothing: %v", err)
	}
	if code := run([]string{"-pantry", p, "-overrides", out}, devnull(t), devnull(t)); code != 0 {
		t.Fatalf("second run code = %d", code)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the second run dropped its own patch: %v", err)
	}
}

// An overrides path the glob cannot parse is an error, not an empty span set —
// same reason as the unreadable directory: an empty set emits exactly the
// patches the check exists to stop.
func TestUnglobbableOverridesDir(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/a", "build:\n  script: cargo install --path .\n")
	bad := filepath.Join(t.TempDir(), "a[b")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Skip("cannot create that directory name here")
	}
	if code := run([]string{"-pantry", p, "-overrides", bad}, devnull(t), devnull(t)); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}
