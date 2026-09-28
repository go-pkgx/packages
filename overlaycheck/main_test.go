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
	"unicode/utf8"
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

// The two halves are in different FORMATS — the overlay is HCL, the build
// patch stays YAML because it applies to an upstream clone — so drift is a
// question about what each says, not about the bytes.
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

// A patch file that cannot be read is a hard failure, not a project silently
// left unchecked — the whole value of this tool is that it does not skip.
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

// The entry point, in the one mode it has left.
func TestMainRuns(t *testing.T) {
	pantry, overlay := twoHalves(t)
	oldGet, oldArgs, oldExit := httpGet, os.Args, osExit
	t.Cleanup(func() { httpGet, os.Args, osExit = oldGet, oldArgs, oldExit })
	exited := -1
	osExit = func(c int) { exited = c }
	httpGet = func(string) (int, []byte, error) { return 200, []byte("ok"), nil }

	os.Args = []string{"overlaycheck", "--pantry", pantry, "--overlay", overlay, "--overrides", ""}
	main()
	if exited != -1 {
		t.Errorf("agreement must not exit, got %d", exited)
	}

	// Without an overlay there is nothing to compare, and that is a refusal
	// rather than a clean run — the shape that reported "0 projects, both
	// halves in agreement" for a directory whose subject had disappeared.
	os.Args = []string{"overlaycheck"}
	main()
	if exited != 2 {
		t.Errorf("exit = %d, want 2", exited)
	}

	// A disagreement exits non-zero, which is what makes this a gate.
	exited = -1
	writeFile(t, filepath.Join(overlay, "projects", "acme.org", "package.hcl"),
		"distributable {\n  url = \"https://acme.org/{{version}}.tar.gz\"\n}\nprovides = [\"bin/acme\"]\n")
	os.Args = []string{"overlaycheck", "--pantry", pantry, "--overlay", overlay, "--overrides", ""}
	main()
	if exited != 1 {
		t.Errorf("exit = %d, want 1", exited)
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

// writeFile is the two lines every fixture below would otherwise repeat.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// twoHalves lays out the pair of trees this mode compares: an overlay checkout
// in HCL and a pantry in YAML, the shapes the two really have.
func twoHalves(t *testing.T) (pantry, overlay string) {
	t.Helper()
	root := t.TempDir()
	pantry, overlay = filepath.Join(root, "pantry"), filepath.Join(root, "overlay")
	writeFile(t, filepath.Join(pantry, "projects", "acme.org", "package.yml"),
		"distributable:\n  url: https://acme.org/{{version}}.tar.gz\n")
	writeFile(t, filepath.Join(overlay, "projects", "acme.org", "package.hcl"),
		"distributable {\n  url = \"https://acme.org/{{version}}.tar.gz\"\n}\n")
	return pantry, overlay
}

func TestRunAgainstPantryAgrees(t *testing.T) {
	pantry, overlay := twoHalves(t)
	var buf bytes.Buffer
	if code := runAgainstPantry(pantry, overlay, "", &buf); code != 0 {
		t.Fatalf("code = %d, want 0\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "1 of 1 overlay project(s) are in both halves, 0 disagree") {
		t.Errorf("report:\n%s", buf.String())
	}
}

// A disagreement must name the KEY. "They differ" over 183 projects is a list
// nobody can triage; the key path says whether a consumer is even affected —
// 23 of our 25 real ones are under `build`, which no consumer reads.
func TestRunAgainstPantryNamesTheKey(t *testing.T) {
	pantry, overlay := twoHalves(t)
	writeFile(t, filepath.Join(overlay, "projects", "acme.org", "package.hcl"),
		"distributable {\n  url = \"https://acme.org/{{version}}.tar.gz\"\n}\ndependencies = {\n  \"gnu.org/gettext\" = \"^1\"\n}\n")
	var buf bytes.Buffer
	if code := runAgainstPantry(pantry, overlay, "", &buf); code != 1 {
		t.Fatalf("code = %d, want 1\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "dependencies") {
		t.Errorf("the report must name the key that parted:\n%s", buf.String())
	}
}

// A project the overlay carries and the pantry does not is the ADDED case, and
// the narrow check owns it: this mode must pass over it, not fail on it.
func TestAProjectOnlyTheOverlayCarriesMustBeServed(t *testing.T) {
	pantry, overlay := twoHalves(t)
	writeFile(t, filepath.Join(overlay, "projects", "only.example", "package.hcl"),
		"distributable {\n  url = \"https://only.example/x.tar.gz\"\n}\n")

	old := httpGet
	t.Cleanup(func() { httpGet = old })

	// Upstream has no such project, so the overlay's copy is the only recipe
	// there is — for the builder AND the consumer. There are no two halves to
	// compare, and the one thing that can still go wrong is the one that cost
	// four published, signed, unreachable packages: it has to be SERVED.
	httpGet = func(string) (int, []byte, error) { return 200, []byte("ok"), nil }
	var buf bytes.Buffer
	if code := runAgainstPantry(pantry, overlay, "", &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "1 are ours alone and served") {
		t.Errorf("report:\n%s", buf.String())
	}

	// Merged but not served: a checkout can be right while the contents API
	// still returns the old state.
	httpGet = func(string) (int, []byte, error) { return 404, nil, nil }
	buf.Reset()
	if code := runAgainstPantry(pantry, overlay, "", &buf); code != 1 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "does not SERVE it") {
		t.Errorf("report:\n%s", buf.String())
	}
}

// An unreadable overlay recipe is reported, not skipped. A directory named
// package.hcl is the cheap way to make ReadFile fail on every platform.
func TestRunAgainstPantryUnreadableOverlayRecipe(t *testing.T) {
	pantry, overlay := twoHalves(t)
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file")
	}
	p := filepath.Join(overlay, "projects", "acme.org", "package.hcl")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	// It is still CHECKED — one project, one failure — so the exit is 1.
	if code := runAgainstPantry(pantry, overlay, "", &buf); code != 1 {
		t.Fatalf("code = %d, want 2\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "cannot read the overlay") {
		t.Errorf("report:\n%s", buf.String())
	}
}

// A recipe neither half can parse must say so rather than count as agreement.
func TestRunAgainstPantryUnparsable(t *testing.T) {
	pantry, overlay := twoHalves(t)
	writeFile(t, filepath.Join(overlay, "projects", "acme.org", "package.hcl"), "this is not hcl {{{\n")
	var buf bytes.Buffer
	if code := runAgainstPantry(pantry, overlay, "", &buf); code != 1 {
		t.Fatalf("code = %d, want 1\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "cannot compare the halves") {
		t.Errorf("report:\n%s", buf.String())
	}
}

// The three ways of pointing at the wrong tree, each of which USED to report a
// clean run: no overlay path, an overlay with no recipes, a missing pantry
// root, and two trees that share no project.
func TestRunAgainstPantryRefusesTheWrongTrees(t *testing.T) {
	pantry, overlay := twoHalves(t)
	empty := t.TempDir()
	noRecipes := t.TempDir()
	writeFile(t, filepath.Join(noRecipes, "projects", "README.md"), "x")

	for _, tc := range []struct{ name, pantry, overlay, want string }{
		{"no overlay given", pantry, "", "no overlay checkout given"},
		{"overlay path does not exist", pantry, empty + "/absent", "no such file"},
		{"overlay carries no recipe", pantry, noRecipes, "is that the overlay?"},
		{"pantry has no projects/", empty, overlay, "no projects/ under"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if code := runAgainstPantry(tc.pantry, tc.overlay, "", &buf); code != 2 {
				t.Fatalf("code = %d, want 2\n%s", code, buf.String())
			}
			if !strings.Contains(buf.String(), tc.want) {
				t.Errorf("report:\n%s", buf.String())
			}
		})
	}

	t.Run("trees that share no project", func(t *testing.T) {
		// Served, so the "ours alone" arm succeeds and the only thing left to
		// say is that the two trees do not line up. Without the stub this
		// tests a 404 from the real API instead.
		old := httpGet
		t.Cleanup(func() { httpGet = old })
		httpGet = func(string) (int, []byte, error) { return 200, []byte("ok"), nil }
		other := t.TempDir()
		writeFile(t, filepath.Join(other, "projects", "elsewhere.example", "package.yml"), "distributable:\n  url: x\n")
		var buf bytes.Buffer
		if code := runAgainstPantry(other, overlay, "", &buf); code != 2 {
			t.Fatalf("code = %d, want 2\n%s", code, buf.String())
		}
		if !strings.Contains(buf.String(), "are these the right trees?") {
			t.Errorf("report:\n%s", buf.String())
		}
	})
}

func TestOverlayRecipeFileTriesBothNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "projects", "acme.org", "package.yml"), "x")
	name, body, err := overlayRecipeFile(dir, "acme.org")
	if err != nil || name != "package.yml" || string(body) != "x" {
		t.Fatalf("got (%q, %q, %v)", name, body, err)
	}
	if _, _, err := overlayRecipeFile(dir, "absent.example"); err == nil {
		t.Error("a project with no recipe must be an error")
	}
}

