package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
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

// The two halves are in different FORMATS — the overlay is HCL, the build
// patch stays YAML because it applies to an upstream clone — so drift is a
// question about what each says, not about the bytes.
func TestSameRecipe(t *testing.T) {
	yaml := []byte("dependencies:\n  openssl.org: ^3\nprovides:\n  - bin/x\n")
	hcl := []byte("dependencies = { \"openssl.org\" = \"^3\" }\nprovides = [\"bin/x\"]\n")
	if same, err := sameRecipe(hcl, "package.hcl", yaml); err != nil || !same {
		t.Errorf("the same recipe in two formats is not drift: %v, %v", same, err)
	}
	// And a real disagreement still is.
	other := []byte("dependencies = { \"openssl.org\" = \"^1\" }\nprovides = [\"bin/x\"]\n")
	if same, err := sameRecipe(other, "package.hcl", yaml); err != nil || same {
		t.Errorf("a different constraint is drift: %v, %v", same, err)
	}
	// Formatting is not drift — which the byte comparison this replaces could
	// not say.
	spaced := []byte("provides = [\"bin/x\"]\n\ndependencies = {\n  \"openssl.org\" = \"^3\"\n}\n")
	if same, err := sameRecipe(spaced, "package.hcl", yaml); err != nil || !same {
		t.Errorf("reordering and whitespace are not drift: %v, %v", same, err)
	}
	// A YAML overlay still works, for a project the flip has not reached.
	if same, err := sameRecipe(yaml, "package.yml", yaml); err != nil || !same {
		t.Errorf("yaml against yaml: %v, %v", same, err)
	}
	// A half that cannot be read is NOT "no drift": saying so would pass a
	// project whose recipe no consumer can parse.
	if _, err := sameRecipe([]byte("build { script = \n"), "package.hcl", yaml); err == nil {
		t.Error("an unreadable overlay half must be an error")
	}
	if _, err := sameRecipe(hcl, "package.hcl", []byte("\tnot yaml\n")); err == nil {
		t.Error("an unreadable patch half must be an error")
	}
}

// The overlay is asked for BOTH names, in the order a consumer tries them.
// Asking for package.yml alone reported all eight of this tool's projects
// "absent from the overlay" the day those recipes became package.hcl — every
// one of them was there.
func TestFetchOverlayRecipeTriesBothNames(t *testing.T) {
	saved := httpGet
	t.Cleanup(func() { httpGet = saved })

	var asked []string
	httpGet = func(url string) (int, []byte, error) {
		asked = append(asked, url)
		if strings.Contains(url, "package.hcl") {
			return 200, []byte("x = 1\n"), nil
		}
		return 404, nil, nil
	}
	name, status, _, err := fetchOverlayRecipe("openucx.org")
	if err != nil || status != 200 || name != "package.hcl" {
		t.Errorf("hcl first: %q %d %v", name, status, err)
	}
	if len(asked) != 1 {
		t.Errorf("the yaml must not be asked for once the hcl answered: %v", asked)
	}

	// Only YAML: the fall-through still works for a project the flip has not
	// reached.
	asked = nil
	httpGet = func(url string) (int, []byte, error) {
		asked = append(asked, url)
		if strings.Contains(url, "package.yml") {
			return 200, []byte("a: 1\n"), nil
		}
		return 404, nil, nil
	}
	if name, status, _, _ := fetchOverlayRecipe("x.org"); status != 200 || name != "package.yml" {
		t.Errorf("fall through to yaml: %q %d", name, status)
	}
	if len(asked) != 2 {
		t.Errorf("both names must be tried: %v", asked)
	}

	// Neither: a 404 only after both, and it names the last one tried.
	httpGet = func(string) (int, []byte, error) { return 404, nil, nil }
	if _, status, _, _ := fetchOverlayRecipe("nowhere.org"); status != 404 {
		t.Errorf("status = %d, want 404", status)
	}

	// A transport failure stops the walk: trying the second name would turn a
	// network fault into "absent from the overlay", which is a different
	// report and a wrong one.
	wantErr := errors.New("no route")
	httpGet = func(string) (int, []byte, error) { return 0, nil, wantErr }
	if _, _, _, err := fetchOverlayRecipe("x.org"); !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want the transport error", err)
	}
}

func TestRunAgreement(t *testing.T) {
	dir := writePatches(t, map[string]string{"acme-new.patch": newPatch})
	old := httpGet
	defer func() { httpGet = old }()
	// The overlay answers in HCL and the patch half is YAML, which is the
	// arrangement in production: the two agree as DOCUMENTS and share barely a
	// byte.
	httpGet = func(url string) (int, []byte, error) {
		if !strings.Contains(url, "/projects/acme.org/tool/package.hcl") {
			t.Errorf("unexpected url %q", url)
		}
		return 200, []byte("distributable {\n  url = \"https://acme.org/tool-{{version}}.tar.gz\"\n}\ndisplay-name = \"tool\"\n"), nil
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
		{"drifted", func(string) (int, []byte, error) { return 200, []byte("display-name = \"something else\"\n"), nil },
			"halves have drifted"},
		// A half that cannot be READ is not "no drift": passing it would let
		// through a recipe no consumer can parse.
		{"unreadable", func(string) (int, []byte, error) { return 200, []byte("build { script = \n"), nil },
			"cannot compare the halves"},
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
// what a bad address is for. Both credential paths are walked: a token is what
// CI has, and its absence is what a laptop has.
func TestHTTPGetIsWired(t *testing.T) {
	for _, tok := range []string{"", "not-a-real-token"} {
		t.Setenv("GITHUB_TOKEN", tok)
		if _, _, err := httpGet("http://127.0.0.1:1/nothing"); err == nil {
			t.Errorf("GITHUB_TOKEN=%q: want a transport error from a closed port", tok)
		}
	}
	// A URL the request builder itself rejects, so the failure is not the
	// transport's: the error must still reach the caller rather than a nil.
	if _, _, err := httpGet("://malformed"); err == nil {
		t.Error("want an error from an unbuildable request")
	}
}

func TestMainRuns(t *testing.T) {
	dir := writePatches(t, map[string]string{"acme-new.patch": newPatch})
	old, oldArgs, oldExit := httpGet, os.Args, osExit
	defer func() { httpGet, os.Args, osExit = old, oldArgs, oldExit }()
	exited := -1
	osExit = func(c int) { exited = c }

	httpGet = func(string) (int, []byte, error) {
		return 200, []byte("distributable {\n  url = \"https://acme.org/tool-{{version}}.tar.gz\"\n}\ndisplay-name = \"tool\"\n"), nil
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

// And the SUCCESS path of the real seam, which the closed-port cases above
// cannot reach: a response that arrives has a body to read and a status to
// return, and nothing else in this tool exercises those three lines.
func TestHTTPGetReadsAResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The contents API is asked for the file itself, not its
		// base64-in-JSON envelope, and CI's token must travel.
		if got := r.Header.Get("Accept"); got != "application/vnd.github.raw" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()
	t.Setenv("GITHUB_TOKEN", "tok")
	status, body, err := httpGet(srv.URL)
	if err != nil || status != http.StatusTeapot || string(body) != "body" {
		t.Errorf("got %d, %q, %v", status, body, err)
	}
}
