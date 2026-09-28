package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pkgx/bk/logical"
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
	if _, err := os.Stat(filepath.Join(out, "crates.io-x.hcl")); err != nil {
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
	orig := overrideOne
	defer func() { overrideOne = orig }()
	overrideOne = func(pantry, proj string) ([]byte, error) {
		if proj == "crates.io/a" {
			return nil, os.ErrNotExist
		}
		return orig(pantry, proj)
	}
	if rc := run([]string{"-pantry", p, "-overrides", out}, devnull(t), devnull(t)); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if _, err := os.Stat(filepath.Join(out, "crates.io-b.hcl")); err != nil {
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

// Two overrides on one project used to be able to silence each other, because
// a diff carries its neighbours as CONTEXT: the openssl3 patch for
// crates.io/zellij carried `cargo install --path .` as context, this tool
// rewrote that line, and the openssl3 patch stopped applying. The build died
// on `no version of openssl.org satisfies "^1.1"` — a message naming openssl
// that has nothing to do with openssl. Guarding against it cost eighty lines
// and two excluded projects.
//
// Logical overrides address KEYS. openssl3 assigns
// `dependencies["openssl.org"]`; this edits `build.script`. There is nothing
// to overlap.
func TestTwoOverridesOnOneProjectDoNotCollide(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/zellij",
		"dependencies:\n  openssl.org: ^1.1\nbuild:\n  script: cargo install --path .\n")

	src, err := overrideFor(p, "crates.io/zellij")
	if err != nil {
		t.Fatal(err)
	}
	mine, err := logical.Parse(src, "z.hcl")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := logical.Parse([]byte(`
project = "crates.io/zellij"
why     = "our registry carries no openssl 1.x"
edits   = [{ path = "dependencies[\"openssl.org\"]", set = "^3" }]
`), "o.hcl")
	if err != nil {
		t.Fatal(err)
	}

	// Both, in either order, and the result is the same.
	for _, order := range [][]logical.Op{
		append(append([]logical.Op{}, mine.Ops...), theirs.Ops...),
		append(append([]logical.Op{}, theirs.Ops...), mine.Ops...),
	} {
		doc, err := recipeDoc(p, "crates.io/zellij")
		if err != nil {
			t.Fatal(err)
		}
		res, err := logical.Apply(doc, order)
		if err != nil {
			t.Fatalf("applying both: %v", err)
		}
		for _, r := range res {
			if r.Outcome != logical.Applied {
				t.Errorf("%s reports %q — one silenced the other", r.Op.Path, r.Outcome)
			}
		}
		if got := doc["dependencies"].(map[string]any)["openssl.org"]; got != "^3" {
			t.Errorf("openssl = %v", got)
		}
		if got := doc["build"].(map[string]any)["script"]; !strings.Contains(got.(string), "--locked") {
			t.Errorf("script = %v", got)
		}
	}
}

// And the operations are IDEMPOTENT. The obvious hand-written version —
// "cargo install " → "cargo install --locked " — is not: the replacement
// contains the run, so a second pass gives `--locked --locked`. Deriving them
// inherits the rule rather than re-deriving it badly.
func TestTheDerivedSubstitutionAppliesOnlyOnce(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/a", "build:\n  script: cargo install --path . --root x\n")
	src, err := overrideFor(p, "crates.io/a")
	if err != nil {
		t.Fatal(err)
	}
	o, err := logical.Parse(src, "a.hcl")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := recipeDoc(p, "crates.io/a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := logical.Apply(doc, o.Ops); err != nil {
		t.Fatal(err)
	}
	once := doc["build"].(map[string]any)["script"].(string)
	res, err := logical.Apply(doc, o.Ops)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Outcome != logical.Redundant {
			t.Errorf("a second pass reports %q, not redundant", r.Outcome)
		}
	}
	if twice := doc["build"].(map[string]any)["script"].(string); twice != once {
		t.Errorf("applying twice changed it:\n once  %q\n twice %q", once, twice)
	}
	if strings.Count(once, "--locked") != 1 {
		t.Errorf("got %q", once)
	}
}

