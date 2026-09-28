package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pkgx/bk/logical"
	"github.com/go-pkgx/bk/overrides"
)

// gitPantry builds the shape overrides.Apply needs: a git worktree, because it
// resets every file a patch touches to its committed state before applying.
func gitPantry(t *testing.T, recipes map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for proj, body := range recipes {
		p := filepath.Join(dir, "projects", proj, "package.yml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.test"},
		{"config", "user.name", "t"},
		{"add", "-A"},
		{"commit", "-qm", "base"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

const acmeRecipe = `distributable:
  url: https://acme.org/{{version}}.tar.gz
dependencies:
  openssl.org: ^1.1
build:
  script: make install
provides:
  - bin/acme
`

// A patch that retargets the openssl pin, in the shape the directory holds.
const acmePatch = `diff --git a/projects/acme.org/package.yml b/projects/acme.org/package.yml
index 1111111..2222222 100644
--- a/projects/acme.org/package.yml
+++ b/projects/acme.org/package.yml
@@ -1,7 +1,7 @@
 distributable:
   url: https://acme.org/{{version}}.tar.gz
 dependencies:
-  openssl.org: ^1.1
+  openssl.org: ^3
 build:
   script: make install
 provides:
`

func overrideDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunChecksAndWrites(t *testing.T) {
	pantry := gitPantry(t, map[string]string{"acme.org": acmeRecipe})
	dir := overrideDir(t, map[string]string{"acme.org-openssl3.patch": acmePatch})

	var buf bytes.Buffer
	if code := run(dir, pantry, false, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "1 project(s) reproduce the unified diff exactly, 0 do not") {
		t.Errorf("report:\n%s", buf.String())
	}
	// Checking writes nothing.
	if _, err := os.Stat(filepath.Join(dir, "acme.org.hcl")); err == nil {
		t.Error("the check must not write")
	}

	buf.Reset()
	if code := run(dir, pantry, true, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "acme.org.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	// The generated family's real reason, not the filename it came from.
	if !strings.Contains(string(got), "end-of-life since September 2023") {
		t.Errorf("an openssl3 conversion must carry the generator's reason:\n%s", got)
	}
	if !strings.Contains(string(got), `path = "dependencies[\"openssl.org\"]"`) {
		t.Errorf("got:\n%s", got)
	}
}

// A patch that no longer applies must stop the conversion. Converting it would
// carry the rot into the new format wearing a clean face.
func TestRunRefusesToConvertAPatchThatNoLongerApplies(t *testing.T) {
	pantry := gitPantry(t, map[string]string{"acme.org": "dependencies:\n  curl.se: ^8\n"})
	dir := overrideDir(t, map[string]string{"acme.org-openssl3.patch": acmePatch})
	var buf bytes.Buffer
	if code := run(dir, pantry, false, &buf); code != 1 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "no longer apply") {
		t.Errorf("report:\n%s", buf.String())
	}
}

// Nothing compared is not agreement: it is a run that did not happen. Reported
// as a clean pass once already, on a clone that never took place.
func TestRunRefusesAnEmptyComparison(t *testing.T) {
	pantry := gitPantry(t, map[string]string{"acme.org": acmeRecipe})
	var buf bytes.Buffer
	if code := run(t.TempDir(), pantry, false, &buf); code != 2 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "not one project was compared") {
		t.Errorf("report:\n%s", buf.String())
	}
}

func TestRunReportsAnUnusablePantry(t *testing.T) {
	dir := overrideDir(t, map[string]string{"acme.org-openssl3.patch": acmePatch})
	var buf bytes.Buffer
	// Not a git worktree: overrides.Apply cannot reset what it patches.
	if code := run(dir, t.TempDir(), false, &buf); code == 0 {
		t.Errorf("a pantry that is not a worktree must not pass:\n%s", buf.String())
	}
}

// The clone path, without the network.
func TestRunClonesWhenGivenNoPantry(t *testing.T) {
	recipes := map[string]string{"acme.org": acmeRecipe}
	old := cloneCmd
	t.Cleanup(func() { cloneCmd = old })
	cloneCmd = func(dest string) error {
		src := gitPantry(t, recipes)
		return exec.Command("cp", "-R", src+"/.", dest).Run()
	}
	dir := overrideDir(t, map[string]string{"acme.org-openssl3.patch": acmePatch})
	var buf bytes.Buffer
	if code := run(dir, "", false, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}

	cloneCmd = func(string) error { return os.ErrPermission }
	buf.Reset()
	if code := run(dir, "", false, &buf); code != 2 {
		t.Errorf("a failed clone must stop the run: %d\n%s", code, buf.String())
	}
}

func TestSlugAndWhy(t *testing.T) {
	if got := slug("crates.io/semverator"); got != "crates.io-semverator" {
		t.Errorf("slug = %q", got)
	}
	dir := overrideDir(t, map[string]string{
		"acme.org-declare-libidn2.patch": "x",
		"acme.org-second-thing.patch":    "x",
	})
	why := whyFor(dir, "acme.org")
	if !strings.Contains(why, "declare-libidn2") || !strings.Contains(why, "second-thing") {
		t.Errorf("why must name every patch it came from: %q", why)
	}
	if got := whyFor(t.TempDir(), "nothing.org"); !strings.Contains(got, "git history") {
		t.Errorf("why = %q", got)
	}
	if r, ok := knownReason("x-cargo-locked.patch"); !ok || !strings.Contains(r, "Cargo.lock") {
		t.Errorf("knownReason = %q %v", r, ok)
	}
	if _, ok := knownReason("x-whatever.patch"); ok {
		t.Error("an unknown family has no known reason")
	}
}

func TestFirstDifference(t *testing.T) {
	a := map[string]any{"a": map[string]any{"b": 1}}
	b := map[string]any{"a": map[string]any{"b": 2}}
	if got := firstDifference(a, b); !strings.Contains(got, ".a.b") {
		t.Errorf("got %q", got)
	}
	c := map[string]any{"a": map[string]any{}}
	if got := firstDifference(a, c); !strings.Contains(got, "one side only") {
		t.Errorf("got %q", got)
	}
	if got := firstDifference(a, a); got != "" {
		t.Errorf("identical documents differ at %q", got)
	}
	long := strings.Repeat("x", 100)
	if got := trunc(long); len(got) > 64 || !strings.HasSuffix(got, "…") {
		t.Errorf("trunc = %q", got)
	}
}

func TestMainRuns(t *testing.T) {
	pantry := gitPantry(t, map[string]string{"acme.org": acmeRecipe})
	dir := overrideDir(t, map[string]string{"acme.org-openssl3.patch": acmePatch})
	oldArgs, oldExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = oldArgs, oldExit })
	exited := -1
	osExit = func(c int) { exited = c }

	os.Args = []string{"logicalise", "-dir", dir, "-pantry", pantry}
	main()
	if exited != -1 {
		t.Errorf("a clean check must not exit, got %d", exited)
	}
	os.Args = []string{"logicalise", "-dir", t.TempDir(), "-pantry", pantry}
	main()
	if exited != 2 {
		t.Errorf("exit = %d, want 2", exited)
	}
}

