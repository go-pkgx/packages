// Command cargolocked generates the override patches that make a `cargo
// install` recipe honour the Cargo.lock its own release ships.
//
// `cargo install` WITHOUT `--locked` ignores that lock and re-resolves every
// dependency to the newest semver-compatible version on crates.io. The recipe
// does not change, the source does not change, and the build rots anyway —
// silently, on someone else's release schedule. crates.io/pqrs 0.3.2 is the
// case that showed it: it locks chrono 0.4.38, cargo picked 0.4.45, and
// chrono's Datelike had meanwhile grown a `quarter` method colliding with the
// one arrow-arith 51 defines in its own ChronoDateExt trait —
//
//	error[E0034]: multiple applicable items in scope
//	 90 |  DatePart::Quarter => |d| d.quarter() as i32,
//	    |                              ^^^^^^^ multiple `quarter` found
//
// — three years after both were released, in a build that had nothing to do
// with either.
//
// This is not one recipe's bug. 129 of the 149 crates.io recipes in the pantry
// already pass `--locked`; the rest are stragglers of the same convention, and
// each new unlocked recipe upstream adds is a future rot with no warning.
//
//	go run ./cargolocked            # write the patches
//	go run ./cargolocked -n         # list what would change, write nothing
//	go run ./cargolocked -prefix "" # widen past crates.io (see the caveat)
//
// The rewrite is deliberately narrow: it inserts ` --locked` immediately after
// the words `cargo install` on lines that lack it, and touches nothing else —
// not the indentation, not the YAML shape (`script:`, `- run:` and a bare list
// item all occur), not a second command chained after `&&`. A recipe it cannot
// rewrite that way is left alone and reported, because a wrong patch here is
// worse than a missing one.
//
// # The caveat, and why -prefix defaults to crates.io/
//
// `--locked` fails hard when there IS no Cargo.lock to read, so adding it to a
// recipe whose distributable ships none turns a working build into a broken
// one. That precondition cannot be checked from the recipe: it depends on the
// tarball. It was checked by hand for the twenty crates.io stragglers — every
// one of those repositories commits a Cargo.lock — which is why that is the
// default scope. Widening it is a measurement, not a flag: check the tarballs
// first, then pass -prefix.
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

// installLine matches a `cargo install` invocation that does not already pass
// --locked, capturing everything up to and including the two words so the
// rewrite can put the line back byte for byte but for the inserted flag.
//
// The negative lookahead Go's regexp does not have is done by a second check
// on the whole line (hasLocked), because `--locked` may sit anywhere after the
// verb: `cargo install --path . --locked` is as correct as `--locked --path .`
// and must not be patched twice.
var installLine = regexp.MustCompile(`^(.*\bcargo\s+install)(\s.*)$`)

// hasLocked reports whether a line already passes --locked.
func hasLocked(line string) bool { return strings.Contains(line, "--locked") }

// rewritable reports whether a line is a `cargo install` this tool may edit.
//
// Two exclusions, both found by reading the generated patches rather than by
// imagining them:
//
//   - A COMMENT. crates.io/bpb explains in prose why `cargo install bpb` does
//     not work; rewriting that sentence changes nothing and makes the patch
//     unproposable upstream.
//   - A line whose arguments come from a SHELL VARIABLE. crates.io/qsv runs
//     `cargo install $CARGO_ARGS` and sets `--locked` inside CARGO_ARGS, so
//     the flag is already there and the line cannot show it. Rewriting gave
//     `cargo install --locked $CARGO_ARGS`, i.e. the flag twice. What is not
//     on the line cannot be judged from the line, so these are reported and
//     left to a person.
func rewritable(line string) bool {
	m := installLine.FindStringSubmatch(line)
	if m == nil || hasLocked(line) {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return false
	}
	return !strings.Contains(m[2], "$")
}

