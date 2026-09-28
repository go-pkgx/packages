// Command logicalise converts the unified-diff overrides to the logical HCL
// form, and — the part that matters — proves the conversion changed nothing.
//
// A unified diff says WHERE lines are. It stops applying the moment upstream
// edits a neighbouring line, and it says "does not apply" whether the thing it
// edits has gone or upstream has already done the job. The logical form says
// WHAT changes, addresses it by key rather than by line, and tells those two
// apart. See go-pkgx/bk's `logical` package for the measurement that chose its
// verbs.
//
// The conversion is mechanical, so the only question worth asking is whether
// it is FAITHFUL:
//
//	go run ./logicalise           # both formats produce the same recipes, or fail
//	go run ./logicalise -write    # convert, then check
//
// Checking is what it does; -write additionally saves the result, and saves it
// only for the projects that checked out. The default is the CI gate. It clones a pristine pantry twice, applies the patches
// to one and the HCL overrides to the other, and compares the resulting recipe
// DOCUMENTS project by project. Anything that differs is named with the key it
// differs at.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/go-pkgx/bk/logical"
	"github.com/go-pkgx/bk/overrides"
	"gopkg.in/yaml.v3"
)

// Seams: the filesystem and the patch applier are indirected so every error
// branch is unit-testable without contriving an unwritable filesystem.
var (
	osExit         = os.Exit
	osMkdirTemp    = os.MkdirTemp
	osWriteFile    = os.WriteFile
	overridesApply = overrides.Apply
	copyTreeFn     = copyTree
	deriveFn       = logical.Derive
	emitFn         = logical.Emit
	// pantryURL is a seam too: a test must reach a local repository rather
	// than the network.
	pantryURL = "https://github.com/pkgxdev/pantry"
)

func main() {
	fs := flag.NewFlagSet("logicalise", flag.ExitOnError)
	dir := fs.String("dir", "overrides", "the override directory")
	pantry := fs.String("pantry", "", "a PRISTINE pantry checkout; cloned into a temporary one when blank")
	write := fs.Bool("write", false, "write the converted *.hcl files (the check runs either way)")
	_ = fs.Parse(os.Args[1:])

	if code := run(*dir, *pantry, *write, os.Stdout); code != 0 {
		osExit(code)
	}
}

// cloneCmd is a seam: the test must not reach the network.
var cloneCmd = func(dest string) error {
	c := exec.Command("git", "clone", "--depth", "1", pantryURL, dest)
	c.Stdout, c.Stderr = io.Discard, io.Discard
	return c.Run()
}