// Every way the run can fail before it compares anything. Each is a seam
// rather than a contrived filesystem, which is how the rest of this repository
// tests its error branches.
func TestRunFailsEarly(t *testing.T) {
	pantry := gitPantry(t, map[string]string{"acme.org": acmeRecipe})
	dir := overrideDir(t, map[string]string{"acme.org-openssl3.patch": acmePatch})

	t.Run("no temporary directory", func(t *testing.T) {
		old := osMkdirTemp
		t.Cleanup(func() { osMkdirTemp = old })
		osMkdirTemp = func(string, string) (string, error) { return "", os.ErrPermission }
		var buf bytes.Buffer
		if code := run(dir, "", false, &buf); code != 2 {
			t.Errorf("code = %d\n%s", code, buf.String())
		}
	})

	t.Run("the pantry cannot be copied", func(t *testing.T) {
		old := copyTreeFn
		t.Cleanup(func() { copyTreeFn = old })
		copyTreeFn = func(string) (string, error) { return "", os.ErrPermission }
		var buf bytes.Buffer
		if code := run(dir, pantry, false, &buf); code != 2 {
			t.Errorf("code = %d\n%s", code, buf.String())
		}
	})

	t.Run("the override directory cannot be read", func(t *testing.T) {
		old := overridesApply
		t.Cleanup(func() { overridesApply = old })
		overridesApply = func(overrides.Options) (overrides.Result, error) {
			return overrides.Result{}, os.ErrPermission
		}
		var buf bytes.Buffer
		if code := run(dir, pantry, false, &buf); code != 2 {
			t.Errorf("code = %d\n%s", code, buf.String())
		}
	})

	t.Run("the walk over the patched tree fails", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root walks an unreadable directory")
		}
		old := overridesApply
		t.Cleanup(func() { overridesApply = old })
		// Between applying the patches and walking the result, the tree stops
		// being readable. Reporting nothing found would read as "no overrides
		// here", which is the one answer that must never be inferred.
		overridesApply = func(o overrides.Options) (overrides.Result, error) {
			res, err := overrides.Apply(o)
			if err != nil {
				return res, err
			}
			return res, os.Chmod(filepath.Join(o.Root, "projects"), 0)
		}
		var buf bytes.Buffer
		if code := run(dir, pantry, false, &buf); code != 2 {
			t.Errorf("code = %d\n%s", code, buf.String())
		}
	})

	t.Run("the patched tree cannot be walked", func(t *testing.T) {
		old := copyTreeFn
		t.Cleanup(func() { copyTreeFn = old })
		copyTreeFn = func(src string) (string, error) {
			d, err := copyTree(src)
			if err != nil {
				return "", err
			}
			// A projects/ that is a FILE: the walk fails rather than finding
			// nothing, and finding nothing is what it must not report.
			_ = os.RemoveAll(filepath.Join(d, "projects"))
			return d, os.WriteFile(filepath.Join(d, "projects"), []byte("x"), 0o644)
		}
		var buf bytes.Buffer
		// overrides.Apply will also fail on this tree; either way it must not
		// report a clean run.
		if code := run(dir, pantry, false, &buf); code == 0 {
			t.Errorf("code = 0\n%s", buf.String())
		}
	})
}