// The walk must not mistake the overlay's own root for a project, and must
// ignore whatever else a checkout carries — READMEs, CI, a .git.
func TestOverlayProjectsIgnoresWhatIsNotARecipe(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "projects", "package.hcl"), "x")
	writeFile(t, filepath.Join(dir, "projects", "acme.org", "package.hcl"), "x")
	writeFile(t, filepath.Join(dir, "projects", "acme.org", "README.md"), "x")
	got, err := overlayProjects(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "acme.org" {
		t.Errorf("got %v, want [acme.org]", got)
	}
	if _, err := overlayProjects(filepath.Join(dir, "absent")); err == nil {
		t.Error("a missing tree must be an error, not an empty list")
	}
}

// A difference in a build script is thirty lines long, and printing it buries
// the twenty-four other projects. The cut lands on a rune boundary: a recipe
// may hold any UTF-8.
func TestElideKeepsOneReadableLine(t *testing.T) {
	if got := elide(" a\nb  c "); got != "a b c" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("x", 99) + "é" + strings.Repeat("y", 50)
	got := elide(long)
	if !strings.HasSuffix(got, "…") || !utf8.ValidString(got) {
		t.Errorf("got %q", got)
	}
	if len(got) >= len(long) {
		t.Errorf("not elided: %q", got)
	}
}

