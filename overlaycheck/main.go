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
	"errors"
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
	// ONE question now: for every project the overlay carries, does it agree
	// with the recipe the factory builds — and for the ones upstream does not
	// carry at all, is the overlay's copy actually SERVED?
	//
	// There used to be a second mode, reading a directory of patches that
	// CREATED a project and checking each was in the published overlay. Those
	// patches are gone: a recipe of ours lives in the overlay and bk builds it
	// from there. The mode did not fail when its subject disappeared — it
	// printed "0 projects, both halves in agreement" and passed. Its question
	// is now an arm of the walk below, where it has eight real subjects.
	//
	// Its OWN FlagSet, not the package-level one: flag.String panics on a name
	// already registered, so a main() that registers globally can be called
	// exactly once per process — and the test calls it several times. A test
	// that cannot run the entry point twice ends up not covering it.
	fs := flag.NewFlagSet("overlaycheck", flag.ExitOnError)
	pantryDir := fs.String("pantry", "pantry", "a pantry checkout")
	overlayDir := fs.String("overlay", "", "an overlay checkout, to enumerate what it carries")
	overridesDir := fs.String("overrides", "overrides", "the override directory, so the pantry recipe is read as the FACTORY reads it")
	_ = fs.Parse(os.Args[1:])

	if *overlayDir == "" {
		fmt.Fprintln(os.Stderr, "overlaycheck: --overlay names the checkout to compare")
		osExit(2)
		return
	}
	if code := runAgainstPantry(*pantryDir, *overlayDir, *overridesDir, os.Stdout); code != 0 {
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
	// And an empty OVERRIDE set is the same failure as an empty overlay, which
	// the guard below has caught since the beginning. LoadDir returns an empty
	// set for a directory that is not there — the comment above says why that
	// matters and the check was still missing.
	//
	// Measured 2026-09-28, the same trees twice: from go-pkgx/packages, where
	// `overrides/` resolves, 26 projects disagree and the run passes; from one
	// directory away it is 175, and 149 of those are our own openssl pin
	// reported as drift. A gate whose answer depends on the shell's working
	// directory is not a gate.
	//
	// An EMPTY name is the one honest way to ask for no overrides, and the
	// tests use it as the control that shows they matter. A name that was
	// given and holds nothing is the mistake.
	if overridesDir != "" && len(set.Projects()) == 0 {
		fmt.Fprintf(out, "overlaycheck: no *.hcl under %s — the pantry half would be read UNOVERRIDDEN, and every override we carry would report as drift\n", overridesDir)
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

	bad, checked, broken, ours := 0, 0, 0, 0
	var unexplained []string
	for _, project := range projects {
		builtDoc, err := builtRecipe(set, pantryDir, project)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			// The recipe is THERE and something else went wrong — most often
			// an override whose premise has gone. Saying "upstream has no such
			// project" about it would send the reader to the wrong question,
			// which is what the first draft of this arm did.
			fmt.Fprintf(out, "✗ %-30s cannot read the built recipe: %v\n", project, err)
			broken++
			bad++
			continue
		}
		if err != nil {
			// Not in the pantry at ALL. Upstream does not carry this project,
			// so the overlay's copy is the only recipe there is — bk builds it
			// from there (go-pkgx/bk#231) and a consumer resolves it from
			// there. There are no two halves to compare.
			//
			// There is still a way for it to go wrong, and it is the one that
			// cost four published, signed, unreachable packages: the recipe
			// has to be SERVED. A checkout can be right while what the
			// contents API returns is not — raw.githubusercontent holds a file
			// for minutes and nothing dislodges it — so this arm asks the API,
			// which is what the narrow mode used to exist for. It was reading
			// a list of project-creating patches; there are none left, and it
			// reported "0 projects, both halves in agreement" and passed.
			ours++
			if _, status, _, err := fetchOverlayRecipe(project); err != nil || status != 200 {
				fmt.Fprintf(out, "✗ %-30s upstream has no such project and the overlay does not SERVE it (%d %v)\n",
					project, status, err)
				broken++
				bad++
			}
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
		d, err := recipeDiffDoc(body, name, builtDoc, upstreamDoc(pantryDir, project))
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
	fmt.Fprintf(out, "\n%d of %d overlay project(s) are in both halves, %d disagree; %d are ours alone and served.\n",
		checked, len(projects), bad, ours)
	// A build-side difference is reported and does NOT fail: nothing reads a
	// build section out of the overlay — the factory compiles the pantry's
	// recipe with the overrides applied. 23 of the 25 disagreements are that,
	// they are our own override patches, and failing on them would mean
	// mirroring every patch into a second tree forever.
	//
	// A CONSUMER-side difference is another matter. It is what resolution
	// actually uses, the overlay WINS there, and a stale one is served. So
	// those fail, minus the handful this overlay exists to make.
	// Nothing in both halves, with an overlay that carries recipes, is not
	// agreement — it means the two trees do not line up at all. Asked FIRST,
	// before any per-project failure: a wrong `--pantry` sends every project
	// down the "ours alone" arm, and the reader needs to be told their paths
	// are wrong rather than handed 183 reports about a distribution.
	//
	// Eight of the 183 genuinely are ours alone, so the threshold is zero
	// rather than a proportion. If that ever stops being true this guard must
	// be revisited — and the message says what to check.
	// `broken == 0` too: a project that FAILED to read was examined, and
	// saying "not one was found" about it sends the reader to check their
	// paths instead of the failure. That is the third time this exact
	// conflation has had to be fixed — twice here and once in logicalise —
	// and every time it came from counting successes where the question is
	// about attempts.
	if checked == 0 && broken == 0 {
		fmt.Fprintln(out, "overlaycheck: not one overlay project was found in the pantry — are these the right trees?")
		return 2
	}
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

// recipeDiffDoc compares what a CONSUMER resolves with what the factory
// builds.
//
// The consumer's side is the overlay MERGED over upstream, not the overlay
// alone (go-pkgx/bottle#103). An entry states only what it changes now, so
// reading it on its own says a reduced entry has no `versions` and no
// `provides` — which is true of the FILE and false of the recipe. Comparing
// the file was right while every entry was a full copy, and became wrong the
// day they stopped being.
func recipeDiffDoc(overlay []byte, overlayName string, b, upstream map[string]any) (string, error) {
	a, err := recipeDoc(overlay, overlayName)
	if err != nil {
		return "", fmt.Errorf("the overlay's %s: %w", overlayName, err)
	}
	if upstream != nil {
		a = mergeOver(upstream, a)
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

// upstreamDoc reads the pantry's own copy, unoverridden — the baseline a
// consumer merges the overlay over. Nil when upstream does not carry it.
func upstreamDoc(pantryDir, project string) map[string]any {
	b, err := os.ReadFile(filepath.Join(pantryDir, "projects", filepath.FromSlash(project), "package.yml"))
	if err != nil {
		return nil
	}
	doc, err := recipeDoc(b, "package.yml")
	if err != nil {
		return nil
	}
	return doc
}

// mergeOver is bottle's rule: a key the overlay states replaces that key, a
// key it omits is inherited, and a list replaces rather than merges.
func mergeOver(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if om, ok := v.(map[string]any); ok {
			if bm, ok2 := out[k].(map[string]any); ok2 {
				out[k] = mergeOver(bm, om)
				continue
			}
		}
		out[k] = v
	}
	return out
}
