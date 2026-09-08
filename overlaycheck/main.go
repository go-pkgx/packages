// Command overlaycheck fails when a project that exists ONLY here has drifted
// from, or is missing in, the consumer half.
//
// The factory keeps a recipe in two places and they must agree:
//
//   - overrides/<name>-new.patch, applied to a fresh pkgxdev/pantry clone, is
//     what `bk` BUILDS from;
//   - go-pkgx/pantry-overlay/projects/<project>/package.yml is what a consumer
//     RESOLVES over HTTP (bottle's fetchRecipe tries the overlay, then upstream,
//     and nothing else).
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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
	dir := "overrides"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if code := run(dir, os.Stdout); code != 0 {
		osExit(code)
	}
}

// run compares every added project against the overlay and returns a process
// exit code.
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
		status, body, err := httpGet(overlayBase + "/" + project + "/package.yml?ref=main")
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
		case !sameRecipe(body, added[project]):
			fmt.Fprintf(out, "✗ %s: the two halves have drifted\n", project)
			bad++
		default:
			fmt.Fprintf(out, "✓ %s\n", project)
		}
	}
	if bad > 0 {
		fmt.Fprintf(out, "\n%d of %d projects disagree between the halves.\n", bad, len(names))
		return 1
	}
	fmt.Fprintf(out, "\n%d projects, both halves in agreement.\n", len(names))
	return 0
}

// sameRecipe compares two recipes ignoring trailing whitespace and a missing
// final newline — differences a copy between repositories can introduce and
// that change nothing about what either half reads.
func sameRecipe(a, b []byte) bool {
	return strings.TrimRight(string(a), "\n \t") == strings.TrimRight(string(b), "\n \t")
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