func run(dir, pantry string, write bool, out io.Writer) int {
	if pantry == "" {
		tmp, err := osMkdirTemp("", "pantry")
		if err != nil {
			fmt.Fprintln(out, "logicalise:", err)
			return 2
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		if err := cloneCmd(tmp); err != nil {
			fmt.Fprintln(out, "logicalise: clone:", err)
			return 2
		}
		pantry = tmp
	}

	// The patched half: a copy of the pristine tree with every *.patch applied,
	// which is what the factory builds from today.
	patched, err := copyTreeFn(pantry)
	if err != nil {
		fmt.Fprintln(out, "logicalise:", err)
		return 2
	}
	defer func() { _ = os.RemoveAll(patched) }()
	res, err := overridesApply(overrides.Options{Dir: dir, Root: patched})
	if err != nil {
		fmt.Fprintln(out, "logicalise:", err)
		return 2
	}
	if len(res.Skipped) > 0 {
		// Converting a patch that no longer applies would carry the rot into
		// the new format, wearing a clean face.
		fmt.Fprintf(out, "logicalise: %d patch(es) no longer apply; fix those first:\n", len(res.Skipped))
		for _, s := range res.Skipped {
			fmt.Fprintln(out, "  ", s)
		}
		return 1
	}

	changed, err := changedProjects(pantry, patched)
	if err != nil {
		fmt.Fprintln(out, "logicalise:", err)
		return 2
	}

	converted, bad := 0, 0
	for _, c := range changed {
		proj, before, after := c.project, c.before, c.after
		why := whyFor(dir, proj)
		ops := deriveFn(before, after, why)
		src := emitFn(&logical.Override{Project: proj, Why: why, Ops: ops})

		// Read the file back rather than trusting the operations in memory:
		// what gets committed is the FILE, and a conversion nobody can re-read
		// is a rewrite.
		reparsed, err := logical.Parse(src, proj+".hcl")
		if err != nil {
			fmt.Fprintf(out, "✗ %-34s the converted file does not parse: %v\n", proj, err)
			bad++
			continue
		}
		fresh := deepCopy(before)
		if _, err := logical.Apply(fresh, reparsed.Ops); err != nil {
			fmt.Fprintf(out, "✗ %-34s %v\n", proj, err)
			bad++
			continue
		}
		if !reflect.DeepEqual(fresh, after) {
			fmt.Fprintf(out, "✗ %-34s %s\n", proj, firstDifference(fresh, after))
			bad++
			continue
		}
		// And AGAIN, to the result. An override must be safe to apply to a
		// recipe that already has it, or nothing may ever apply one twice —
		// not a re-run, not two tools, and not the two formats side by side
		// while one replaces the other.
		//
		// This is not hypothetical. Derived without the rule, the cargo
		// --locked fix came out as `"l --"` → `"l --locked --"`, and "l --" is
		// still there afterwards inside "install --locked": 24 operations
		// across 22 projects compounded on a second pass, and the coexistence
		// this migration depends on would have corrupted them.
		if why, ok := notIdempotent(reparsed.Ops, after); !ok {
			fmt.Fprintf(out, "✗ %-34s applying it twice is not the same as once: %s\n", proj, why)
			bad++
			continue
		}
		converted++
		if write {
			if err := osWriteFile(filepath.Join(dir, slug(proj)+".hcl"), src, 0o644); err != nil {
				fmt.Fprintf(out, "✗ %-34s %v\n", proj, err)
				bad++
			}
		}
	}

	if converted+bad == 0 {
		// Nothing EXAMINED is not agreement — but it has two causes, and only
		// one of them is a fault.
		//
		// Once the migration is done the directory holds patches that CREATE a
		// project and nothing else; those are recipes of ours, not overrides,
		// and there is genuinely nothing to convert. A directory with no patch
		// AT ALL is the other case: a wrong path, or a clone that never
		// happened, which this reported as a clean run once already.
		//
		// Counted as converted+bad, not as converted: written the other way it
		// said "not one project was compared" for a run where one WAS compared
		// and failed, sending the reader to check their paths instead of the
		// failure. The same sentence with the same defect had to be fixed in
		// overlaycheck the day before.
		if len(res.Applied) == 0 {
			fmt.Fprintf(out, "logicalise: no patch in %s — is that an override directory?\n", dir)
			return 2
		}
		fmt.Fprintf(out, "%d patch(es), none of which modify an existing recipe: nothing to convert\n", len(res.Applied))
		return 0
	}
	fmt.Fprintf(out, "\n%d project(s) reproduce the unified diff exactly, %d do not\n", converted, bad)
	if bad > 0 {
		return 1
	}
	return 0
}

// notIdempotent applies the operations to a document that already has them and
// reports whether anything moved. Every outcome must be Redundant: an override
// that reports Applied the second time is one that would compound.
func notIdempotent(ops []logical.Op, done map[string]any) (string, bool) {
	again := deepCopy(done)
	res, err := logical.Apply(again, ops)
	if err != nil {
		return err.Error(), false
	}
	for _, r := range res {
		if r.Outcome != logical.Redundant {
			return fmt.Sprintf("%s reports %q, not redundant", r.Op.Path, r.Outcome), false
		}
	}
	// No document comparison after that loop: Redundant is DEFINED as having
	// changed nothing, and every verb that reports it returns before touching
	// the document. A comparison here could not fail, and a check that cannot
	// fail is not a check.
	return "", true
}

// slug turns a project name into a filename, matching the convention the patch
// directory already uses.
func slug(project string) string {
	return strings.ReplaceAll(project, "/", "-")
}

// whyFor is the reason a converted override carries.
//
// It cannot be invented: a unified diff has no field for one, and the reasons
// live in commit messages and pull requests. What the directory DOES have is
// descriptive filenames — rsync.samba.org-declare-libidn2.patch,
// llvm.org-darwin-rpath.patch — so the conversion carries those, and says
// plainly that it is carrying them rather than explaining anything.
func whyFor(dir, project string) string {
	paths, _ := filepath.Glob(filepath.Join(dir, slug(project)+"*.patch"))
	var names []string
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	sort.Strings(names)
	// The two GENERATED families know their own reason, and they are 151 of
	// the 242. Carrying a filename for those would throw away a fact the tool
	// that wrote them already stated.
	for _, n := range names {
		if r, ok := knownReason(n); ok {
			return r
		}
	}
	if len(names) == 0 {
		return "converted from the unified diff overrides; the reason is in the git history"
	}
	return "converted from " + strings.Join(names, ", ") +
		"; the reason is in the git history of those files"
}

// knownReason is what the generators say about what they generate.
//
// A `why` that reads "converted from a file" is a formality, and the field was
// added so an override could be DELETED when its reason stops holding. For the
// two families that were machine-written, the reason is known exactly.
func knownReason(patch string) (string, bool) {
	switch {
	case strings.HasSuffix(patch, "-openssl3.patch"):
		return "This recipe pins openssl to a 1.x line. Our registry holds no 1.x bottle — " +
			"every one of the 48 is 3.x or 4.x — so the recipe dies in the resolver before a " +
			"compiler runs: `no version of openssl.org satisfies \"^1.1\"`. openssl 1.1.1 has " +
			"been end-of-life since September 2023, so pinning it is not an alternative. " +
			"Generated by ./openssl3; delete this when upstream retargets the pin.", true
	case strings.HasSuffix(patch, "-cargo-locked.patch"), strings.HasSuffix(patch, "-locked.patch"):
		return "`cargo install` without --locked ignores the Cargo.lock the release ships and " +
			"re-resolves every dependency to the newest semver-compatible crate. The recipe " +
			"does not change, the source does not change, and the build rots anyway on someone " +
			"else's release schedule. Generated by ./cargolocked; delete this when upstream " +
			"adds --locked.", true
	}
	return "", false
}

func copyTree(src string) (string, error) {
	dst, err := osMkdirTemp("", "patched")
	if err != nil {
		return "", err
	}
	c := exec.Command("cp", "-R", src+"/.", dst)
	if out, err := c.CombinedOutput(); err != nil {
		return "", fmt.Errorf("copy pantry: %v: %s", err, out)
	}
	return dst, nil
}

// change is one project's two halves, read once.
type change struct {
	project       string
	before, after map[string]any
}

// changedProjects names every project whose recipe the patches altered, and
// hands back the documents it read. A project a patch CREATES is not an
// override at all — it is a recipe of ours — and is left out.
//
// Returning the documents rather than re-reading them in the caller is not
// only cheaper: the re-read carried two error branches that could fire only if
// the tree changed between the walk and the loop, which is a guard for a race
// nobody runs.
func changedProjects(pristine, patched string) ([]change, error) {
	root := filepath.Join(patched, "projects")
	var out []change
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "package.yml" {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(p))
		a, errA := loadDoc(filepath.Join(pristine, "projects", rel, "package.yml"))
		if errA != nil {
			return nil // created outright: not an override
		}
		b, errB := loadDoc(p)
		if errB != nil || reflect.DeepEqual(a, b) {
			return nil
		}
		out = append(out, change{filepath.ToSlash(rel), a, b})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].project < out[j].project })
	return out, err
}

