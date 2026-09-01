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
	if rc := run([]string{"-pantry", p, "-overrides", out, "-n"}, os.Stdout, os.Stderr); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	ents, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("-n wrote %d file(s)", len(ents))
	}
	if rc := run([]string{"-pantry", p, "-overrides", out}, os.Stdout, os.Stderr); rc != 0 {
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
	if rc := run([]string{"-pantry", p, "-overrides", out}, os.Stdout, os.Stderr); rc != 0 {
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
