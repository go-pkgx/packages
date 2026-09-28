package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func overlayTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for proj, body := range files {
		p := filepath.Join(dir, "projects", proj, "package.hcl")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func pantryTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for proj, body := range files {
		p := filepath.Join(dir, "projects", proj, "package.yml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const upstreamAcme = `distributable:
  url: https://acme.org/{{version}}.tar.gz
dependencies:
  openssl.org: ^1.1
build:
  script: make install
provides:
  - bin/acme
`

// The whole point: a block upstream already states goes, a block that differs
// stays, and merging the remainder over upstream gives what the full copy gave.
func TestReduceKeepsOnlyWhatDiffers(t *testing.T) {
	overlay := overlayTree(t, map[string]string{"acme.org": `distributable {
  url = "https://acme.org/{{version}}.tar.gz"
}
dependencies = {
  "openssl.org" = "^3"
}
build {
  script = "make install"
}
provides = ["bin/acme"]
`})
	pantry := pantryTree(t, map[string]string{"acme.org": upstreamAcme})

	var buf bytes.Buffer
	if code := run(overlay, pantry, true, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	got, err := os.ReadFile(filepath.Join(overlay, "projects", "acme.org", "package.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "openssl.org") || !strings.Contains(s, `"^3"`) {
		t.Errorf("the delta was dropped:\n%s", s)
	}
	for _, gone := range []string{"distributable", "provides", "script"} {
		if strings.Contains(s, gone) {
			t.Errorf("%s is identical upstream and should not be restated:\n%s", gone, s)
		}
	}
	if !strings.Contains(buf.String(), "1 reduced") {
		t.Errorf("report:\n%s", buf.String())
	}
}

// A comment belongs to the item hclwrite removes, so a block carrying one is
// kept whole. Measured, not assumed.
func TestReduceKeepsABlockThatCarriesAComment(t *testing.T) {
	overlay := overlayTree(t, map[string]string{"acme.org": `# why we restate this, in prose worth more than the bytes
distributable {
  url = "https://acme.org/{{version}}.tar.gz"
}
dependencies = {
  "openssl.org" = "^3"
}
`})
	pantry := pantryTree(t, map[string]string{"acme.org": upstreamAcme})

	var buf bytes.Buffer
	if code := run(overlay, pantry, true, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	got, _ := os.ReadFile(filepath.Join(overlay, "projects", "acme.org", "package.hcl"))
	if !strings.Contains(string(got), "worth more than the bytes") {
		t.Errorf("the comment was lost:\n%s", got)
	}
	if !strings.Contains(string(got), "distributable") {
		t.Errorf("the block its comment belongs to was dropped anyway:\n%s", got)
	}
	if !strings.Contains(buf.String(), "1 block(s) kept because a comment") {
		t.Errorf("report:\n%s", buf.String())
	}
}

// A `#` inside a string is a shebang, not a comment. Reading it as one keeps
// blocks nobody needs to keep — perl.org's script has
// `sed -i.bak 's|^#!{{prefix}}/bin/|…` and the line-based version of this
// check called it a comment.
func TestAHashInsideAStringIsNotAComment(t *testing.T) {
	src := []byte("build {\n  script = \"sed -i 's|^#!/bin/sh|x|'\"\n}\n")
	if got := comments(src); len(got) != 0 {
		t.Errorf("comments = %q", got)
	}
	if got := comments([]byte("# real\na = 1\n")); len(got) != 1 {
		t.Errorf("comments = %q", got)
	}
	// Something that does not parse has no comments to report, rather than a
	// panic.
	if got := comments([]byte("build {")); got != nil {
		t.Errorf("comments = %q", got)
	}
}

// A project upstream does not carry is left exactly alone: the overlay is its
// whole recipe and there is nothing to reduce against.
func TestReduceLeavesOursAlone(t *testing.T) {
	body := "distributable {\n  url = \"https://ours.example/x.tar.gz\"\n}\n"
	overlay := overlayTree(t, map[string]string{"ours.example": body})
	pantry := pantryTree(t, map[string]string{})
	var buf bytes.Buffer
	if code := run(overlay, pantry, true, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	got, _ := os.ReadFile(filepath.Join(overlay, "projects", "ours.example", "package.hcl"))
	if string(got) != body {
		t.Errorf("it was rewritten:\n%s", got)
	}
}

// Checking writes nothing.
func TestReduceWithoutWriteChangesNoFile(t *testing.T) {
	body := "distributable {\n  url = \"https://acme.org/{{version}}.tar.gz\"\n}\ndependencies = { \"openssl.org\" = \"^3\" }\n"
	overlay := overlayTree(t, map[string]string{"acme.org": body})
	pantry := pantryTree(t, map[string]string{"acme.org": upstreamAcme})
	var buf bytes.Buffer
	if code := run(overlay, pantry, false, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	got, _ := os.ReadFile(filepath.Join(overlay, "projects", "acme.org", "package.hcl"))
	if string(got) != body {
		t.Errorf("the check wrote:\n%s", got)
	}
}

// Nothing to read is a refusal, not a clean run — the shape that reported a
// clean pass on a clone that never happened.
func TestReduceRefusesAnEmptyOverlay(t *testing.T) {
	// A projects/ with nothing in it: the walk succeeds and finds no recipe.
	empty := t.TempDir()
	if err := os.MkdirAll(filepath.Join(empty, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := run(empty, t.TempDir(), false, &buf); code != 2 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "is that the overlay?") {
		t.Errorf("report:\n%s", buf.String())
	}
	// No projects/ at all: the walk itself fails, and that is a different
	// sentence for a different mistake.
	buf.Reset()
	if code := run(t.TempDir(), t.TempDir(), false, &buf); code != 2 {
		t.Errorf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "no such file") {
		t.Errorf("report:\n%s", buf.String())
	}
}

// Every way one entry can fail, and the run reports it without stopping.
func TestRunReportsPerEntryFailures(t *testing.T) {
	pantry := pantryTree(t, map[string]string{"acme.org": upstreamAcme})

	t.Run("the entry does not parse", func(t *testing.T) {
		overlay := overlayTree(t, map[string]string{"acme.org": "distributable {"})
		var buf bytes.Buffer
		if code := run(overlay, pantry, false, &buf); code != 1 {
			t.Fatalf("code = %d\n%s", code, buf.String())
		}
	})

	t.Run("the entry cannot be read", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads a 0000 file")
		}
		overlay := overlayTree(t, map[string]string{"acme.org": "a = 1\n"})
		if err := os.Chmod(filepath.Join(overlay, "projects", "acme.org", "package.hcl"), 0); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if code := run(overlay, pantry, false, &buf); code != 1 {
			t.Fatalf("code = %d\n%s", code, buf.String())
		}
	})

	t.Run("reducing would change what a consumer resolves", func(t *testing.T) {
		overlay := overlayTree(t, map[string]string{"acme.org": `distributable {
  url = "https://acme.org/{{version}}.tar.gz"
}
dependencies = { "openssl.org" = "^3" }
`})
		old := sameAfterFn
		t.Cleanup(func() { sameAfterFn = old })
		sameAfterFn = func([]byte, []byte, string, map[string]any) (bool, error) { return false, nil }
		var buf bytes.Buffer
		if code := run(overlay, pantry, true, &buf); code != 1 {
			t.Fatalf("code = %d\n%s", code, buf.String())
		}
		if !strings.Contains(buf.String(), "changes what a consumer resolves") {
			t.Errorf("report:\n%s", buf.String())
		}
	})

	t.Run("the file cannot be written", func(t *testing.T) {
		overlay := overlayTree(t, map[string]string{"acme.org": `distributable {
  url = "https://acme.org/{{version}}.tar.gz"
}
dependencies = { "openssl.org" = "^3" }
`})
		old := osWriteFile
		t.Cleanup(func() { osWriteFile = old })
		osWriteFile = func(string, []byte, os.FileMode) error { return os.ErrPermission }
		var buf bytes.Buffer
		if code := run(overlay, pantry, true, &buf); code != 1 {
			t.Fatalf("code = %d\n%s", code, buf.String())
		}
	})

	t.Run("a comment goes missing", func(t *testing.T) {
		overlay := overlayTree(t, map[string]string{"acme.org": "# keep me\ndependencies = { \"openssl.org\" = \"^3\" }\n"})
		old := reduceFileFn
		t.Cleanup(func() { reduceFileFn = old })
		reduceFileFn = func([]byte, string, map[string]any) ([]byte, int, error) {
			return []byte("dependencies = { \"openssl.org\" = \"^3\" }\n"), 0, nil
		}
		var buf bytes.Buffer
		if code := run(overlay, pantry, false, &buf); code != 1 {
			t.Fatalf("code = %d\n%s", code, buf.String())
		}
		if !strings.Contains(buf.String(), "went missing — refusing") {
			t.Errorf("report:\n%s", buf.String())
		}
	})
}

// removeTop, hclDoc, mergeOver and tidy, at their corners.
func TestTheEditingPrimitives(t *testing.T) {
	// A key that is neither a block nor an attribute.
	if got, ok := removeTop([]byte("a = 1\n"), "t.hcl", "absent"); ok || got != nil {
		t.Errorf("got %q %v", got, ok)
	}
	// A file that does not parse and a key that is not there both mean
	// "nothing to remove" — reduceFile has already reported the parse failure.
	if _, ok := removeTop([]byte("build {"), "t.hcl", "build"); ok {
		t.Error("a file that does not parse has nothing to remove")
	}
	// A LABELLED block is not the same key: `foo "bar" {}` is not `foo`.
	if got, ok := removeTop([]byte("foo \"bar\" {\n}\n"), "t.hcl", "foo"); ok || got != nil {
		t.Errorf("a labelled block must not be mistaken for the key: %q", got)
	}

	if _, err := hclDoc([]byte("a: [\n"), "t.yml"); err == nil {
		t.Error("YAML that does not parse must be an error")
	}
	if _, err := hclDoc([]byte("a = "), "t.hcl"); err == nil {
		t.Error("HCL that does not parse must be an error")
	}

	// mergeOver: a nested map merges, everything else replaces.
	got2 := mergeOver(
		map[string]any{"a": map[string]any{"x": 1, "y": 2}, "b": "keep"},
		map[string]any{"a": map[string]any{"y": 3}, "c": "new"})
	inner := got2["a"].(map[string]any)
	if inner["x"] != 1 || inner["y"] != 3 || got2["b"] != "keep" || got2["c"] != "new" {
		t.Errorf("got %v", got2)
	}
	// A map over a scalar replaces it.
	if _, ok := mergeOver(map[string]any{"a": "s"}, map[string]any{"a": map[string]any{"b": 1}})["a"].(map[string]any); !ok {
		t.Error("a map must replace a scalar")
	}

	if tidy([]byte("\n\n\n")) != nil {
		t.Error("a file with nothing but blank lines reduces to nothing")
	}
	if got := string(tidy([]byte("a = 1\n\n\n\nb = 2\n"))); got != "a = 1\n\nb = 2\n" {
		t.Errorf("tidy = %q", got)
	}
}

func TestSameAfterMergeReportsUnreadable(t *testing.T) {
	if _, err := sameAfterMerge([]byte("a = "), []byte("a = 1\n"), "t.hcl", nil); err == nil {
		t.Error("an unreadable original must be an error")
	}
	if _, err := sameAfterMerge([]byte("a = 1\n"), []byte("a = "), "t.hcl", nil); err == nil {
		t.Error("an unreadable result must be an error")
	}
}

func TestMainRuns(t *testing.T) {
	overlay := overlayTree(t, map[string]string{"acme.org": "dependencies = { \"openssl.org\" = \"^3\" }\n"})
	pantry := pantryTree(t, map[string]string{"acme.org": upstreamAcme})
	oldArgs, oldExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = oldArgs, oldExit })
	exited := -1
	osExit = func(c int) { exited = c }

	os.Args = []string{"reduce", "-overlay", overlay, "-pantry", pantry}
	main()
	if exited != -1 {
		t.Errorf("a clean run must not exit, got %d", exited)
	}
	os.Args = []string{"reduce"}
	main()
	if exited != 2 {
		t.Errorf("exit = %d, want 2 — without an overlay there is nothing to compare", exited)
	}
	// And an overlay that holds nothing: run itself refuses, through main.
	exited = -1
	os.Args = []string{"reduce", "-overlay", t.TempDir(), "-pantry", pantry}
	main()
	if exited != 2 {
		t.Errorf("exit = %d, want 2", exited)
	}
}

// The walk: two projects so the ordering comparison runs, a non-recipe file
// beside them, and a stray file at the root of projects/ which is not a
// project at all.
func TestOverlayProjectsIgnoresWhatIsNotARecipe(t *testing.T) {
	overlay := overlayTree(t, map[string]string{"b.org": "a = 1\n", "a.org": "a = 1\n"})
	for _, extra := range []string{"projects/README.md", "projects/a.org/NOTES.md", "projects/package.hcl"} {
		if err := os.WriteFile(filepath.Join(overlay, extra), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := overlayProjects(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].project != "a.org" || got[1].project != "b.org" {
		t.Errorf("got %+v", got)
	}
}

// An upstream recipe that is not YAML is not a baseline, and the entry is left
// alone rather than reduced against nonsense.
func TestUpstreamDocRefusesWhatIsNotYAML(t *testing.T) {
	pantry := pantryTree(t, map[string]string{"acme.org": "a: [\n"})
	if _, err := upstreamDoc(pantry, "acme.org"); err == nil {
		t.Error("a pantry recipe that does not parse must be an error")
	}
	overlay := overlayTree(t, map[string]string{"acme.org": "dependencies = { \"openssl.org\" = \"^3\" }\n"})
	var buf bytes.Buffer
	if code := run(overlay, pantry, true, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	got, _ := os.ReadFile(filepath.Join(overlay, "projects", "acme.org", "package.hcl"))
	if !strings.Contains(string(got), "openssl.org") {
		t.Errorf("it was reduced against a baseline that does not parse:\n%s", got)
	}
}

// reduceFile's own error and no-op arms: an entry that does not parse, and a
// key present in neither form.
func TestReduceFileCorners(t *testing.T) {
	if _, _, err := reduceFile([]byte("a = "), "t.hcl", nil); err == nil {
		t.Error("an entry that does not parse must be an error")
	}
	// A key the document reports but hclwrite finds as neither a block nor an
	// attribute cannot happen from a parsed file, so the no-op arm is reached
	// through removeTop directly — see TestTheEditingPrimitives. Here: a key
	// upstream does not state is simply kept.
	out, kept, err := reduceFile([]byte("only = \"ours\"\n"), "t.hcl", map[string]any{})
	if err != nil || kept != 0 || !strings.Contains(string(out), "only") {
		t.Errorf("out=%q kept=%d err=%v", out, kept, err)
	}
}

// lostAComment counts, so a file that gains a comment is not mistaken for one
// that kept them all, and one that loses the second of two duplicates is
// caught.
func TestLostACommentCounts(t *testing.T) {
	two := []byte("# same\na = 1\n# same\nb = 2\n")
	one := []byte("# same\na = 1\n")
	if !lostAComment(two, one) {
		t.Error("losing one of two identical comments must be noticed")
	}
	if lostAComment(one, two) {
		t.Error("gaining a comment is not losing one")
	}
}

// A LABELLED block reads as a key in the document but is not a top-level key
// hclwrite can remove by name, so reduceFile leaves it. Rare in a recipe, and
// the alternative — removing the first block that merely shares the type —
// would delete something nobody asked it to.
func TestReduceLeavesALabelledBlock(t *testing.T) {
	src := []byte("foo \"bar\" {\n  x = 1\n}\n")
	doc, err := hclDoc(src, "t.hcl")
	if err != nil {
		t.Skipf("the converter does not read a labelled block: %v", err)
	}
	out, kept, err := reduceFile(src, "t.hcl", doc) // identical to "upstream"
	if err != nil {
		t.Fatal(err)
	}
	if kept != 0 || !strings.Contains(string(out), "foo") {
		t.Errorf("out=%q kept=%d", out, kept)
	}
}