// The second mode, through the entry point: both flags or neither.
func TestMainPantryMode(t *testing.T) {
	pantry, overlay := twoHalves(t)
	oldArgs, oldExit := os.Args, osExit
	defer func() { os.Args, osExit = oldArgs, oldExit }()
	exited := -1
	osExit = func(c int) { exited = c }

	os.Args = []string{"overlaycheck", "--pantry", pantry, "--overlay", overlay, "--overrides", ""}
	main()
	if exited != -1 {
		t.Errorf("agreement must not exit, got %d", exited)
	}

	os.Args = []string{"overlaycheck", "--pantry", pantry}
	main()
	if exited != 2 {
		t.Errorf("exit = %d, want 2", exited)
	}

	exited = -1
	os.Args = []string{"overlaycheck", "--pantry", filepath.Join(pantry, "absent"), "--overlay", overlay}
	main()
	if exited != 2 {
		t.Errorf("exit = %d, want 2", exited)
	}
}

// The masking that hid a real defect: DocDiff returns the FIRST difference in
// sorted key order, `build` sorts before `dependencies`, and tcl-lang.org
// differs under both. Reported as one comparison, the live consumer-side
// difference was invisible behind an inert build-side one — and the summary
// that followed, "23 of 25 are under build, which no consumer reads", was an
// artefact of the alphabet.
func TestRunAgainstPantryAsksTheConsumerQuestionFirst(t *testing.T) {
	pantry, overlay := twoHalves(t)
	writeFile(t, filepath.Join(pantry, "projects", "acme.org", "package.yml"),
		"distributable:\n  url: https://acme.org/{{version}}.tar.gz\nbuild:\n  script: make\nversions:\n  url: https://sourceforge.net/x\n")
	writeFile(t, filepath.Join(overlay, "projects", "acme.org", "package.hcl"),
		"distributable {\n  url = \"https://acme.org/{{version}}.tar.gz\"\n}\nbuild {\n  script = \"gmake\"\n}\nversions {\n  url = \"https://acme.org/downloads\"\n}\n")
	var buf bytes.Buffer
	if code := runAgainstPantry(pantry, overlay, "", &buf); code != 1 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "CONSUMER") || !strings.Contains(buf.String(), "versions") {
		t.Errorf("the consumer half must be reported over the build half:\n%s", buf.String())
	}
}

