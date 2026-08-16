// Command openssl3 generates the override patches that retarget a stale
// `openssl.org: ^1.1` recipe pin at the openssl our registry actually carries.
//
// The problem is not one recipe. 117 pantry recipes pin openssl to a 1.x line;
// the registry holds 48 openssl bottles and every one of them is 3.x or 4.x, so
// each of those recipes dies before a single compiler runs:
//
//	resolve deps: no version of openssl.org satisfies "^1.1" (available: 48)
//
// Writing 117 patches by hand is how a systemic problem becomes a maintenance
// one, so this tool writes them — and can rewrite them when upstream moves. It
// emits one `overrides/<slug>-openssl3.patch` per project, matching the
// directory's convention (a `git diff` against the pantry root, one project per
// patch, ready to be proposed upstream as-is).
//
//	go run ./openssl3            # write the patches
//	go run ./openssl3 -n         # list what would change, write nothing
//
// The rewrite is deliberately narrow: it touches ONLY the constraint value on a
// dependency line naming openssl.org, and preserves the line's indentation,
// quoting style and trailing comment. Anything it cannot parse that way is left
// alone and reported, because a wrong patch here is worse than a missing one.
//
// What this does NOT do: prove those recipes build against openssl 3. It makes
// them RESOLVE. openssl 1.1.1 has been end-of-life since September 2023, so
// pinning it is not a real alternative — but a recipe that used a removed 1.1
// API will now fail in the compiler instead of in the resolver, which is the
// factory's job to measure, project by project.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// depLine matches a dependency line pinning openssl to the 1.1 line, capturing
// the indentation, the opening quote (if any) and any trailing comment, so the
// rewrite can put the line back exactly as it was but for the constraint.
// `^1` is as stale as `^1.1` — both admit only openssl 1.x, and there is no
// published openssl 1.x — so both are matched.
var depLine = regexp.MustCompile(`^(\s+openssl\.org:\s*)(['"]?)(\^1(?:\.[^\s'"#]*)?)(['"]?)(\s*(?:#.*)?)$`)

// want is what the constraint becomes: the major line our registry carries.
const want = "^3"

// osExit and patchOne are seams: the first lets a test drive main() without
// killing the test binary, the second lets it drive the "this project could not
// be patched" path, which is the one that must NOT abort the whole run.
var (
	osExit   = os.Exit
	patchOne = patchFor
)

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("openssl3", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pantry := fs.String("pantry", "pantry", "pantry checkout to read recipes from")
	out := fs.String("overrides", "overrides", "directory the patches are written to")
	dry := fs.Bool("n", false, "report what would change, write nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	projects, err := pinned(filepath.Join(*pantry, "projects"))
	if err != nil {
		fmt.Fprintln(stderr, "openssl3:", err)
		return 1
	}
	sort.Strings(projects)
	for _, proj := range projects {
		patch, err := patchOne(*pantry, proj)
		if err != nil {
			fmt.Fprintf(stderr, "openssl3: skip %s: %v\n", proj, err)
			continue
		}
		name := filepath.Join(*out, strings.ReplaceAll(proj, "/", "-")+"-openssl3.patch")
		if *dry {
			fmt.Fprintln(stdout, name)
			continue
		}
		if err := os.WriteFile(name, []byte(patch), 0o644); err != nil {
			fmt.Fprintln(stderr, "openssl3:", err)
			return 1
		}
		fmt.Fprintln(stdout, name)
	}
	fmt.Fprintf(stdout, "%d project(s) pin an openssl that does not exist\n", len(projects))
	return 0
}

// pinned lists the projects whose recipe pins openssl to the 1.1 line.
func pinned(projectsDir string) ([]string, error) {
	var out []string
	err := filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "package.yml" {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			if depLine.MatchString(line) {
				// Walk always hands back a path under projectsDir, so the project
				// name is simply what is left once that prefix comes off.
				rel := strings.TrimPrefix(filepath.Dir(path), projectsDir+string(filepath.Separator))
				out = append(out, filepath.ToSlash(rel))
				return nil
			}
		}
		return nil
	})
	return out, err
}

// patchFor renders the unified diff retargeting one project's pin. It is
// written directly rather than shelled out to `git diff`: the change is a
// single line, and the pantry is not always a git checkout (CI clones it
// shallow, the local harness mounts a copy).
func patchFor(pantry, proj string) (string, error) {
	rel := "projects/" + proj + "/package.yml"
	b, err := os.ReadFile(filepath.Join(pantry, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")
	var hit []int
	for i, line := range lines {
		if depLine.MatchString(line) {
			hit = append(hit, i)
		}
	}
	if len(hit) == 0 {
		return "", fmt.Errorf("no openssl pin")
	}
	// A recipe can pin openssl twice — crates.io/sccache pins it in both its
	// runtime and its build dependencies — so emit one hunk per changed line,
	// merging the ranges that would otherwise overlap into a single hunk.
	var b2 strings.Builder
	fmt.Fprintf(&b2, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", rel, rel, rel, rel)
	for _, r := range ranges(hit, len(lines)) {
		fmt.Fprintf(&b2, "@@ -%d,%d +%d,%d @@\n", r.start+1, r.end-r.start, r.start+1, r.end-r.start)
		for i := r.start; i < r.end; i++ {
			m := depLine.FindStringSubmatch(lines[i])
			if m == nil {
				fmt.Fprintf(&b2, " %s\n", lines[i])
				continue
			}
			fmt.Fprintf(&b2, "-%s\n+%s\n", lines[i], m[1]+m[2]+want+m[4]+m[5])
		}
	}
	return b2.String(), nil
}

// hunkRange is a half-open [start, end) line span of a patch hunk.
type hunkRange struct{ start, end int }

// ranges turns changed line indexes into hunk spans with three lines of context
// either side (what `git diff -U3` writes), merging spans that touch so a hunk
// never repeats a line — a patch that lists the same context twice is rejected.
func ranges(hit []int, n int) []hunkRange {
	var out []hunkRange
	for _, i := range hit {
		r := hunkRange{start: max(i-3, 0), end: min(i+4, n)}
		if len(out) > 0 && r.start <= out[len(out)-1].end {
			out[len(out)-1].end = r.end
			continue
		}
		out = append(out, r)
	}
	return out
}