// Every way ONE project can fail, with the others still reported.
func TestRunReportsAProjectThatDoesNotReproduce(t *testing.T) {
	pantry := gitPantry(t, map[string]string{"acme.org": acmeRecipe})
	dir := overrideDir(t, map[string]string{"acme.org-openssl3.patch": acmePatch})

	t.Run("the converted file does not parse", func(t *testing.T) {
		old := emitFn
		t.Cleanup(func() { emitFn = old })
		emitFn = func(*logical.Override) []byte { return []byte("project = ") }
		var buf bytes.Buffer
		if code := run(dir, pantry, false, &buf); code != 1 {
			t.Fatalf("code = %d\n%s", code, buf.String())
		}
		if !strings.Contains(buf.String(), "does not parse") {
			t.Errorf("report:\n%s", buf.String())
		}
	})

	t.Run("the operations do not apply", func(t *testing.T) {
		old := deriveFn
		t.Cleanup(func() { deriveFn = old })
		deriveFn = func(_, _ map[string]any, why string) []logical.Op {
			p, _ := logical.ParsePath("build.script")
			return []logical.Op{{Why: why, Path: p, Substitute: true, From: "nowhere", To: "x"}}
		}
		var buf bytes.Buffer
		if code := run(dir, pantry, false, &buf); code != 1 {
			t.Fatalf("code = %d\n%s", code, buf.String())
		}
		if !strings.Contains(buf.String(), "does not contain") {
			t.Errorf("report:\n%s", buf.String())
		}
	})

	t.Run("they apply and give something else", func(t *testing.T) {
		old := deriveFn
		t.Cleanup(func() { deriveFn = old })
		deriveFn = func(_, _ map[string]any, why string) []logical.Op {
			p, _ := logical.ParsePath(`dependencies["openssl.org"]`)
			return []logical.Op{{Why: why, Path: p, Set: "^4"}} // the patch says ^3
		}
		var buf bytes.Buffer
		if code := run(dir, pantry, false, &buf); code != 1 {
			t.Fatalf("code = %d\n%s", code, buf.String())
		}
		if !strings.Contains(buf.String(), "openssl.org") {
			t.Errorf("the report must name the key that differs:\n%s", buf.String())
		}
	})

	t.Run("the file cannot be written", func(t *testing.T) {
		old := osWriteFile
		t.Cleanup(func() { osWriteFile = old })
		osWriteFile = func(string, []byte, os.FileMode) error { return os.ErrPermission }
		var buf bytes.Buffer
		if code := run(dir, pantry, true, &buf); code != 1 {
			t.Fatalf("code = %d\n%s", code, buf.String())
		}
	})
}