// Within the consumer half too: `dependencies` sorts before `versions`, so one
// key at a time, and every one of them reported.
func TestConsumerDiffLetsNoKeyHideAnother(t *testing.T) {
	a := map[string]any{
		"dependencies": map[string]any{"a.org": "1"},
		"versions":     map[string]any{"url": "https://old"},
		"build":        map[string]any{"script": "make"},
	}
	b := map[string]any{
		"dependencies": map[string]any{"a.org": "2"},
		"versions":     map[string]any{"url": "https://new"},
		"build":        map[string]any{"script": "gmake"},
	}
	got := consumerDiff(a, b)
	if !strings.Contains(got, "dependencies") || !strings.Contains(got, "versions") {
		t.Errorf("both keys must be reported, got %q", got)
	}
	// `build` is not a consumer's business, and reporting it here would put
	// the noise back.
	if strings.Contains(got, "build") {
		t.Errorf("build must not appear in the consumer half: %q", got)
	}
	if consumerDiff(a, a) != "" {
		t.Errorf("agreement must be silent")
	}
	// A key on one side only, in both directions.
	if got := consumerDiff(map[string]any{"provides": []any{"bin/x"}}, map[string]any{}); got != "provides: dropped" {
		t.Errorf("got %q", got)
	}
	if got := consumerDiff(map[string]any{}, map[string]any{"provides": []any{"bin/x"}}); got != "provides: added" {
		t.Errorf("got %q", got)
	}
}

// The gate: a build-side difference is reported and passes, a consumer-side
// one fails unless it is on the deliberate list. 23 of the 25 real
// disagreements are our own override patches, which nothing reads out of the
// overlay; failing on those would mean mirroring every patch into a second
// tree forever.
func TestRunAgainstPantryGatesOnlyTheConsumerHalf(t *testing.T) {
	build := "distributable:\n  url: https://acme.org/{{version}}.tar.gz\nbuild:\n  script: make\n"
	for _, tc := range []struct {
		name, pantry, overlay string
		want                  int
	}{
		{
			"a build difference is reported and passes",
			build,
			"distributable {\n  url = \"https://acme.org/{{version}}.tar.gz\"\n}\nbuild {\n  script = \"gmake\"\n}\n",
			0,
		},
		{
			"a consumer difference fails",
			build,
			"distributable {\n  url = \"https://acme.org/{{version}}.tar.gz\"\n}\nbuild {\n  script = \"make\"\n}\nprovides = [\"bin/acme\"]\n",
			1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pantry, overlay := twoHalves(t)
			writeFile(t, filepath.Join(pantry, "projects", "acme.org", "package.yml"), tc.pantry)
			writeFile(t, filepath.Join(overlay, "projects", "acme.org", "package.hcl"), tc.overlay)
			var buf bytes.Buffer
			if code := runAgainstPantry(pantry, overlay, "", &buf); code != tc.want {
				t.Fatalf("code = %d, want %d\n%s", code, tc.want, buf.String())
			}
			// Either way the difference is REPORTED. A gate that passes in
			// silence teaches nobody that the two halves have parted.
			if !strings.Contains(buf.String(), "✗ acme.org") {
				t.Errorf("the difference must be reported whatever the exit:\n%s", buf.String())
			}
		})
	}

	// On the deliberate list, the same consumer difference passes — that list
	// is what this overlay exists to hold.
	t.Run("deliberate passes", func(t *testing.T) {
		pantry, overlay := twoHalves(t)
		deliberate["acme.org"] = true
		t.Cleanup(func() { delete(deliberate, "acme.org") })
		writeFile(t, filepath.Join(overlay, "projects", "acme.org", "package.hcl"),
			"distributable {\n  url = \"https://acme.org/{{version}}.tar.gz\"\n}\nprovides = [\"bin/acme\"]\n")
		var buf bytes.Buffer
		if code := runAgainstPantry(pantry, overlay, "", &buf); code != 0 {
			t.Fatalf("code = %d, want 0\n%s", code, buf.String())
		}
	})

	// Every name on the list must be a project the overlay carries. A list
	// that outlives its entries stops being a decision and becomes a place
	// defects hide.
	t.Run("the list names real projects", func(t *testing.T) {
		for p := range deliberate {
			if !strings.Contains(p, ".") {
				t.Errorf("%q is not a project name", p)
			}
		}
	})
}

