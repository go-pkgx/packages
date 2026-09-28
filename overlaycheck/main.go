// Command overlaycheck fails when a project that exists ONLY here has drifted
// from, or is missing in, the consumer half.
//
// The factory keeps a recipe in two places and they must agree:
//
//   - overrides/<name>-new.patch, applied to a fresh pkgxdev/pantry clone, is
//     what `bk` BUILDS from;
//   - go-pkgx/pantry-overlay/projects/<project>/package.hcl is what a consumer
//     RESOLVES over HTTP (bottle's fetchRecipe tries the overlay, then upstream,
//     and nothing else).
//
// The two halves are in DIFFERENT FORMATS and that is deliberate: the patch
// applies to a fresh upstream clone, which is YAML, while the overlay is HCL.
// So they are compared as documents, through the same conversion the client
// runs — not as bytes, which is what they were until the overlay changed
// format and this check called all eight of its projects missing.
//
// For a project that upstream does not carry — openucx.org, rdma-core,
// cuda-cudart, ROCr — the overlay is the ONLY place a consumer can learn the
// dependencies and the runtime environment. Four such projects were published,
// signed and unreachable to any consumer for days because only the build half
// existed. Nothing said so: the bottles were there, the registry listed them,
// and every fetch of a recipe returned 404 from both pantries.
//
// This checks only the case where the answer is not a judgement call — a patch
// that ADDS a whole project. A patch that MODIFIES an upstream recipe may
// belong in the overlay or may be build-only, and flagging every one of those
// would train people to ignore the check.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-pkgx/bk/logical"
	"github.com/go-pkgx/bottle"
)

// overlayBase names the overlay's recipes through the GitHub contents API
// rather than through raw.githubusercontent.com.
//
// A checkout is the wrong source — it can be right while what is published is
// not — but so, it turns out, is the raw CDN. Seconds after an overlay change
// merged, three requests for the same file returned the OLD 3959-byte recipe
// and only the API returned the new 6077-byte one:
//
//	plain             3959
//	Cache-Control: no-cache   3959
//	?t=<nanoseconds>  3959
//	contents API      6077
//
// Neither a request-side cache directive nor a query-string buster dislodges
// it. So the check would have reported a drift that was already fixed, for as
// long as the CDN held the file — and a check that cries wolf after every fix
// teaches people to re-run it until it agrees, which is the same as not having
// one.
//
// Worth knowing beyond this tool: bottle's fetchRecipe reads that same CDN, so
// a consumer can resolve a stale recipe for minutes after a change merges. That
// is a property of the delivery path, not of this check, and it is not what
// this check is for — a maintainer forgetting a half is about what is
// COMMITTED.
const overlayBase = "https://api.github.com/repos/go-pkgx/pantry-overlay/contents/projects"

