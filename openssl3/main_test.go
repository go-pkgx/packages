package main

import (
	"bytes"
	"github.com/go-pkgx/bk/logical"
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

// The override names the KEY, wherever the pin sits. There is no line to
// preserve and no indentation to get right, because the file is not touched:
// the correction is applied to the recipe's document as it is read.
func TestOverrideNamesEveryPinnedPath(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "a.org", `dependencies:
  openssl.org: '^1.1'
  zlib.net: ^1
build:
  dependencies:
    openssl.org: ^1.1
    linux:
      openssl.org: 1.1
`)
	src, err := overrideFor(p, "a.org")
	if err != nil {
		t.Fatal(err)
	}
	o, err := logical.Parse(src, "a.hcl")
	if err != nil {
		t.Fatalf("the generator wrote something that does not parse: %v\n%s", err, src)
	}
	want := map[string]bool{
		`dependencies["openssl.org"]`:             false,
		`build.dependencies["openssl.org"]`:       false,
		`build.dependencies.linux["openssl.org"]`: false,
	}
	for _, op := range o.Ops {
		k := op.Path.String()
		if _, ok := want[k]; !ok {
			t.Errorf("touched a path nobody asked for: %s", k)
			continue
		}
		if op.Set != "^3" {
			t.Errorf("%s set to %v", k, op.Set)
		}
		want[k] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("%s was not retargeted", k)
		}
	}
	// zlib's own ^1 is a different project.
	if strings.Contains(string(src), "zlib.net") {
		t.Errorf("touched an unrelated dependency:\n%s", src)
	}
	// And the reason is in the file, with the condition for deleting it.
	if !strings.Contains(o.Why, "end-of-life") || !strings.Contains(o.Why, "delete it when") {
		t.Errorf("why = %q", o.Why)
	}
}

