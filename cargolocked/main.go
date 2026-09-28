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
	"io"
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

// excluded lists the projects where `--locked` is the WRONG answer, each with
// the measurement that says so. `--locked` is not free: it trades "a dependency
// drifted forward and broke" for "the lock is older than the toolchain and no
// longer compiles". Which one bites cannot be known from the recipe — only a
// build tells them apart — so this list grows by measurement, never by caution.
var excluded = map[string]string{
	// pueue's lock pins time 0.3.31, which does not compile with a modern rustc:
	//   error[E0282]: type annotations needed for `Box<_>`
	//     time-0.3.31/src/format_description/parse/mod.rs:83
	// Unlocked, cargo picks a time that builds. Every published pueue (3.2.0
	// through 3.4.0) failed the same way in the control run of 2026-09-01.
	"crates.io/pueue": "its lock pins time 0.3.31, which no longer compiles (E0282)",
}

// suffix names this tool's output.
const suffix = ".hcl"

// osExit and patchOne are seams: the first lets a test drive main() without
// killing the test binary, the second lets it drive the "this project could
// not be patched" path, which must NOT abort the whole run.
var (
	osExit      = os.Exit
	overrideOne = overrideFor
)

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
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
	written, shared := 0, 0
	for _, proj := range deferred {
		fmt.Fprintf(stderr, "cargolocked: %s installs with arguments from a variable — read it by hand\n", proj)
	}
	for _, proj := range projects {
		if why, ok := excluded[proj]; ok {
			fmt.Fprintf(stderr, "cargolocked: %s excluded — %s\n", proj, why)
			continue
		}
		name := filepath.Join(*out, strings.ReplaceAll(proj, "/", "-")+suffix)

		// A file already there may hold somebody else's work: a project's
		// operations live in one file so they are read together, and a
		// generator cannot own one it shares.
		if _, err := os.Stat(name); err == nil {
			ok, err := carriesTheFlag(name)
			if err != nil {
				fmt.Fprintf(stderr, "cargolocked: %s: %v\n", name, err)
				continue
			}
			if !ok {
				fmt.Fprintf(stderr, "cargolocked: %s exists and does NOT add --locked — edit it by hand\n", name)
				shared++
			}
			continue
		}

		src, err := overrideOne(*pantry, proj)
		if err != nil {
			fmt.Fprintf(stderr, "cargolocked: skip %s: %v\n", proj, err)
			continue
		}
		if *dry {
			fmt.Fprintln(stdout, name)
			continue
		}
		if err := os.WriteFile(name, src, 0o644); err != nil {
			fmt.Fprintln(stderr, "cargolocked:", err)
			return 1
		}
		fmt.Fprintln(stdout, name)
		written++
	}
	fmt.Fprintf(stdout, "%d of %d project(s) given an override; %d left alone\n", written, len(projects), len(projects)-written)
	if shared > 0 {
		fmt.Fprintf(stdout, "%d have an override written for another reason and must be edited by hand\n", shared)
	}
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