// httpGet is a seam so the tests do not reach the network.
var httpGet = func(url string) (int, []byte, error) {
	c := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	// The raw media type asks the contents API for the file itself rather than
	// its base64-in-JSON envelope.
	req.Header.Set("Accept", "application/vnd.github.raw")
	req.Header.Set("User-Agent", "overlaycheck")
	// Unauthenticated is 60 requests an hour, ample for a handful of recipes;
	// CI has a token anyway and 5000 removes the question.
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

// osExit is a seam so a test can exercise the failure path without exiting.
var osExit = os.Exit

func main() {
	// Two questions, two modes.
	//
	//	overlaycheck [overrides-dir]
	//	   a project a patch ADDS must exist in the overlay at all
	//	overlaycheck --pantry <patched-pantry> --overlay <checkout>
	//	   a project in BOTH halves must say the same thing in both
	//
	// The second needs a pantry with the overrides applied, because a
	// modifying patch's + lines are a fragment and the resulting recipe is
	// the only thing there is to compare.
	// Its OWN FlagSet, not the package-level one: flag.String panics on a
	// name already registered, so a main() that registers globally can be
	// called exactly once per process — and the test calls it four times, to
	// cover the two modes and both ways of being wrong. A test that cannot
	// run the entry point twice ends up not covering the entry point.
	fs := flag.NewFlagSet("overlaycheck", flag.ExitOnError)
	pantryDir := fs.String("pantry", "", "a pantry checkout with the overrides ALREADY APPLIED — `bk overrides` leaves one")
	overlayDir := fs.String("overlay", "", "an overlay checkout, to enumerate what it carries")
	overridesDir := fs.String("overrides", "overrides", "the override directory, so the pantry recipe is read as the FACTORY reads it")
	_ = fs.Parse(os.Args[1:])

	if *pantryDir != "" || *overlayDir != "" {
		if *pantryDir == "" || *overlayDir == "" {
			fmt.Fprintln(os.Stderr, "overlaycheck: --pantry and --overlay go together")
			osExit(2)
			return
		}
		if code := runAgainstPantry(*pantryDir, *overlayDir, *overridesDir, os.Stdout); code != 0 {
			osExit(code)
		}
		return
	}

	dir := "overrides"
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	if code := run(dir, os.Stdout); code != 0 {
		osExit(code)
	}
}

// run compares every added project against the overlay and returns a process
// exit code.
// runAgainstPantry compares EVERY project the overlay carries against the
// recipe the builder would use — the patched pantry — rather than only the
// projects a patch creates.
//
// The narrow check above exists because a MODIFYING patch's + lines are a
// fragment, not a recipe, so there is nothing to compare them with. Given a
// pantry with the overrides already applied there is: the file itself.
//
// And it needs checking. 180 of the overlay's 183 recipes also have an
// override patch, so the same project is expressed twice — once as a YAML
// patch the builder reads, once as HCL a consumer resolves — and until now
// nothing compared those two for a project that already existed upstream.
// bk builds from the first and pkgx resolves dependencies from the second, in
// the same build.
func runAgainstPantry(pantryDir, overlayHint, overridesDir string, out io.Writer) int {
	// The pantry on disk is no longer the recipe the factory builds. Since the
	// overrides became logical (go-pkgx/packages#268) most of them are applied
	// as a recipe is READ and change nothing on the filesystem, so a check that
	// reads package.yml is comparing the overlay against an UNOVERRIDDEN
	// recipe. It reported 180 disagreements the moment the last unified diff
	// was deleted, every one of them ours.
	set, err := logical.LoadDir(overridesDir)
	if err != nil {
		fmt.Fprintln(out, "overlaycheck:", err)
		return 2
	}
	projects, err := overlayProjects(overlayHint)
	if err != nil {
		fmt.Fprintln(out, "overlaycheck:", err)
		return 2
	}
	// An empty overlay is a FAILURE, not agreement: a wrong path would
	// otherwise report a clean run forever.
	if len(projects) == 0 {
		fmt.Fprintf(out, "overlaycheck: no recipe under %s — is that the overlay?\n", overlayHint)
		return 2
	}
	sort.Strings(projects)

	// The pantry ROOT must be there. A missing one makes every project look
	// like the ADDED case below, and the run reports "0 in both halves, 0
	// disagree" — a clean pass on a measurement that never happened. Written
	// that way once, and caught by the count being absurd rather than by the
	// exit status.
	if _, err := os.Stat(filepath.Join(pantryDir, "projects")); err != nil {
		fmt.Fprintf(out, "overlaycheck: no projects/ under %s: %v\n", pantryDir, err)
		return 2
	}

	bad, checked, broken := 0, 0, 0
	var unexplained []string
	for _, project := range projects {
		builtDoc, err := builtRecipe(set, pantryDir, project)
		if err != nil {
			// Not in the pantry at all: that is the ADDED case, and the
			// narrow check above already owns it.
			continue
		}
		// Counted here, BEFORE the overlay read, because this counter answers
		// "do the two trees line up at all" and the pantry has just said this
		// one does. Counting it after the read made an unreadable overlay
		// recipe report "not one overlay project was found in the pantry" —
		// which is false, and sends the reader to the wrong tree. Found by a
		// test asserting the exit code, not by reading the guard.
		checked++
		// From the CHECKOUT, not the API. This mode asks whether the two
		// SOURCES agree — a repository question — while the narrow check
		// above asks what is SERVED, which is why that one reads the
		// contents API. It also keeps 360 API calls out of a CI run.
		name, body, err := overlayRecipeFile(overlayHint, project)
		if err != nil {
			fmt.Fprintf(out, "✗ %s: cannot read the overlay: %v\n", project, err)
			bad++
			broken++
			continue
		}
		d, err := recipeDiffDoc(body, name, builtDoc)
		switch {
		case err != nil:
			fmt.Fprintf(out, "✗ %s: cannot compare the halves: %v\n", project, err)
			bad++
			broken++
		case d != "":
			fmt.Fprintf(out, "✗ %-30s %s\n", project, elide(d))
			bad++
			if c, ok := strings.CutPrefix(d, consumerMark); ok && !deliberate[project] {
				unexplained = append(unexplained, project+": "+elide(c))
			}
		}
	}
	fmt.Fprintf(out, "\n%d of %d overlay project(s) are in both halves, %d disagree.\n",
		checked, len(projects), bad)
	// A build-side difference is reported and does NOT fail: nothing reads a
	// build section out of the overlay — the factory compiles the pantry's
	// recipe with the overrides applied. 23 of the 25 disagreements are that,
	// they are our own override patches, and failing on them would mean
	// mirroring every patch into a second tree forever.
	//
	// A CONSUMER-side difference is another matter. It is what resolution
	// actually uses, the overlay WINS there, and a stale one is served. So
	// those fail, minus the handful this overlay exists to make.
	// A comparison that could not be PERFORMED always fails, whichever half it
	// would have been about. Only a difference the tool actually measured can
	// be waved through as build-side, and an unreadable or unparsable recipe
	// is not a measurement — it is the absence of one. Written the other way
	// first, and caught by a test rather than by reading it.
	if broken > 0 {
		fmt.Fprintf(out, "%d recipe(s) could not be compared at all\n", broken)
		return 1
	}
	if len(unexplained) > 0 {
		fmt.Fprintln(out, "\nthese are the half a CONSUMER resolves from, where the overlay WINS:")
		for _, u := range unexplained {
			fmt.Fprintln(out, "  ✗", u)
		}
		fmt.Fprintln(out, "either the overlay declares this deliberately — add it to `deliberate` in overlaycheck,")
		fmt.Fprintln(out, "with the reason — or it has fallen behind the pantry and should be brought back in line.")
		return 1
	}
	// Nothing in both halves, with an overlay that carries recipes, is not
	// agreement either — it means the two trees do not line up at all.
	if checked == 0 {
		fmt.Fprintln(out, "overlaycheck: not one overlay project was found in the pantry — are these the right trees?")
		return 2
	}
	return 0
}

// consumerMark prefixes a difference in the half a consumer resolves from.
const consumerMark = "CONSUMER "

// deliberate names the projects whose CONSUMER half is meant to disagree with
// upstream's. Each one is this overlay doing the job it exists for: declaring
// a runtime edge the upstream recipe omits and the closure needs.
//
// It is a list of projects rather than of project+key, because the point is
// "this project's dependencies are ours"; pinning the exact key would turn
// every legitimate addition into a second edit here.
//
// Adding to it is a decision, not a formality. The one defect this gate was
// built after — tcl-lang.org enumerating versions from a page that lists only
// what upstream recommends, while its own distributable downloads from
// SourceForge — would have belonged nowhere near this list.
var deliberate = map[string]bool{
	// go-pkgx/pantry-overlay#30: the published bottle links gettext on darwin
	// and upstream's recipe names it only as a build dependency.
	"gnu.org/libiconv": true,
	// The published perl bottle NEEDs libcrypt.so.1; glibc dropped it, and
	// upstream's perl.org says nothing about the provider.
	"perl.org": true,
	// Links openssl 3; upstream's recipe declares no runtime dependency at all.
	"rpm.org/rpm-sequoia": true,
}

// overlayRecipeFile reads a project's recipe out of an overlay checkout, in the
// order a consumer tries the two names.
func overlayRecipeFile(dir, project string) (name string, body []byte, err error) {
	for _, n := range recipeNames {
		p := filepath.Join(dir, "projects", filepath.FromSlash(project), n)
		if b, err := os.ReadFile(p); err == nil {
			return n, b, nil
		}
	}
	return "", nil, fmt.Errorf("no %v under %s", recipeNames, project)
}

// overlayProjects lists what the overlay carries, from a local checkout when
// one is given and from the contents API otherwise.
func overlayProjects(dir string) ([]string, error) {
	if dir == "" {
		return nil, fmt.Errorf("no overlay checkout given")
	}
	root := filepath.Join(dir, "projects")
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isRecipeName(d.Name()) {
			return nil
		}
		rel := filepath.ToSlash(filepath.Dir(p))
		base := filepath.ToSlash(root)
		if rel == base {
			return nil
		}
		out = append(out, strings.TrimPrefix(rel, base+"/"))
		return nil
	})
	return out, err
}

