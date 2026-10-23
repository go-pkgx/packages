// Command npmtag prints an npm package's dist-tags: which version each of
// `latest`, `previous`, `beta` … currently points at.
//
// # WHY THE FACTORY NEEDS IT
//
// ⛔ "REBUILD AT A FIXED VERSION" REQUIRES KNOWING THAT THE FIXED VERSION
// EXISTS, AND WHICH. On 2026-10-21 the CVE lane found three advisories
// against the `vitejs.dev` we publish (8.2.1). The affected range is
// `>= 8.2.0, < 8.2.4` — but the 8.3 line is affected too (`>= 8.3.0,
// < 8.3.3`), so "rebuild at the newest" without looking could have landed
// on 8.3.1 and stayed vulnerable. npm's `latest` was 8.3.5, above both
// boundaries, and that is what made the rebuild the right answer.
//
// # ⛔ AND IT IS HERE BECAUSE THE LAST ONE WAS NOT
//
// That check was written as a throwaway Go program in a scratchpad, and
// the note recording it said "tool: npmtag (20 lines of Go)" — as if it
// existed. On 2026-10-23 the same question came up for the same package
// and there was no npmtag: not on PATH, not in any repository. The
// scratchpad is not durable. So the answer was re-derived with curl, which
// is the loop this file ends.
//
// # IT PRINTS; IT DOES NOT JUDGE
//
// Whether a version is fixed is a statement about an ADVISORY's ranges,
// not about a dist-tag, and those ranges are per release line. A flag here
// that answered "is this fixed?" would be a second opinion beside OSV's,
// written in a place nobody would think to check. So this prints what the
// registry says and stops.
//
//	npmtag vite
//	npmtag @scope/pkg latest
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// httpGet is the seam: the tests need no network.
var httpGet = func(u string) (int, []byte, error) {
	c := &http.Client{Timeout: 20 * time.Second}
	resp, err := c.Get(u)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	// Bounded: this endpoint returns a few hundred bytes, and an unbounded
	// read of whatever answers is how a redirect to something large becomes
	// this process's memory.
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, err
}

func run(argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, "usage: npmtag <package> [dist-tag...]")
		return 2
	}
	pkg, want := argv[0], argv[1:]

	u, err := tagsURL(pkg)
	if err != nil {
		fmt.Fprintln(stderr, "npmtag:", err)
		return 2
	}
	code, body, err := httpGet(u)
	if err != nil {
		fmt.Fprintf(stderr, "npmtag: asking the registry about %s: %v\n", pkg, err)
		return 1
	}
	if code != http.StatusOK {
		// ⛔ A 404 here means "no such package", and a 500 means the
		// registry is having a bad day. Neither is "this package has no
		// versions", so neither prints an empty list.
		fmt.Fprintf(stderr, "npmtag: %s: the registry answered %d\n", pkg, code)
		return 1
	}
	var tags map[string]string
	if err := json.Unmarshal(body, &tags); err != nil {
		fmt.Fprintf(stderr, "npmtag: %s: the registry's answer is not a tag map: %v\n", pkg, err)
		return 1
	}
	if len(tags) == 0 {
		// Every published package has at least `latest`. None means the
		// answer was not about a package, and an empty print would read as
		// "it has no releases".
		fmt.Fprintf(stderr, "npmtag: %s: the registry named no dist-tag at all\n", pkg)
		return 1
	}

	names := want
	if len(names) == 0 {
		for t := range tags {
			names = append(names, t)
		}
		sort.Strings(names)
	}
	missing := 0
	for _, t := range names {
		v, ok := tags[t]
		if !ok {
			// ⛔ A tag that was ASKED FOR and is absent is reported, not
			// skipped: `npmtag vite next` printing nothing would read as
			// "next is empty" rather than "there is no next".
			fmt.Fprintf(stdout, "%-12s —\n", t)
			missing++
			continue
		}
		fmt.Fprintf(stdout, "%-12s %s\n", t, v)
	}
	if missing == len(names) {
		fmt.Fprintf(stderr, "npmtag: %s: none of the tag(s) asked for exists\n", pkg)
		return 1
	}
	return 0
}

// tagsURL builds the abbreviated endpoint's URL.
//
// ⛔ THE PACKAGE NAME GOES INTO A PATH, so it is escaped per SEGMENT and
// not as one string: a scoped name is `@scope/name` and its slash is real,
// while any other slash — or a `..` — is a caller reaching outside the
// endpoint this tool is for.
func tagsURL(pkg string) (string, error) {
	if pkg == "" {
		return "", fmt.Errorf("the package name is empty")
	}
	parts := strings.Split(pkg, "/")
	if len(parts) > 2 || (len(parts) == 2 && !strings.HasPrefix(parts[0], "@")) {
		return "", fmt.Errorf("%q is not an npm package name: only a scoped name has a slash in it", pkg)
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return "", fmt.Errorf("%q is not an npm package name", pkg)
		}
	}
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "https://registry.npmjs.org/-/package/" + strings.Join(parts, "/") + "/dist-tags", nil
}
