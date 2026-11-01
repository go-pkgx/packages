package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-pkgx/bottle"
)

// ⛔ A RECIPE MAY DECLARE THE SAME VARIABLE IN TWO PLACES, AND AN OVERRIDE
// THAT PATCHES ONE OF THEM IS A HALF-FIX.
//
// gnu.org/glibc names its dynamic loader per architecture in `build.env`
// AND, separately, in `test.env`. The override for it was written against
// the build table, because that is where the failure was seen: on s390x
// $LDSO was empty, and
//
//	ln -sf ../lib/glibc-2.44/$LDSO ld.so
//
// made a symlink to a DIRECTORY. The test table has the same two entries and
// the same gap, one block further down, where an empty $LDSO turns
//
//	test -f "$LIBDIR/$LDSO" || { echo "missing $LDSO"; exit 1; }
//
// into a test of a directory that fails with `missing ` and nothing after it.
// So the package would have built on s390x and then failed its own test, and
// the second defect would have been diagnosed from scratch.
//
// Nothing compared the two tables. This does, for every override here — the
// same argument as comparing a pin's build side with its consumer side
// rather than trusting that whoever edited one edited the other.
func TestAnOverrideThatNamesAnArchForTheBuildNamesItForTheTest(t *testing.T) {
	for _, path := range overrideFiles(t) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			doc := overrideDoc(t, path)
			merge, ok := doc["merge"].(map[string]any)
			if !ok {
				return // nothing merged; `edits` and `patch` only
			}
			buildEnv := envOf(merge, "build")
			testEnv := envOf(merge, "test")
			if len(buildEnv) == 0 || len(testEnv) == 0 {
				// One side absent is not a mismatch: an override may have no
				// reason to touch the test block at all. The mismatch this
				// catches is a PER-ARCHITECTURE key in one and not the other,
				// which only means something once both exist.
				return
			}
			for _, arch := range archKeys(buildEnv) {
				if !slices.Contains(archKeys(testEnv), arch) {
					t.Errorf("merge.build.env sets %s and merge.test.env does not; "+
						"the recipe declares this variable twice and patching one table leaves the other empty", arch)
				}
			}
		})
	}
}

// ⛔ TestGlibcNamesALoaderForEveryArchItClaims is the specific statement,
// kept beside the general one because the general one passes vacuously if
// somebody deletes the s390x entry from BOTH tables.
//
// An empty loader name is not a missing feature, it is a bottle that builds,
// publishes and then fails in somebody else's build with "No such file or
// directory" against a path that plainly exists.
func TestGlibcNamesALoaderForEveryArchItClaims(t *testing.T) {
	doc := overrideDoc(t, filepath.Join("overrides", "gnu.org-glibc.hcl"))
	merge, ok := doc["merge"].(map[string]any)
	if !ok {
		t.Fatal("the glibc override merges nothing")
	}
	for _, block := range []string{"build", "test"} {
		env := envOf(merge, block)
		if env == nil {
			t.Errorf("merge.%s.env is absent, so %s has no loader for any architecture the recipe omits", block, block)
			continue
		}
		s390x, ok := env["s390x"].(map[string]any)
		if !ok {
			t.Errorf("merge.%s.env has no s390x entry", block)
			continue
		}
		// The s390x ABI calls it ld64.so.1, and it cannot be derived from the
		// other two names — which is why it was missed in the first place.
		if got := s390x["LDSO"]; got != "ld64.so.1" {
			t.Errorf("merge.%s.env.s390x.LDSO = %v, want ld64.so.1", block, got)
		}
	}
}

// overrideFiles is every HCL override in this directory.
func overrideFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("overrides", "*.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	// ⛔ A scan that reads nothing reports no findings. There are over two
	// hundred of these; a glob that matched none would make every test above
	// pass by doing nothing.
	if len(paths) == 0 {
		t.Fatal("no override files were found, so these tests assert nothing")
	}
	return paths
}

// overrideDoc parses one override through the SAME reader the factory uses.
// A second parser here would be two readings of one format, and the day they
// disagreed the test would pass on a file the builder reads differently.
func overrideDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := bottle.HCLToMap(b, path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

// envOf is merge.<block>.env, or nil.
func envOf(merge map[string]any, block string) map[string]any {
	b, ok := merge[block].(map[string]any)
	if !ok {
		return nil
	}
	env, _ := b["env"].(map[string]any)
	return env
}

// archKeys is the per-architecture keys of an env table, which are the ones
// whose value is itself a table. A plain `FOO = "bar"` beside them applies to
// every architecture and is not one.
//
// Lower-case so a variable name cannot be read as an architecture: recipes
// spell variables in capitals and pkgx spells architectures in lower case
// (x86-64, aarch64, s390x, riscv64, ppc64le, loong64).
func archKeys(env map[string]any) []string {
	var out []string
	for k, v := range env {
		if _, nested := v.(map[string]any); !nested {
			continue
		}
		if k != strings.ToLower(k) {
			continue
		}
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