// YAML reads an unquoted 1.1 as a float, and amp.rs writes it that way.
// Reading only strings missed it, and the conversion said so:
// `string(^3) became float64(1.1)`.
func TestAnUnquotedPinIsStillAPin(t *testing.T) {
	for _, v := range []string{"1.1", "'^1.1'", "^1", "'>=1.1<2'", "1"} {
		p := t.TempDir()
		writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: "+v+"\n")
		if _, err := overrideFor(p, "a.org"); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
	// And what is NOT a 1.x pin is left alone.
	for _, v := range []string{"'^3'", "3", "'*'", "^4"} {
		p := t.TempDir()
		writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: "+v+"\n")
		if _, err := overrideFor(p, "a.org"); err == nil {
			t.Errorf("%s is not a stale pin", v)
		}
	}
	// A value that is neither text nor a number.
	p := t.TempDir()
	writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: [1.1]\n")
	if _, err := overrideFor(p, "a.org"); err == nil {
		t.Error("a list is not a constraint")
	}
}

// A project can need an override for more than one reason, and its operations
// live in ONE file so they are read together — so a generator cannot own such
// a file. It checks instead.
func TestCarriesThePins(t *testing.T) {
	dir := t.TempDir()
	pathFor := func(name, body string) string {
		f := filepath.Join(dir, name)
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return f
	}
	pins := []logical.Path{{"dependencies", "openssl.org"}}

	ok, err := carriesThePins(pathFor("has.hcl", `
project = "a.org"
why     = "somebody's own reason"
edits   = [
  { path = "build.script", from = "make", to = "gmake" },
  { path = "dependencies[\"openssl.org\"]", set = "^3" },
]
`), pins)
	if err != nil || !ok {
		t.Errorf("ok=%v err=%v", ok, err)
	}

	ok, err = carriesThePins(pathFor("lacks.hcl", `
project = "a.org"
why     = "somebody's own reason"
edits   = [{ path = "build.script", from = "make", to = "gmake" }]
`), pins)
	if err != nil || ok {
		t.Errorf("ok=%v err=%v", ok, err)
	}

	if _, err := carriesThePins(filepath.Join(dir, "absent.hcl"), pins); err == nil {
		t.Error("a missing file must be an error, not a silent no")
	}
	if _, err := carriesThePins(pathFor("bad.hcl", "project = "), pins); err == nil {
		t.Error("a file that does not parse must be an error")
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
	if _, err := overrideFor(p, "clean.org"); err == nil {
		t.Error("expected an error for a recipe with no stale pin")
	}
	// nor is one that does not exist
	if _, err := overrideFor(p, "absent.org"); err == nil {
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
	for _, want := range []string{"a.org.hcl", "nested-b.org.hcl"} {
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
	old := overrideOne
	overrideOne = func(string, string) ([]byte, error) { return nil, errBoom }
	defer func() { overrideOne = old }()

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

// TestOverlayMode: the install side. pkgx does not read overrides/ — it fetches
// the recipe itself — so the corrected recipe has to exist WHOLE in the overlay
// or every consumer of those packages still fails to resolve.
func TestOverlayMode(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "nested/b.org", "dependencies:\n  openssl.org: ^1.1\n  zlib.net: ^1\nbuild:\n  dependencies:\n    openssl.org: '^1'\n")
	out, ovl := filepath.Join(p, "overrides"), filepath.Join(p, "overlay", "projects")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if code := run([]string{"-pantry", p, "-overrides", out, "-overlay", ovl}, null, null); code != 0 {
		t.Fatalf("code = %d", code)
	}
	got, err := os.ReadFile(filepath.Join(ovl, "nested/b.org", "package.yml"))
	if err != nil {
		t.Fatalf("overlay recipe not written: %v", err)
	}
	want := "dependencies:\n  openssl.org: ^3\n  zlib.net: ^1\nbuild:\n  dependencies:\n    openssl.org: '^3'\n"
	if string(got) != want {
		t.Errorf("overlay recipe =\n%q\nwant\n%q", got, want)
	}
	// without -overlay nothing is written there
	ovl2 := filepath.Join(p, "overlay2")
	if code := run([]string{"-pantry", p, "-overrides", out}, null, null); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if _, err := os.Stat(ovl2); err == nil {
		t.Error("overlay written without -overlay")
	}
}

// A recipe that vanishes between listing and writing is an error, not a silent
// half-fix: the build side would be corrected and the install side would not.
func TestWriteOverlayMissingRecipe(t *testing.T) {
	if err := writeOverlay(t.TempDir(), t.TempDir(), "absent.org"); err == nil {
		t.Error("expected an error for a missing recipe")
	}
}

// An overlay directory that cannot be created is an error, not a silent
// half-fix that corrects the build side and leaves the install side broken.
func TestOverlayUnwritable(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: ^1.1\n")
	out := filepath.Join(p, "overrides")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	// a regular file where the overlay tree should go: MkdirAll cannot proceed
	blocker := filepath.Join(p, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if code := run([]string{"-pantry", p, "-overrides", out, "-overlay", blocker}, null, null); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	// and so is a recipe that cannot be read back
	file := filepath.Join(p, "projects", "a.org", "package.yml")
	if err := os.Chmod(file, 0o000); err != nil {
		t.Skip("cannot drop read permission here")
	}
	defer os.Chmod(file, 0o644)
	if err := writeOverlay(p, filepath.Join(p, "ovl"), "a.org"); err == nil {
		t.Error("expected an error for an unreadable recipe")
	}
}

// TestOverlayKeepsAHandWrittenEntry: regeneration must not trade a hand-written
// explanation for nothing. curl.se's overlay entry says WHY its openssl is ^3
// rather than '*'; an entry that is already correct is left untouched.
func TestOverlayKeepsAHandWrittenEntry(t *testing.T) {
	p, ovl := t.TempDir(), t.TempDir()
	writeRecipe(t, p, "a.org", "dependencies:\n  openssl.org: ^1.1\n")
	annotated := "dependencies:\n  # ^3 because the published bottle links libssl.so.3\n  openssl.org: ^3\n"
	if err := os.MkdirAll(filepath.Join(ovl, "a.org"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ovl, "a.org", "package.yml"), []byte(annotated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeOverlay(p, ovl, "a.org"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(ovl, "a.org", "package.yml"))
	if string(got) != annotated {
		t.Errorf("a corrected entry was overwritten:\n%s", got)
	}
	// but one that still carries a stale pin IS rewritten (upstream drifted back)
	if err := os.WriteFile(filepath.Join(ovl, "a.org", "package.yml"), []byte("dependencies:\n  openssl.org: ^1.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeOverlay(p, ovl, "a.org"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(ovl, "a.org", "package.yml")); strings.Contains(string(got), "^1.1") {
		t.Errorf("a stale entry was not refreshed:\n%s", got)
	}
}

// A project whose override already exists is CHECKED, not overwritten: it may
// hold somebody else's work, and a project's operations live in one file so
// they are read together.
func TestRunChecksAnOverrideItDoesNotOwn(t *testing.T) {
	p := t.TempDir()
	writeRecipe(t, p, "shared.org", "dependencies:\n  openssl.org: ^1.1\n")
	writeRecipe(t, p, "mine.org", "dependencies:\n  openssl.org: ^1.1\n")
	out := filepath.Join(p, "overrides")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	// Hand-written, carrying another correction and NOT the pin.
	handWritten := `
project = "shared.org"
why     = "it needs gmake, and somebody wrote that down"
edits   = [{ path = "build.script", from = "make", to = "gmake" }]
`
	if err := os.WriteFile(filepath.Join(out, "shared.org.hcl"), []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}

	var outBuf, errBuf bytes.Buffer
	code := run([]string{"-pantry", p, "-overrides", out}, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("code = %d\n%s", code, errBuf.String())
	}
	// Untouched.
	got, err := os.ReadFile(filepath.Join(out, "shared.org.hcl"))
	if err != nil || string(got) != handWritten {
		t.Errorf("the hand-written override was rewritten:\n%s", got)
	}
	// And said so, loudly enough to act on.
	if !strings.Contains(errBuf.String(), "does NOT carry the pin") {
		t.Errorf("stderr:\n%s", errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "must be edited by hand") {
		t.Errorf("stdout:\n%s", outBuf.String())
	}
	// The one it owns was written.
	if _, err := os.Stat(filepath.Join(out, "mine.org.hcl")); err != nil {
		t.Errorf("the project it owns was not written: %v", err)
	}

	// Now give the shared one the pin as well: nothing more to say.
	withPin := handWritten + "\n"
	withPin = strings.Replace(handWritten,
		`edits   = [{ path = "build.script", from = "make", to = "gmake" }]`,
		"edits   = [\n  { path = \"build.script\", from = \"make\", to = \"gmake\" },\n  { path = \"dependencies[\\\"openssl.org\\\"]\", set = \"^3\" },\n]", 1)
	if err := os.WriteFile(filepath.Join(out, "shared.org.hcl"), []byte(withPin), 0o644); err != nil {
		t.Fatal(err)
	}
	outBuf.Reset()
	errBuf.Reset()
	if code := run([]string{"-pantry", p, "-overrides", out}, &outBuf, &errBuf); code != 0 {
		t.Fatalf("code = %d\n%s", code, errBuf.String())
	}
	if strings.Contains(errBuf.String(), "does NOT carry") {
		t.Errorf("an override that carries the pin must be silent:\n%s", errBuf.String())
	}
}

// A recipe the generator cannot read at all is skipped and reported, never
// fatal: one unreadable project must not stop the other 133.
func TestOverrideForOnAnUnreadableRecipe(t *testing.T) {
	p := t.TempDir()
	if _, err := overrideFor(p, "absent.org"); err == nil {
		t.Error("a missing recipe must be an error")
	}
	writeRecipe(t, p, "bad.org", "a: [\n")
	if _, err := overrideFor(p, "bad.org"); err == nil {
		t.Error("a recipe that is not YAML must be an error")
	}
}
