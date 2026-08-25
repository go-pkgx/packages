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
//	go run ./openssl3                         # write the build-side patches
//	go run ./openssl3 -overlay <dir>/projects # and the install-side recipes
//	go run ./openssl3 -n                      # list what would change, write nothing
//
// There are two halves and they are not interchangeable: `overrides/` patches
// the pantry bk BUILDS from, while pkgx resolves an INSTALL by fetching the
// recipe itself through PKGX_PANTRY_OVERLAY. Fixing only the build half leaves
// every CONSUMER of those packages broken.
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
// Every way of naming the 1.x line is equally stale, and the pantry uses six of
// them. Measured against the published registry with bottle's own semver
// (ParseVer(v).Satisfies(c)), rather than assumed:
//
//	^1.1 ^1 ^1.1.1 ^1.0.1 ^1.1.1k   caret, the common forms
//	1.1  1                          BARE — a bare version is a range too
//	~1   ~1.1                        tilde
//
// all admit only openssl 1.x, and no openssl 1.x is published. So the operator
// is optional and may be `^` or `~`.
//
// `>=1.1` is NOT in that set and must not be matched: it is unbounded above, so
// openssl 3.x satisfies it and the recipe already resolves. The same check said
// so, which is why it is excluded by the anchor rather than by hope.
var depLine = regexp.MustCompile(`^(\s+openssl\.org:\s*)(['"]?)([\^~]?1(?:\.[^\s'"#]*)?)(['"]?)(\s*(?:#.*)?)$`)

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
	overlay := fs.String("overlay", "", "ALSO write each corrected recipe whole into this pantry-overlay projects/ dir")
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
		if *overlay == "" {
			continue
		}
		if err := writeOverlay(*pantry, *overlay, proj); err != nil {
			fmt.Fprintln(stderr, "openssl3:", err)
			return 1
		}
		fmt.Fprintln(stdout, filepath.Join(*overlay, proj, "package.yml"))
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

// writeOverlay writes the corrected recipe WHOLE into a pantry-overlay tree.
// The two halves are not interchangeable: `overrides/` patches the pantry that
// bk BUILDS from, while pkgx resolves an INSTALL by fetching the recipe itself,
// through PKGX_PANTRY_OVERLAY. Fixing only the build half leaves every consumer
// of those packages broken at install time — measured three times over in the
// sovereign builder, where gnutls, p11-kit and linux-pam each died on
// "no version of openssl.org satisfies ^1.1" AFTER the build-side patches had
// been applied, because the dependency's own recipe still asked for it.
func writeOverlay(pantry, overlayDir, proj string) error {
	dest := filepath.Join(overlayDir, filepath.FromSlash(proj), "package.yml")
	// An overlay entry that is ALREADY corrected is left exactly as it is. Some
	// were written by hand and carry their reasoning in comments -- curl.se
	// explains why its openssl is ^3 and not '*' -- and regenerating over them
	// would trade that explanation for nothing. Only an entry still carrying a
	// stale pin (upstream drifted back) gets rewritten.
	if b, err := os.ReadFile(dest); err == nil && !hasStalePin(b) {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(pantry, "projects", filepath.FromSlash(proj), "package.yml"))
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if m := depLine.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + m[2] + want + m[4] + m[5]
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, []byte(strings.Join(lines, "\n")), 0o644)
}

// hasStalePin reports whether a recipe still pins openssl to a 1.x line.
// depLine is anchored per LINE, and Go's ^/$ match the whole text unless the
// multiline flag is set — matching against the file as one blob would never
// hit, and would silently declare every recipe already corrected.
func hasStalePin(recipe []byte) bool {
	for _, line := range strings.Split(string(recipe), "\n") {
		if depLine.MatchString(line) {
			return true
		}
	}
	return false
}