// The walk finds an install wherever it sits, and the shapes a recipe writes
// one in: a plain string, a list, a step written as a mapping, a
// platform-scoped script.
func TestOverrideForFindsEveryShape(t *testing.T) {
	for _, tc := range []struct{ name, recipe, wantPath string }{
		{"a plain script", "build:\n  script: cargo install --path .\n", "build.script"},
		{"a list", "build:\n  script:\n    - cargo install --path .\n    - echo done\n", "build.script"},
		{"a mapping step", "build:\n  script:\n    - run: cargo install --path .\n      if: '>=1'\n", "build.script"},
		{"under a platform", "build:\n  linux:\n    script: cargo install --path .\n", "build.linux.script"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := t.TempDir()
			writeRecipe(t, p, "crates.io/a", tc.recipe)
			src, err := overrideFor(p, "crates.io/a")
			if err != nil {
				t.Fatal(err)
			}
			o, err := logical.Parse(src, "a.hcl")
			if err != nil {
				t.Fatalf("%v\n%s", err, src)
			}
			if len(o.Ops) != 1 || o.Ops[0].Path.String() != tc.wantPath {
				t.Fatalf("ops = %+v", o.Ops)
			}
			// And it reproduces: applying it leaves --locked exactly once.
			doc, err := recipeDoc(p, "crates.io/a")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := logical.Apply(doc, o.Ops); err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(fmt.Sprint(doc), "--locked"); n != 1 {
				t.Errorf("--locked appears %d times: %v", n, doc)
			}
		})
	}
}

// A recipe with nothing to do, and one that cannot be read at all.
func TestOverrideForRefuses(t *testing.T) {
	p := t.TempDir()
	if _, err := overrideFor(p, "absent.example"); err == nil {
		t.Error("a missing recipe must be an error")
	}
	writeRecipe(t, p, "crates.io/bad", "a: [\n")
	if _, err := overrideFor(p, "crates.io/bad"); err == nil {
		t.Error("a recipe that is not YAML must be an error")
	}
	writeRecipe(t, p, "crates.io/done", "build:\n  script: cargo install --locked --path .\n")
	if _, err := overrideFor(p, "crates.io/done"); err == nil {
		t.Error("a recipe that already locks has nothing to do")
	}
	// A value that is not text at all, beside one that is.
	writeRecipe(t, p, "crates.io/mixed", "build:\n  jobs: 4\n  script: cargo install --path .\n")
	if _, err := overrideFor(p, "crates.io/mixed"); err != nil {
		t.Errorf("a number beside a command must not stop it: %v", err)
	}
}

// carriesTheFlag: the generator checks a file it does not own rather than
// overwriting somebody's work.
func TestCarriesTheFlag(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return f
	}
	yes := write("yes.hcl", `
project = "crates.io/a"
why     = "somebody's own reason"
edits   = [{ path = "build.script", from = "install --path", to = "install --locked --path" }]
`)
	if ok, err := carriesTheFlag(yes); err != nil || !ok {
		t.Errorf("ok=%v err=%v", ok, err)
	}
	no := write("no.hcl", `
project = "crates.io/a"
why     = "somebody's own reason"
edits   = [{ path = "dependencies[\"openssl.org\"]", set = "^3" }]
`)
	if ok, err := carriesTheFlag(no); err != nil || ok {
		t.Errorf("ok=%v err=%v", ok, err)
	}
	// A substitution whose `from` ALREADY has --locked is not this tool's.
	already := write("already.hcl", `
project = "crates.io/a"
why     = "somebody's own reason"
edits   = [{ path = "build.script", from = "install --locked --path", to = "install --locked --path=." }]
`)
	if ok, err := carriesTheFlag(already); err != nil || ok {
		t.Errorf("ok=%v err=%v", ok, err)
	}
	if _, err := carriesTheFlag(filepath.Join(dir, "absent.hcl")); err == nil {
		t.Error("a missing file must be an error")
	}
	if _, err := carriesTheFlag(write("bad.hcl", "project = ")); err == nil {
		t.Error("a file that does not parse must be an error")
	}
}

// The run reports a shared file it may not own, and leaves it untouched.
func TestRunChecksASharedOverride(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "crates.io/shared", "build:\n  script: cargo install --path .\n")
	out := t.TempDir()
	hand := "\nproject = \"crates.io/shared\"\nwhy     = \"it needs openssl 3, and somebody wrote that down\"\nedits   = [{ path = \"dependencies[\\\"openssl.org\\\"]\", set = \"^3\" }]\n"
	if err := os.WriteFile(filepath.Join(out, "crates.io-shared.hcl"), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if rc := run([]string{"-pantry", p, "-overrides", out}, &stdout, &stderr); rc != 0 {
		t.Fatalf("rc = %d\n%s", rc, stderr.String())
	}
	got, err := os.ReadFile(filepath.Join(out, "crates.io-shared.hcl"))
	if err != nil || string(got) != hand {
		t.Errorf("the hand-written override was rewritten:\n%s", got)
	}
	if !strings.Contains(stderr.String(), "does NOT add --locked") ||
		!strings.Contains(stdout.String(), "must be edited by hand") {
		t.Errorf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}

	// A file that exists and does not parse is reported, not overwritten.
	if err := os.WriteFile(filepath.Join(out, "crates.io-shared.hcl"), []byte("project = "), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if rc := run([]string{"-pantry", p, "-overrides", out}, &stdout, &stderr); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if !strings.Contains(stderr.String(), "crates.io-shared.hcl") {
		t.Errorf("stderr:\n%s", stderr.String())
	}
}