// isRecipeName is the pair of spellings a pantry uses, in the order a consumer
// tries them.
func isRecipeName(n string) bool {
	for _, r := range recipeNames {
		if n == r {
			return true
		}
	}
	return false
}

func run(dir string, out io.Writer) int {
	added, err := addedProjects(dir)
	if err != nil {
		fmt.Fprintln(out, "overlaycheck:", err)
		return 2
	}
	names := make([]string, 0, len(added))
	for p := range added {
		names = append(names, p)
	}
	sort.Strings(names)

	bad := 0
	for _, project := range names {
		name, status, body, err := fetchOverlayRecipe(project)
		switch {
		case err != nil:
			fmt.Fprintf(out, "✗ %s: cannot read the overlay: %v\n", project, err)
			bad++
		case status == http.StatusNotFound:
			fmt.Fprintf(out, "✗ %s: added here, absent from the overlay — no consumer can resolve it\n", project)
			bad++
		case status != http.StatusOK:
			fmt.Fprintf(out, "✗ %s: overlay answered %d\n", project, status)
			bad++
		default:
			same, err := sameRecipe(body, name, added[project])
			switch {
			case err != nil:
				fmt.Fprintf(out, "✗ %s: cannot compare the halves: %v\n", project, err)
				bad++
			case !same:
				fmt.Fprintf(out, "✗ %s: the two halves have drifted\n", project)
				bad++
			default:
				fmt.Fprintf(out, "✓ %s\n", project)
			}
		}
	}
	if bad > 0 {
		fmt.Fprintf(out, "\n%d of %d projects disagree between the halves.\n", bad, len(names))
		return 1
	}
	fmt.Fprintf(out, "\n%d projects, both halves in agreement.\n", len(names))
	return 0
}