// copyTree's own failures, and the sort that orders more than one project.
func TestCopyTreeAndOrdering(t *testing.T) {
	if _, err := copyTree(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("copying a tree that is not there must fail")
	}
	old := osMkdirTemp
	t.Cleanup(func() { osMkdirTemp = old })
	osMkdirTemp = func(string, string) (string, error) { return "", os.ErrPermission }
	if _, err := copyTree(t.TempDir()); err == nil {
		t.Error("no temporary directory must fail")
	}
	osMkdirTemp = old

	// Two projects, so the ordering comparison runs and the report is stable.
	zebraRecipe := strings.ReplaceAll(acmeRecipe, "acme.org", "zebra.org")
	pantry := gitPantry(t, map[string]string{"acme.org": acmeRecipe, "zebra.org": zebraRecipe})
	dir := overrideDir(t, map[string]string{
		"acme.org-openssl3.patch":  acmePatch,
		"zebra.org-openssl3.patch": strings.ReplaceAll(acmePatch, "acme.org", "zebra.org"),
	})
	var buf bytes.Buffer
	if code := run(dir, pantry, false, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "2 project(s) reproduce") {
		t.Errorf("report:\n%s", buf.String())
	}
}

// A project the patches CREATE is not an override and is left out; a recipe
// that is not YAML at all is reported by the loader rather than crashing.
func TestChangedProjectsSkipsAndReports(t *testing.T) {
	pristine := gitPantry(t, map[string]string{"acme.org": acmeRecipe})
	patched, err := copyTree(pristine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(patched) })
	// Created outright.
	newDir := filepath.Join(patched, "projects", "brand.new")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "package.yml"), []byte("build: make\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := changedProjects(pristine, patched)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.project == "brand.new" {
			t.Error("a project the patches CREATE is not an override")
		}
	}

	if _, err := loadDoc(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a missing file must be an error")
	}
	bad := filepath.Join(t.TempDir(), "bad.yml")
	if err := os.WriteFile(bad, []byte("a: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDoc(bad); err == nil {
		t.Error("a file that is not YAML must be an error")
	}
}

func TestDeepCopyKeepsListsOfSteps(t *testing.T) {
	in := map[string]any{"build": map[string]any{
		"script": []any{map[string]any{"run": "make"}, "plain"},
	}}
	out := deepCopy(in)
	step := out["build"].(map[string]any)["script"].([]any)[0].(map[string]any)
	step["run"] = "changed"
	if in["build"].(map[string]any)["script"].([]any)[0].(map[string]any)["run"] != "make" {
		t.Error("deepCopy shared a step with its input")
	}
}

// firstDifference stops at the FIRST, so a second difference must not change
// what it says.
func TestFirstDifferenceStopsAtTheFirst(t *testing.T) {
	a := map[string]any{"a": 1, "b": 1}
	b := map[string]any{"a": 2, "b": 2}
	if got := firstDifference(a, b); !strings.Contains(got, ".a:") {
		t.Errorf("got %q", got)
	}
}