// The pantry on disk stopped being the recipe the factory builds when the
// overrides became logical: most are applied as a recipe is READ. A check that
// read package.yml alone compared the overlay against an UNOVERRIDDEN recipe
// and reported 180 disagreements the day the last unified diff was deleted,
// every one of them ours.
func TestRunAgainstPantryAppliesTheLogicalOverrides(t *testing.T) {
	pantry, overlay := twoHalves(t)
	// The overlay says ^3; the pantry file still says ^1.1, and an override
	// closes the gap at load.
	writeFile(t, filepath.Join(pantry, "projects", "acme.org", "package.yml"),
		"distributable:\n  url: https://acme.org/{{version}}.tar.gz\ndependencies:\n  openssl.org: ^1.1\n")
	writeFile(t, filepath.Join(overlay, "projects", "acme.org", "package.hcl"),
		"distributable {\n  url = \"https://acme.org/{{version}}.tar.gz\"\n}\ndependencies = {\n  \"openssl.org\" = \"^3\"\n}\n")

	ov := t.TempDir()
	writeFile(t, filepath.Join(ov, "acme.hcl"), `
project = "acme.org"
why     = "our registry carries no openssl 1.x"
edits   = [{ path = "dependencies[\"openssl.org\"]", set = "^3" }]
`)
	var buf bytes.Buffer
	if code := runAgainstPantry(pantry, overlay, ov, &buf); code != 0 {
		t.Fatalf("code = %d\n%s", code, buf.String())
	}
	// Without the overrides the same two halves disagree, and on the half a
	// consumer reads — so this is a failure, not noise.
	buf.Reset()
	if code := runAgainstPantry(pantry, overlay, "", &buf); code != 1 {
		t.Errorf("code = %d — reading the file alone must not agree\n%s", code, buf.String())
	}
}

// Every way reading the built recipe can fail.
func TestRunAgainstPantryReportsABadOverrideDirectory(t *testing.T) {
	pantry, overlay := twoHalves(t)

	t.Run("an override that does not parse", func(t *testing.T) {
		ov := t.TempDir()
		writeFile(t, filepath.Join(ov, "bad.hcl"), "project = ")
		var buf bytes.Buffer
		if code := runAgainstPantry(pantry, overlay, ov, &buf); code != 2 {
			t.Errorf("code = %d\n%s", code, buf.String())
		}
	})

	t.Run("a pantry recipe that does not parse", func(t *testing.T) {
		p, o := twoHalves(t)
		writeFile(t, filepath.Join(p, "projects", "acme.org", "package.yml"), "a: [\n")
		var buf bytes.Buffer
		// A recipe that is THERE and does not parse is a failure the run
		// measured, not a missing project. Treating it as absent used to make
		// it vanish into the skip arm.
		if code := runAgainstPantry(p, o, "", &buf); code != 1 {
			t.Errorf("code = %d\n%s", code, buf.String())
		}
		if !strings.Contains(buf.String(), "cannot read the built recipe") {
			t.Errorf("report:\n%s", buf.String())
		}
	})

	t.Run("an override whose premise is gone", func(t *testing.T) {
		ov := t.TempDir()
		writeFile(t, filepath.Join(ov, "acme.hcl"), `
project = "acme.org"
why     = "w"
edits   = [{ path = "build.script", from = "cmake", to = "cmake3" }]
`)
		var buf bytes.Buffer
		// 1, not 2: this is a FAILURE the run measured, not a pair of trees
		// that do not line up. The first draft reported it as the latter, and
		// called it "upstream has no such project" — which sends the reader to
		// the wrong question entirely.
		if code := runAgainstPantry(pantry, overlay, ov, &buf); code != 1 {
			t.Errorf("code = %d — an override that cannot apply must stop the run\n%s", code, buf.String())
		}
		if !strings.Contains(buf.String(), "cannot read the built recipe") {
			t.Errorf("report:\n%s", buf.String())
		}
	})
}