// recipeNames are the two spellings a consumer looks for, in the order
// bottle's fetchRecipe tries them. The overlay is HCL now and upstream is YAML,
// so asking for only one of them is how a check goes blind: on 2026-09-27 this
// tool asked for package.yml alone and reported all eight of its projects
// "absent from the overlay" the day those recipes became package.hcl. Every
// one of them was there.
var recipeNames = []string{"package.hcl", "package.yml"}

// fetchOverlayRecipe returns the first recipe file the overlay answers for, and
// which one it was. A 404 is only reported once BOTH names have been tried.
func fetchOverlayRecipe(project string) (name string, status int, body []byte, err error) {
	for _, n := range recipeNames {
		status, body, err = httpGet(overlayBase + "/" + project + "/" + n + "?ref=main")
		if err != nil || status != http.StatusNotFound {
			return n, status, body, err
		}
	}
	return recipeNames[len(recipeNames)-1], http.StatusNotFound, nil, nil
}

// sameRecipe reports whether the two halves say the same thing, whatever
// format each is written in.
//
// It used to be a byte comparison, which worked while both halves were YAML.
// The overlay is HCL now: the build half patches an upstream clone and stays
// YAML, so there is nothing left for a byte comparison to mean. Comparing
// DOCUMENTS is also strictly better — a reformatting is no longer a drift.
//
// Both sides go through the conversion the CLIENT runs, so this agrees with
// what a consumer will actually resolve rather than with a second reading of
// the same files. A hand-rolled HCL reader here would be exactly the kind of
// second opinion that goes stale without anyone noticing.
func sameRecipe(overlay []byte, overlayName string, patch []byte) (bool, error) {
	d, err := recipeDiff(overlay, overlayName, patch)
	return d == "", err
}

// elide keeps a difference to one readable line.
//
// DocDiff reports the two values in full, which is right for a caller that
// wants them and wrong for a list of 25 projects: a build script that differs
// by one line prints both scripts, sixty lines, and buries the twenty-four
// other projects. The key path is the actionable part — it says WHERE to look —
// and the tail is a hint, not the evidence.
func elide(d string) string {
	d = strings.Join(strings.Fields(d), " ")
	cut := 100
	if len(d) > cut {
		// Cut on a rune boundary: a recipe may hold any UTF-8, and half a
		// rune in a terminal is a worse report than a shorter one.
		for cut > 0 && !utf8.RuneStart(d[cut]) {
			cut--
		}
		return d[:cut] + "…"
	}
	return d
}

// consumerKeys are the keys a CONSUMER reads out of a recipe: resolving a
// closure needs the runtime dependencies, what a package provides, and which
// versions exist. Everything else in a recipe describes a build.
var consumerKeys = []string{"dependencies", "provides", "versions", "distributable"}

// consumerDiff reports EVERY consumer-visible key the two halves part at.
//
// One key at a time, not one comparison of the four: DocDiff returns the first
// difference in sorted key order and stops, so within a single comparison
// `dependencies` hides `versions` exactly as `build` hides both. tcl-lang.org
// was hidden twice over — its `versions` block still scrapes tcl-lang.org's
// download page, which lists only what upstream currently recommends, while
// the pantry was corrected to enumerate SourceForge, where the tarball its own
// `distributable` downloads actually lives. The overlay WINS for a consumer,
// so that stale block is what a consumer resolves tcl versions with.
func consumerDiff(a, b map[string]any) string {
	var out []string
	for _, k := range consumerKeys {
		x, inA := a[k]
		y, inB := b[k]
		switch {
		case !inA && !inB:
		case inA != inB:
			side := "added"
			if inA {
				side = "dropped"
			}
			out = append(out, k+": "+side)
		default:
			if d := bottle.DocDiff(map[string]any{k: x}, map[string]any{k: y}); d != "" {
				out = append(out, strings.TrimPrefix(d, ".."))
			}
		}
	}
	return strings.Join(out, "; ")
}

