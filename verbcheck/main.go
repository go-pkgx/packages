// Command verbcheck refuses a workflow that asks `pkgx` for a verb that
// belongs to `pkgs`.
//
// # WHY IT EXISTS
//
// go-pkgx/pkgx#113 moved nine verbs — ls, search, why, store, browse,
// catalog, env, compat, completion — out of `pkgx` and into a second binary,
// so that the first word after `pkgx` is a PACKAGE again, as it is upstream.
// `pkgx ls` now runs gnu.org/coreutils' ls.
//
// ⛔ THE SWEEP IS THE PART THAT WAS MISSED, NOT THE CHANGE. The split updated
// pkgx's own README, the landing site and the docs, and left one caller
// standing: vulnscan.yml ran `pkgx catalog update --platform`. The scheduled
// run of 2026-10-19 failed with
//
//	pkgx: "catalog" is a pkgs command, not a pkgx one: pkgs catalog
//
// which is pkgx's refusal working exactly as designed — and arriving five
// hours after the cron, on a job whose whole purpose is to be believed when
// it is green. A guard that reads the workflows costs one second.
//
//	go run ./verbcheck [dir...]      # default: .github/workflows
//
// It exits 2 if it read no files, for the reason every scanner in this tree
// does: a check that found nothing to check must not report success. That is
// not hypothetical here — a sweep of this very defect returned ZERO from the
// repository root on this machine, because the grep in use skips hidden
// directories and every workflow lives under .github.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// verbs are the nine. They are listed rather than imported because this is a
// different module from pkgx; the comment above names the pull request that
// is the source of truth, and the test below plants each one.
var verbs = []string{"ls", "search", "why", "store", "browse", "catalog", "env", "compat", "completion"}

// pkgxVerb matches `pkgx <verb>` as a COMMAND, not as prose.
//
// ⛔ IT DELIBERATELY MATCHES INSIDE COMMENTS TOO. A comment saying "what
// `pkgx ls` reads" is wrong in the same way the command is, and this
// repository's workflows carry more explanation than code — leaving the
// prose stale is how the next reader learns the old spelling. The word
// boundary is what keeps `pkgx lsof` and `pkgx envsubst` out.
var pkgxVerb = regexp.MustCompile(`\bpkgx\s+(` + strings.Join(verbs, "|") + `)\b`)

func main() {
	dirs := os.Args[1:]
	if len(dirs) == 0 {
		dirs = []string{filepath.Join(".github", "workflows")}
	}
	files, complaints := 0, 0
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".yml" && ext != ".yaml" {
				return nil
			}
			files++
			complaints += check(path)
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "verbcheck:", err)
			os.Exit(1)
		}
	}
	if files == 0 {
		// ⛔ NOT SUCCESS. A scanner that read nothing reports the same "no
		// problems" as one that read everything, and this defect was found
		// by a sweep that returned zero because it could not see .github.
		fmt.Fprintln(os.Stderr, "verbcheck: read no workflow at all — wrong directory?")
		os.Exit(2)
	}
	fmt.Printf("verbcheck: %d file(s), %d complaint(s)\n", files, complaints)
	if complaints > 0 {
		os.Exit(1)
	}
}

func check(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verbcheck:", err)
		return 1
	}
	n := 0
	for i, line := range strings.Split(string(b), "\n") {
		for _, m := range pkgxVerb.FindAllStringSubmatch(line, -1) {
			fmt.Fprintf(os.Stderr, "%s:%d: `pkgx %s` — that verb is pkgs's since go-pkgx/pkgx#113; write `pkgs %s`\n",
				path, i+1, m[1], m[1])
			n++
		}
	}
	return n
}