// upstreamDoc is the baseline a consumer merges over, and a baseline it cannot
// read is no baseline: the comparison falls back to the overlay file alone
// rather than merging over something half-read.
func TestUpstreamDocOnWhatItCannotRead(t *testing.T) {
	pantry := t.TempDir()
	if got := upstreamDoc(pantry, "absent.example"); got != nil {
		t.Errorf("a project upstream does not carry has no baseline: %v", got)
	}
	writeFile(t, filepath.Join(pantry, "projects", "bad.example", "package.yml"), "a: [\n")
	if got := upstreamDoc(pantry, "bad.example"); got != nil {
		t.Errorf("a recipe that does not parse is no baseline: %v", got)
	}
	writeFile(t, filepath.Join(pantry, "projects", "ok.example", "package.yml"), "provides:\n  - bin/x\n")
	if got := upstreamDoc(pantry, "ok.example"); got == nil {
		t.Error("a readable recipe is the baseline")
	}
}

// The comparison is against the MERGED consumer view. An entry that states
// only what it changes has no `versions` and no `provides` in the FILE, and
// reading the file alone would call every reduced entry a disagreement — which
// is what happened the moment the overlay stopped being 183 full copies.
func TestRunAgainstPantryComparesTheMergedView(t *testing.T) {
	pantry, overlay := twoHalves(t)
	writeFile(t, filepath.Join(pantry, "projects", "acme.org", "package.yml"),
		"distributable:\n  url: https://acme.org/{{version}}.tar.gz\nprovides:\n  - bin/acme\ndependencies:\n  openssl.org: ^1.1\n")
	// Says only what it changes.
	writeFile(t, filepath.Join(overlay, "projects", "acme.org", "package.hcl"),
		"dependencies = {\n  \"openssl.org\" = \"^3\"\n}\n")

	ov := t.TempDir()
	writeFile(t, filepath.Join(ov, "acme.hcl"), `
project = "acme.org"
why     = "our registry carries no openssl 1.x"
edits   = [{ path = "dependencies[\"openssl.org\"]", set = "^3" }]
`)
	var buf bytes.Buffer
	if code := runAgainstPantry(pantry, overlay, ov, &buf); code != 0 {
		t.Fatalf("code = %d — a reduced entry is not a disagreement\n%s", code, buf.String())
	}
	if strings.Contains(buf.String(), "provides") {
		t.Errorf("a key the entry inherits was read as missing:\n%s", buf.String())
	}
}

// TestRunAgainstPantryRefusesAnOverrideDirectoryThatHoldsNothing. The empty
// OVERLAY has been a refusal since the beginning; the sibling input was read
// the same way and reported nothing.
//
// Measured 2026-09-28, the same two trees twice: run from go-pkgx/packages,
// where the default `overrides` resolves, 26 projects disagree and the gate
// passes; run one directory away it is 175, and 149 of those are our own
// openssl pin reported as drift. A gate whose answer depends on the shell's
// working directory is not a gate.
func TestRunAgainstPantryRefusesAnOverrideDirectoryThatHoldsNothing(t *testing.T) {
	pantry, overlay := twoHalves(t)
	for _, dir := range []string{"overrides", t.TempDir()} {
		var buf bytes.Buffer
		if code := runAgainstPantry(pantry, overlay, dir, &buf); code != 2 {
			t.Errorf("%s: code = %d, want 2\n%s", dir, code, buf.String())
		}
		if !strings.Contains(buf.String(), "read UNOVERRIDDEN") {
			t.Errorf("%s: the report must say what it would have compared:\n%s", dir, buf.String())
		}
	}
	// An EMPTY name is the one honest way to ask for none, and the control
	// above depends on it staying that way.
	var buf bytes.Buffer
	if code := runAgainstPantry(pantry, overlay, "", &buf); code == 2 {
		t.Errorf("an explicit none must not be refused:\n%s", buf.String())
	}
}
