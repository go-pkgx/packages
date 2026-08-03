// Command closure expands a set of pkgx pantry projects to their transitive
// runtime-dependency closure for a target platform, printing them in
// topological order (every project's dependencies before it), one per line.
//
// The factory uses it so that building a package also builds its whole runtime
// closure into ghcr — deps first — and a consumer resolving the package from
// ghcr finds every dependency there too.
//
//	PANTRY=<dir> PLATFORM=linux/x86-64 closure lz4.org sqlite.org …
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-pkgx/bk/build"
	"github.com/go-pkgx/bk/pantry"
	"github.com/go-pkgx/bk/target"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "closure:", err)
		os.Exit(1)
	}
}

func run(projects []string, stdout, stderr *os.File) error {
	pantryDir := env("PANTRY", "pantry")
	osn, arch, _ := strings.Cut(env("PLATFORM", "linux/x86-64"), "/")
	tgt := target.Target{Platform: osn, Arch: arch}

	seen := map[string]bool{}
	var order []string
	var visit func(proj string)
	visit = func(proj string) {
		if seen[proj] {
			return
		}
		seen[proj] = true // mark first: breaks dependency cycles
		rec, err := loadRecipe(pantryDir, proj)
		if err != nil {
			// A dependency we have no recipe for can't be built by us — skip it
			// (it'll be resolved from upstream dist at build time), but note it.
			fmt.Fprintf(stderr, "closure: skip %s: %v\n", proj, err)
			return
		}
		for _, spec := range build.DepSpecs(rec.Dependencies, tgt) {
			visit(depName(spec))
		}
		order = append(order, proj) // post-order → deps precede dependents
	}
	for _, p := range projects {
		visit(p)
	}
	for _, p := range order {
		fmt.Fprintln(stdout, p)
	}
	return nil
}

// depName strips the version constraint from a "project@constraint" dep spec.
func depName(spec string) string {
	if i := strings.IndexByte(spec, '@'); i >= 0 {
		return spec[:i]
	}
	return spec
}

func loadRecipe(pantryDir, proj string) (*pantry.Recipe, error) {
	b, err := os.ReadFile(filepath.Join(pantryDir, "projects", proj, "package.yml"))
	if err != nil {
		return nil, err
	}
	return pantry.Parse(b)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