// The clone command itself, against a local repository rather than the network.
func TestCloneCmd(t *testing.T) {
	src := gitPantry(t, map[string]string{"acme.org": acmeRecipe})
	old := pantryURL
	t.Cleanup(func() { pantryURL = old })
	pantryURL = src
	dest := filepath.Join(t.TempDir(), "clone")
	if err := cloneCmd(dest); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "projects", "acme.org", "package.yml")); err != nil {
		t.Errorf("the clone is empty: %v", err)
	}
}

// An override that compounds on a second pass must be refused. 24 real
// operations across 22 projects did exactly that before go-pkgx/bk#229, and
// the migration this tool exists for depends on both formats being applied to
// the same recipe at once.
//
// The fixture is a patch whose RESULT CONTAINS THE ORIGINAL — `make install`
// becoming `make -j4 make install` — because that is the only shape where a
// substitution can both reproduce the patch and survive its own replacement.
func TestRunRefusesAnOverrideThatAppliesTwice(t *testing.T) {
	const recipe = "build:\n  script: make install\nprovides:\n  - bin/acme\n"
	const patch = `diff --git a/projects/acme.org/package.yml b/projects/acme.org/package.yml
index 1111111..2222222 100644
--- a/projects/acme.org/package.yml
+++ b/projects/acme.org/package.yml
@@ -1,4 +1,4 @@
 build:
-  script: make install
+  script: make -j4 make install
 provides:
   - bin/acme
`
	pantry := gitPantry(t, map[string]string{"acme.org": recipe})
	dir := overrideDir(t, map[string]string{"acme.org-duplicate.patch": patch})

	// Derived honestly this is an assignment, because no run survives. Forced
	// to a substitution, it reproduces the patch and then compounds.
	old := deriveFn
	t.Cleanup(func() { deriveFn = old })
	deriveFn = func(_, _ map[string]any, why string) []logical.Op {
		p, _ := logical.ParsePath("build.script")
		return []logical.Op{{Why: why, Path: p, Substitute: true, From: "make", To: "make -j4 make"}}
	}
	var buf bytes.Buffer
	if code := run(dir, pantry, false, &buf); code != 1 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "not redundant") {
		t.Errorf("report:\n%s", buf.String())
	}

	// Left honest, it converts and is idempotent.
	deriveFn = old
	buf.Reset()
	if code := run(dir, pantry, false, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
}

// The other arm: the second pass does not merely re-apply, it FAILS. A
// substitution that DELETES text cannot report "already reads the new way",
// because there is no new way to read — so once the text is gone its premise
// is gone with it.
func TestRunRefusesAnOverrideThatCannotRunTwice(t *testing.T) {
	const recipe = "build:\n  script: make install extra\nprovides:\n  - bin/acme\n"
	const patch = `diff --git a/projects/acme.org/package.yml b/projects/acme.org/package.yml
index 1111111..2222222 100644
--- a/projects/acme.org/package.yml
+++ b/projects/acme.org/package.yml
@@ -1,4 +1,4 @@
 build:
-  script: make install extra
+  script: make install
 provides:
   - bin/acme
`
	pantry := gitPantry(t, map[string]string{"acme.org": recipe})
	dir := overrideDir(t, map[string]string{"acme.org-trim.patch": patch})

	old := deriveFn
	t.Cleanup(func() { deriveFn = old })
	deriveFn = func(_, _ map[string]any, why string) []logical.Op {
		p, _ := logical.ParsePath("build.script")
		return []logical.Op{{Why: why, Path: p, Substitute: true, From: " extra", To: ""}}
	}
	var buf bytes.Buffer
	if code := run(dir, pantry, false, &buf); code != 1 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "applying it twice") {
		t.Errorf("report:\n%s", buf.String())
	}
}

func TestNotIdempotentOnNothing(t *testing.T) {
	if why, ok := notIdempotent(nil, map[string]any{"a": "x"}); !ok {
		t.Errorf("no operations move nothing: %q", why)
	}
}