// osExit and patchOne are seams: the first lets a test drive main() without
// killing the test binary, the second lets it drive the "this project could
// not be patched" path, which must NOT abort the whole run.
var (
	osExit   = os.Exit
	patchOne = patchFor
)

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("cargolocked", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pantry := fs.String("pantry", "pantry", "pantry checkout to read recipes from")
	out := fs.String("overrides", "overrides", "directory the patches are written to")
	prefix := fs.String("prefix", "crates.io/", "only projects whose name starts with this (see the caveat)")
	dry := fs.Bool("n", false, "report what would change, write nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	projects, deferred, err := unlocked(filepath.Join(*pantry, "projects"), *prefix)
	if err != nil {
		fmt.Fprintln(stderr, "cargolocked:", err)
		return 1
	}
	sort.Strings(projects)
	sort.Strings(deferred)
	for _, proj := range deferred {
		fmt.Fprintf(stderr, "cargolocked: %s installs with arguments from a variable — read it by hand\n", proj)
	}
	for _, proj := range projects {
		patch, err := patchOne(*pantry, proj)
		if err != nil {
			fmt.Fprintf(stderr, "cargolocked: skip %s: %v\n", proj, err)
			continue
		}
		name := filepath.Join(*out, strings.ReplaceAll(proj, "/", "-")+"-cargo-locked.patch")
		if *dry {
			fmt.Fprintln(stdout, name)
			continue
		}
		if err := os.WriteFile(name, []byte(patch), 0o644); err != nil {
			fmt.Fprintln(stderr, "cargolocked:", err)
			return 1
		}
		fmt.Fprintln(stdout, name)
	}
	fmt.Fprintf(stdout, "%d project(s) install a crate without its lock\n", len(projects))
	return 0
}

// unlocked lists the projects with at least one rewritable `cargo install`
// line, and separately those whose only unlocked install hides its arguments
// in a variable — which this tool must not touch, but a person should see.
func unlocked(projectsDir, prefix string) (out, deferred []string, err error) {
	err = filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "package.yml" {
			return err
		}
		// Walk always hands back a path under projectsDir, so the project name
		// is what is left once that prefix comes off.
		rel := filepath.ToSlash(strings.TrimPrefix(filepath.Dir(path), projectsDir+string(filepath.Separator)))
		if !strings.HasPrefix(rel, prefix) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var opaque bool
		for _, line := range strings.Split(string(b), "\n") {
			if rewritable(line) {
				out = append(out, rel)
				return nil
			}
			if installLine.MatchString(line) && !hasLocked(line) && strings.Contains(line, "$") {
				opaque = true
			}
		}
		if opaque {
			deferred = append(deferred, rel)
		}
		return nil
	})
	return out, deferred, err
}

// patchFor renders the unified diff for one project. It is written directly
// rather than shelled out to `git diff`: the change is a line, and the pantry
// is not always a git checkout (CI clones it shallow, the local repro harness
// mounts a copy).
func patchFor(pantry, proj string) (string, error) {
	rel := "projects/" + proj + "/package.yml"
	b, err := os.ReadFile(filepath.Join(pantry, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")
	var hit []int
	for i, line := range lines {
		if rewritable(line) {
			hit = append(hit, i)
		}
	}
	if len(hit) == 0 {
		return "", fmt.Errorf("no unlocked cargo install")
	}
	// A recipe can install more than once — a workspace with two binaries does
	// — so emit one hunk per changed line, merging ranges that would overlap.
	var out strings.Builder
	fmt.Fprintf(&out, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", rel, rel, rel, rel)
	for _, r := range ranges(hit, len(lines)) {
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", r.start+1, r.end-r.start, r.start+1, r.end-r.start)
		for i := r.start; i < r.end; i++ {
			m := installLine.FindStringSubmatch(lines[i])
			if m == nil || !rewritable(lines[i]) {
				fmt.Fprintf(&out, " %s\n", lines[i])
				continue
			}
			fmt.Fprintf(&out, "-%s\n+%s --locked%s\n", lines[i], m[1], m[2])
		}
	}
	return out.String(), nil
}

// hunkRange is a half-open [start, end) line span of a patch hunk.
type hunkRange struct{ start, end int }

// ranges turns changed line indexes into hunk spans with three lines of
// context either side (what `git diff -U3` writes), merging spans that touch
// so a hunk never repeats a line — a patch that lists the same context twice
// is rejected.
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
