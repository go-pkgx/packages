// Command reduce turns each pantry-overlay entry into what it actually
// CHANGES, now that the overlay merges over upstream instead of replacing it
// (go-pkgx/bottle#103).
//
// Every entry is a full copy of an upstream recipe, taken once so that two or
// three lines could differ. That is a fork, and a fork drifts: `overlaycheck`
// reports 25 of the 183 disagreeing with the recipe the factory builds, all of
// them under keys nobody meant to change. A block you do not state cannot
// drift.
//
//	go run ./reduce -overlay <checkout>            # report
//	go run ./reduce -overlay <checkout> -write     # and rewrite
//
// TWO RULES, and both exist because of something that would otherwise be lost
// silently:
//
//   - the baseline is UPSTREAM, never the recipe the factory builds. A
//     consumer merges the overlay over upstream and never sees
//     packages/overrides, so comparing against the overridden recipe would
//     call curl.se's `dependencies` block redundant — our override says
//     openssl ^3 and so does the overlay — and dropping it would hand every
//     consumer upstream's ^1.1 back;
//
//   - a block carrying a COMMENT is never dropped. hclwrite removes a leading
//     comment along with the item it belongs to, measured rather than assumed,
//     and perl.org's explanation of why the published bottle NEEDs
//     libcrypt.so.1 is worth more than the bytes it costs. The check is
//     behavioural: remove it, and if any comment line went, put it back.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Seams: the filesystem and the reducer are indirected so every error branch
// is reachable from a test without contriving one on disk.
var (
	osExit       = os.Exit
	osWriteFile  = os.WriteFile
	reduceFileFn = reduceFile
	sameAfterFn  = sameAfterMerge
)

func main() {
	fs := flag.NewFlagSet("reduce", flag.ExitOnError)
	overlay := fs.String("overlay", "", "a pantry-overlay checkout")
	pantry := fs.String("pantry", "pantry", "a PRISTINE upstream pantry — what a consumer merges over")
	write := fs.Bool("write", false, "rewrite the files (the check runs either way)")
	_ = fs.Parse(os.Args[1:])
	if *overlay == "" {
		fmt.Fprintln(os.Stderr, "reduce: -overlay names the checkout")
		osExit(2)
		return
	}
	if code := run(*overlay, *pantry, *write, os.Stdout); code != 0 {
		osExit(code)
	}
}

func run(overlayDir, pantryDir string, write bool, out io.Writer) int {
	projects, err := overlayProjects(overlayDir)
	if err != nil {
		fmt.Fprintln(out, "reduce:", err)
		return 2
	}
	if len(projects) == 0 {
		fmt.Fprintf(out, "reduce: no recipe under %s — is that the overlay?\n", overlayDir)
		return 2
	}

	reduced, unchanged, kept, bad := 0, 0, 0, 0
	var before, after, commentsBefore, commentsAfter int
	for _, p := range projects {
		src, err := os.ReadFile(p.path)
		if err != nil {
			fmt.Fprintf(out, "✗ %-30s %v\n", p.project, err)
			bad++
			continue
		}
		up, err := upstreamDoc(pantryDir, p.project)
		if err != nil {
			// Upstream does not carry it: the overlay is the whole recipe and
			// there is nothing to reduce against.
			unchanged++
			continue
		}
		out2, held, err := reduceFileFn(src, filepath.Base(p.path), up)
		if err != nil {
			fmt.Fprintf(out, "✗ %-30s %v\n", p.project, err)
			bad++
			continue
		}
		kept += held
		before += len(src)
		after += len(out2)
		commentsBefore += len(comments(src))
		commentsAfter += len(comments(out2))
		if len(out2) == len(src) {
			unchanged++
			continue
		}
		// The property: what a consumer resolves must not move. Merging the
		// reduced entry over upstream has to give the document the full entry
		// gave.
		okDoc, err := sameAfterFn(src, out2, filepath.Base(p.path), up)
		if err != nil || !okDoc {
			fmt.Fprintf(out, "✗ %-30s reducing it changes what a consumer resolves: %v\n", p.project, err)
			bad++
			continue
		}
		reduced++
		if write {
			if err := osWriteFile(p.path, out2, 0o644); err != nil {
				fmt.Fprintf(out, "✗ %-30s %v\n", p.project, err)
				bad++
			}
		}
	}

	fmt.Fprintf(out, "\n%d entr(ies): %d reduced, %d already minimal or ours alone, %d failed\n",
		len(projects), reduced, unchanged, bad)
	fmt.Fprintf(out, "%d block(s) kept because a comment belongs to them\n", kept)
	fmt.Fprintf(out, "%d KiB → %d KiB\n", before/1024, after/1024)
	// Every comment must survive, counted rather than hoped for. Reporting
	// "0 kept for comments" says the protection never fired; only this says
	// it never needed to.
	fmt.Fprintf(out, "%d comment(s) before, %d after\n", commentsBefore, commentsAfter)
	if commentsAfter != commentsBefore {
		fmt.Fprintf(out, "reduce: %d comment(s) went missing — refusing\n", commentsBefore-commentsAfter)
		return 1
	}
	if bad > 0 {
		return 1
	}
	return 0
}

type entry struct{ project, path string }

func overlayProjects(dir string) ([]entry, error) {
	root := filepath.Join(dir, "projects")
	var out []entry
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Name() != "package.hcl" && d.Name() != "package.yml" {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(p))
		if rel == "." {
			return nil
		}
		out = append(out, entry{filepath.ToSlash(rel), p})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].project < out[j].project })
	return out, err
}

func upstreamDoc(pantryDir, project string) (map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(pantryDir, "projects", filepath.FromSlash(project), "package.yml"))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