// recipeDiff names the first key the two halves part at, or "" when they agree.
//
// "They differ" is not a report anybody can act on, and 25 of the 183 projects
// that exist in both halves do differ — some of them on purpose, since the
// overlay carries dependencies the build does not need. Naming the key is what
// turns that list into a triage.
//
// bottle.DocDiff, not a comparison of our own: a second walk over two recipe
// documents would be a second opinion about what a recipe means.
func recipeDiff(overlay []byte, overlayName string, patch []byte) (string, error) {
	b, err := recipeDoc(patch, "package.yml")
	if err != nil {
		return "", fmt.Errorf("the patch's package.yml: %w", err)
	}
	return recipeDiffDoc(overlay, overlayName, b)
}

// builtRecipe reads a pantry recipe the way the FACTORY reads it: the file,
// plus the project's logical override applied to the document. Reading the
// file alone stopped being the built recipe when the overrides stopped
// rewriting the tree.
func builtRecipe(set *logical.Set, pantryDir, project string) (map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(pantryDir, "projects", filepath.FromSlash(project), "package.yml"))
	if err != nil {
		return nil, err
	}
	doc, err := recipeDoc(b, "package.yml")
	if err != nil {
		return nil, err
	}
	if _, err := set.ApplyTo(project, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// recipeDiffDoc is recipeDiff against a document already read.
func recipeDiffDoc(overlay []byte, overlayName string, b map[string]any) (string, error) {
	a, err := recipeDoc(overlay, overlayName)
	if err != nil {
		return "", fmt.Errorf("the overlay's %s: %w", overlayName, err)
	}
	// The CONSUMER half first, and on its own. DocDiff returns the FIRST
	// difference in sorted key order and stops — and "build" sorts before
	// "dependencies", so any build-side difference hides every consumer-side
	// one behind it. Reported as one comparison, 23 of 25 disagreements looked
	// like build drift and 2 like deliberate overlay declarations; that split
	// was an artefact of the alphabet, not a measurement. Asking the question
	// that matters first is the only way the answer cannot be masked.
	if d := consumerDiff(a, b); d != "" {
		return consumerMark + d, nil
	}
	return bottle.DocDiff(a, b), nil
}

// recipeDoc reads either format into one document shape.
func recipeDoc(src []byte, name string) (map[string]any, error) {
	if strings.HasSuffix(name, ".yml") {
		converted, err := bottle.YAMLToHCL(src, name)
		if err != nil {
			return nil, err
		}
		src, name = converted, name+".hcl"
	}
	return bottle.HCLToMap(src, name)
}

// addedProjects reads every patch in dir and returns, per project, the content
// of a package.yml the patch ADDS. A patch that only modifies existing files
// contributes nothing.
func addedProjects(dir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".patch") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		for project, content := range addedRecipes(string(b)) {
			out[project] = content
		}
	}
	return out, nil
}

// addedRecipes extracts, from one unified diff, the full content of every
// projects/<project>/package.yml the diff creates.
//
// "Creates" is read off `new file mode`, not off the presence of + lines: a
// patch that appends to an existing recipe is a modification, and the content
// reconstructed from its hunks would be a fragment presented as a whole file.
func addedRecipes(patch string) map[string][]byte {
	out := map[string][]byte{}
	var project string
	var body []string
	inHunk, isNew := false, false

	flush := func() {
		if project != "" && isNew {
			out[project] = []byte(strings.Join(body, "\n") + "\n")
		}
		project, body, inHunk, isNew = "", nil, false, false
	}

	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
		case strings.HasPrefix(line, "new file mode "):
			isNew = true
		case strings.HasPrefix(line, "+++ b/"):
			project = projectOf(strings.TrimPrefix(line, "+++ b/"))
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case inHunk && strings.HasPrefix(line, "+"):
			body = append(body, line[1:])
		}
	}
	flush()
	return out
}

// projectOf turns projects/<project>/package.yml into <project>, and returns ""
// for any other path — a patch may touch sibling files (a .patch prop, a
// helper script) and those are not recipes.
func projectOf(path string) string {
	if !strings.HasPrefix(path, "projects/") || !strings.HasSuffix(path, "/package.yml") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(path, "projects/"), "/package.yml")
}
