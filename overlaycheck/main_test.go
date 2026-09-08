package main

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const newPatch = `diff --git a/projects/acme.org/tool/package.yml b/projects/acme.org/tool/package.yml
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/projects/acme.org/tool/package.yml
@@ -0,0 +1,3 @@
+distributable:
+  url: https://acme.org/tool-{{version}}.tar.gz
+display-name: tool
`

// A patch that MODIFIES an existing recipe: its + lines are a fragment, and
// treating them as a whole file would compare a few lines against a real one
// and call every such project drifted.
const editPatch = `diff --git a/projects/gnu.org/sed/package.yml b/projects/gnu.org/sed/package.yml
index 1111111..2222222 100644
--- a/projects/gnu.org/sed/package.yml
+++ b/projects/gnu.org/sed/package.yml
@@ -3,3 +3,4 @@ dependencies:
   zlib.net: '*'
+  gnu.org/gettext: '*'
`

// A new file that is not a recipe — patches carry sibling props too.
const propPatch = `diff --git a/projects/acme.org/tool/fix.diff b/projects/acme.org/tool/fix.diff
new file mode 100644
--- /dev/null
+++ b/projects/acme.org/tool/fix.diff
@@ -0,0 +1,1 @@
+not a recipe
`

func writePatches(t *testing.T, m map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range m {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A non-patch file and a directory must both be ignored.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.patch"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAddedRecipes(t *testing.T) {
	got := addedRecipes(newPatch + editPatch + propPatch)
	if len(got) != 1 {
		t.Fatalf("added = %v, want only the created recipe", got)
	}
	want := "distributable:\n  url: https://acme.org/tool-{{version}}.tar.gz\ndisplay-name: tool\n"
	if string(got["acme.org/tool"]) != want {
		t.Errorf("content = %q, want %q", got["acme.org/tool"], want)
	}
}

func TestProjectOf(t *testing.T) {
	for path, want := range map[string]string{
		"projects/acme.org/tool/package.yml": "acme.org/tool",
		"projects/acme.org/tool/fix.diff":    "",
		"elsewhere/package.yml":              "",
	} {
		if got := projectOf(path); got != want {
			t.Errorf("projectOf(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestSameRecipe(t *testing.T) {
	if !sameRecipe([]byte("a: 1\n"), []byte("a: 1")) {
		t.Error("a missing final newline is not drift")
	}
	if sameRecipe([]byte("a: 1\n"), []byte("a: 2\n")) {
		t.Error("different content is drift")
	}
}

func TestRunAgreement(t *testing.T) {
	dir := writePatches(t, map[string]string{"acme-new.patch": newPatch})
	old := httpGet
	defer func() { httpGet = old }()
	httpGet = func(url string) (int, []byte, error) {
		if !strings.HasSuffix(url, "/projects/acme.org/tool/package.yml") {
			t.Errorf("unexpected url %q", url)
		}
		return 200, []byte("distributable:\n  url: https://acme.org/tool-{{version}}.tar.gz\ndisplay-name: tool\n"), nil
	}
	var out bytes.Buffer
	if code := run(dir, &out); code != 0 {
		t.Fatalf("code = %d, out = %s", code, out.String())
	}
	if !strings.Contains(out.String(), "✓ acme.org/tool") {
		t.Errorf("out = %s", out.String())
	}
}

func TestRunDisagreements(t *testing.T) {
	dir := writePatches(t, map[string]string{"acme-new.patch": newPatch})
	old := httpGet
	defer func() { httpGet = old }()

	for _, tc := range []struct {
		name string
		get  func(string) (int, []byte, error)
		want string
	}{
		{"absent", func(string) (int, []byte, error) { return http.StatusNotFound, nil, nil },
			"absent from the overlay"},
		{"server error", func(string) (int, []byte, error) { return 500, nil, nil },
			"overlay answered 500"},
		{"transport", func(string) (int, []byte, error) { return 0, nil, errors.New("boom") },
			"cannot read the overlay: boom"},
		{"drifted", func(string) (int, []byte, error) { return 200, []byte("something else\n"), nil },
			"halves have drifted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			httpGet = tc.get
			var out bytes.Buffer
			if code := run(dir, &out); code != 1 {
				t.Fatalf("code = %d, want 1", code)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("out = %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestRunUnreadableDir(t *testing.T) {
	var out bytes.Buffer
	if code := run(filepath.Join(t.TempDir(), "absent"), &out); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(out.String(), "overlaycheck:") {
		t.Errorf("out = %q", out.String())
	}
}

// A patch file that cannot be read is a hard failure, not a project silently
// left unchecked — the whole value of this tool is that it does not skip.
func TestRunUnreadablePatch(t *testing.T) {
	dir := writePatches(t, map[string]string{"acme-new.patch": newPatch})
	p := filepath.Join(dir, "unreadable.patch")
	if err := os.WriteFile(p, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	var out bytes.Buffer
	if code := run(dir, &out); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

// The default seam must be the real one; exercising it without a network is
// what a bad address is for.
func TestHTTPGetIsWired(t *testing.T) {
	if _, _, err := httpGet("http://127.0.0.1:1/nothing"); err == nil {
		t.Error("want a transport error from a closed port")
	}
}

func TestMainRuns(t *testing.T) {
	dir := writePatches(t, map[string]string{"acme-new.patch": newPatch})
	old, oldArgs, oldExit := httpGet, os.Args, osExit
	defer func() { httpGet, os.Args, osExit = old, oldArgs, oldExit }()
	exited := -1
	osExit = func(c int) { exited = c }

	httpGet = func(string) (int, []byte, error) {
		return 200, []byte("distributable:\n  url: https://acme.org/tool-{{version}}.tar.gz\ndisplay-name: tool\n"), nil
	}
	os.Args = []string{"overlaycheck", dir}
	main()
	if exited != -1 {
		t.Errorf("agreement must not exit, got %d", exited)
	}

	// A disagreement exits non-zero, which is what makes this a CI gate rather
	// than a report nobody reads.
	httpGet = func(string) (int, []byte, error) { return http.StatusNotFound, nil, nil }
	main()
	if exited != 1 {
		t.Errorf("exit = %d, want 1", exited)
	}

	// With no argument it reads ./overrides — the layout of this repository,
	// which is the only way it is ever invoked in CI.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "overrides"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"overlaycheck"}
	exited = -1
	main()
	if exited != -1 {
		t.Errorf("an empty overrides dir is not a failure, got %d", exited)
	}
}