// deepCopy leaves the document changedProjects read untouched, so a failed
// conversion cannot corrupt the input of the next one.
func deepCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch t := v.(type) {
		case map[string]any:
			out[k] = deepCopy(t)
		case []any:
			l := make([]any, len(t))
			for i, e := range t {
				if em, ok := e.(map[string]any); ok {
					l[i] = deepCopy(em)
					continue
				}
				l[i] = e
			}
			out[k] = l
		default:
			out[k] = v
		}
	}
	return out
}

func loadDoc(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// firstDifference names where two documents part, so a failure sends the
// reader to a key rather than to a diff of two whole recipes.
func firstDifference(a, b any) string {
	got := ""
	var walk func(x, y any, p string)
	walk = func(x, y any, p string) {
		if got != "" {
			return
		}
		am, ok1 := x.(map[string]any)
		bm, ok2 := y.(map[string]any)
		if ok1 && ok2 {
			keys := map[string]bool{}
			for k := range am {
				keys[k] = true
			}
			for k := range bm {
				keys[k] = true
			}
			var ks []string
			for k := range keys {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			for _, k := range ks {
				u, i1 := am[k]
				v, i2 := bm[k]
				if i1 != i2 {
					got = p + "." + k + ": present on one side only"
					return
				}
				walk(u, v, p+"."+k)
			}
			return
		}
		if !reflect.DeepEqual(x, y) {
			got = fmt.Sprintf("%s: %s != %s", p, trunc(x), trunc(y))
		}
	}
	walk(a, b, "")
	return got
}

func trunc(v any) string {
	s := strings.Join(strings.Fields(fmt.Sprint(v)), " ")
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}
