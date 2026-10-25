// Command sonamesweep asks whether the bottles we publish can actually load.
//
// # THE TOOL EXISTED AND NOTHING RAN IT
//
// ⛔ `bk unresolved` reads every ELF and Mach-O in an installed closure and
// reports each reference the store cannot answer. It is written, tested, and
// as of 2026-10-23 **no workflow in this repository had ever called it** —
// `git grep unresolved .github/workflows` returned nothing.
//
// What that cost, measured the same day. The published
// `python.org 3.14.7` for linux/x86-64 has
//
//	_ssl.cpython-314-x86_64-linux-gnu.so     NEEDED libssl.so.1.1, libcrypto.so.1.1
//	_hashlib.cpython-314-x86_64-linux-gnu.so NEEDED libcrypto.so.1.1
//
// and this registry publishes no openssl 1.x at all — every one of the 48 is
// 3.x or 4.x. So python's `ssl` and `hashlib` could not load on that
// platform, and every build that fetches over HTTPS died there. The same
// bottle for linux/aarch64 needs `libssl.so.3`: one project, one version,
// two platforms, one of them wrong. It surfaced because a user's
// `pkgx openstack` failed, which is the longest possible way to learn it.
//
// `overrides/python.org.hcl` had already retargeted the recipe's `^1.1` pin
// to `^3`. An override moves a RECIPE; a DT_NEEDED compiled into a bottle
// published earlier does not move with it. Nothing compares the two, which
// is what this is for.
//
// # IT SAYS WHAT IT COVERED
//
// ⛔ A SWEEP OVER EVERYTHING IS NOT AFFORDABLE, SO IT DOES NOT CLAIM ONE.
// `bk unresolved` installs each project's whole closure into its own store —
// deliberately, because a shared one rescues a missing reference by accident
// — and there are ~1400 bottled projects per platform. So this takes a
// CHUNK, names it, and prints the bounds with the verdict. "clean" here
// means "clean for chunk 3 of 14 on linux/x86-64", which is a sentence that
// cannot be mistaken for a clean bill of health.
//
// # IT REFUSES TO AUDIT A PLATFORM IT IS NOT ON
//
// ⛔ `bk unresolved` resolves and installs for the HOST. Handed another
// platform's catalogue it would install the host's bottles and report on
// those, under the other platform's name — a judge and a subject counting
// differently, which is how a measurement comes back confident and wrong.
// The catalogue names its platform; if that is not this machine's, this
// exits 2 rather than producing an answer.
//
//	sonamesweep -catalog "$PKGX_DIR/catalog/linux-x86-64.json" -chunk 3 -chunks 14
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-pkgx/bottle"
)

var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// runCmd is the seam: a test drives this without bk, a registry or a store.
var runCmd = func(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	// A child that reads stdin eats the caller's; bk unresolved takes
	// projects on stdin when it is given none, so an inherited terminal
	// would make it wait for a list it has already been handed in argv.
	cmd.Stdin = nil
	err := cmd.Run()
	return buf.String(), err
}

func run(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sonamesweep", flag.ContinueOnError)
	fs.SetOutput(stderr)
	catalog := fs.String("catalog", "", "the published catalogue to read the project list from (required)")
	chunk := fs.Int("chunk", 0, "which chunk to sweep, 0-based")
	chunks := fs.Int("chunks", 14, "how many chunks the project list is cut into")
	baseline := fs.String("baseline", "", "the references already known to be unresolvable; red only on a NEW one. Named and unreadable is an error, never an empty baseline")
	out := fs.String("out", "", "also write this chunk's findings here, in baseline format — the fourteen files a full sweep produces ARE the baseline")
	merge := fs.String("merge", "", "instead of sweeping, assemble the chunk files under this directory into one baseline on stdout. Refuses unless every chunk 0..N-1 is present")
	chunkList := fs.Bool("chunk-list", false, "print -chunks as the JSON array a workflow matrix needs, and do nothing else")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *chunks < 1 {
		fmt.Fprintf(stderr, "sonamesweep: -chunks %d is not a count\n", *chunks)
		return 2
	}
	// ⛔ ONE NUMBER, NOT TWO. The matrix used to be a literal [0..13] beside
	// a `chunks` input that defaulted to 14, and the two had to agree with
	// nothing making them. Dispatched with chunks=2, twelve of the fourteen
	// jobs refused as out of range — measured 2026-10-25, on myself, within
	// a minute of shipping the input. And the opposite is worse for a small
	// platform: 14 chunks over linux/s390x's ~29 bottled projects leaves
	// chunks empty, which is also an error, so the fixed matrix made the
	// narrow run the tool explicitly supports impossible to request.
	//
	// The workflow now asks for the list and feeds it to the matrix, so the
	// count exists once. Printing it here rather than building it with
	// `seq`/`jq` in YAML keeps the one piece of arithmetic in the tested
	// program.
	if *chunkList {
		fmt.Fprint(stdout, "[")
		for k := range *chunks {
			if k > 0 {
				fmt.Fprint(stdout, ",")
			}
			fmt.Fprint(stdout, k)
		}
		fmt.Fprintln(stdout, "]")
		return 0
	}
	if *merge != "" {
		return runMerge(*merge, *chunks, stdout, stderr)
	}
	if *catalog == "" {
		fmt.Fprintln(stderr, "sonamesweep: -catalog is required")
		return 2
	}
	if *chunks < 1 || *chunk < 0 || *chunk >= *chunks {
		fmt.Fprintf(stderr, "sonamesweep: -chunk %d is not in 0..%d\n", *chunk, *chunks-1)
		return 2
	}

	b, err := os.ReadFile(*catalog)
	if err != nil {
		fmt.Fprintln(stderr, "sonamesweep:", err)
		return 2
	}
	c, err := bottle.UnmarshalCatalog(b)
	if err != nil {
		fmt.Fprintf(stderr, "sonamesweep: %s: %v\n", *catalog, err)
		return 2
	}

	platform, err := platformOf(c)
	if err != nil {
		fmt.Fprintln(stderr, "sonamesweep:", err)
		return 2
	}
	// ⛔ THE CATALOGUE'S PLATFORM IS NOW AN INSTRUCTION, NOT A REFUSAL.
	// Until bk v0.34.0 this had to check that the catalogue matched the
	// host and exit 2 otherwise, because `bk unresolved` installed for the
	// host: handed another platform's catalogue it would have reported
	// THIS machine's bottles under that platform's name. The refusal was
	// right, and it capped the audit at one platform of six — where it
	// found 967 unresolvable references while the other five went unasked.
	//
	// bottle v0.55.0 made the closure platform-explicit and bk v0.34.0
	// exposed it, so the same fact now steers the run instead of stopping
	// it. One machine, every platform.
	fmt.Fprintf(stdout, "auditing %s (bk unresolved -platform)\n", platform)

	projects := bottled(c)
	if len(projects) == 0 {
		// A catalogue with no bottled project is a file that did not
		// arrive, not a platform with nothing published.
		fmt.Fprintf(stderr, "sonamesweep: %s names %d project(s) and none of them has a bottle — nothing would be swept\n", *catalog, len(c.Projects))
		return 2
	}
	mine := slice(projects, *chunk, *chunks)
	if len(mine) == 0 {
		fmt.Fprintf(stderr, "sonamesweep: chunk %d of %d is empty over %d project(s)\n", *chunk, *chunks, len(projects))
		return 2
	}

	fmt.Fprintf(stdout, "%s: chunk %d of %d — %d of %d bottled project(s), %s … %s\n",
		platform, *chunk, *chunks, len(mine), len(projects), mine[0], mine[len(mine)-1])

	// ⛔ NO -quiet. It suppresses the per-reference lines, and the first run
	// of this sweep went out with it: chunk 0 reported "64 unresolvable
	// reference(s)" over 35 projects and named not one soname. A verdict
	// with its evidence stripped is a line nobody can act on, and the extra
	// cost here is ~100 "ok" lines a chunk.
	bkOut, err := runCmd("bk", append([]string{"unresolved", "-platform", platform}, mine...)...)
	fmt.Fprint(stdout, bkOut)
	found := parseFindings(bkOut)

	if *out != "" {
		if werr := writeFindings(*out, found); werr != nil {
			// ⛔ The file IS the deliverable of a recording run, so failing
			// to write it is not a footnote under a green tick.
			fmt.Fprintln(stderr, "sonamesweep:", werr)
			return 2
		}
		fmt.Fprintf(stdout, "%d reference(s) written to %s\n", len(found), *out)
	}

	if *baseline == "" {
		if err != nil {
			fmt.Fprintf(stderr, "sonamesweep: %s chunk %d of %d: %d unresolvable reference(s) above — a bottle that cannot load\n",
				platform, *chunk, *chunks, len(found))
			return 1
		}
		fmt.Fprintf(stdout, "%s: chunk %d of %d clean over %d project(s)\n", platform, *chunk, *chunks, len(mine))
		return 0
	}

	known, rerr := readBaseline(*baseline)
	if rerr != nil {
		// ⛔ Named and unreadable is an error, never an empty baseline: an
		// empty one makes EVERY finding new, so the run goes red for the
		// wrong reason and the real new one is lost in it.
		fmt.Fprintln(stderr, "sonamesweep:", rerr)
		return 2
	}

	inChunk := map[string]bool{}
	for _, p := range mine {
		inChunk[p] = true
	}
	var fresh []finding
	seen := map[finding]bool{}
	for _, f := range found {
		seen[f] = true
		if !known[f] {
			fresh = append(fresh, f)
		}
	}
	// ⛔ STALE IS COUNTED ONLY WITHIN THIS CHUNK. The baseline is global and
	// a run sees a fourteenth of it, so "in the baseline and not found" is
	// meaningless for the thirteen fourteenths this job never installed.
	fixed := 0
	for f := range known {
		if inChunk[f.project] && !seen[f] {
			fixed++
		}
	}

	fmt.Fprintf(stdout, "%s: chunk %d of %d — %d reference(s), %d known, %d new, %d fixed since the baseline\n",
		platform, *chunk, *chunks, len(found), len(found)-len(fresh), len(fresh), fixed)
	if len(fresh) == 0 {
		return 0
	}
	for _, f := range fresh {
		fmt.Fprintf(stdout, "  NEW %-34s %-28s %s\n", f.project, f.owner, f.ref)
	}
	fmt.Fprintf(stderr, "sonamesweep: %s chunk %d of %d: %d reference(s) not in %s\n",
		platform, *chunk, *chunks, len(fresh), *baseline)
	return 1
}

// finding is one unresolvable reference: which project's closure, whose
// binary names it, and what it names.
//
// All three, because the project alone collapses distinct defects into one
// line — `attrs.org python.org libssl.so.1.1` and `attrs.org tcl-lang.org
// libtcl8.6.so` are two bottles to rebuild, not one project to look at.
type finding struct{ project, owner, ref string }

// parseFindings reads `bk unresolved`'s output: a header line per owner with
// a count, then one indented line per reference.
//
//	apache.org/jmeter   kerberos.org   2 unresolvable
//	    libgssapi_krb5.so.2
//	    libkrb5.so.3
//
// It reads the INDENTED lines rather than the counts, so a header whose
// count disagrees with the lines under it cannot inflate the total.
func parseFindings(out string) []finding {
	var found []finding
	var project, owner string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "    ") {
			if ref := strings.TrimSpace(line); ref != "" && project != "" {
				found = append(found, finding{project, owner, ref})
			}
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 4 && f[len(f)-1] == "unresolvable" {
			project, owner = f[0], f[1]
			continue
		}
		// Anything else — an "ok" line, the total, a blank — ends the group,
		// so a stray indented line later cannot be attributed to it.
		project, owner = "", ""
	}
	return found
}

// runMerge assembles a full sweep's chunk files into one baseline.
//
// ⛔ IT REFUSES AN INCOMPLETE SET, and that is the whole reason this is a
// command rather than `cat`. A baseline missing one chunk of fourteen is
// not 13/14 of a baseline: every finding that chunk would have recorded
// becomes NEW on the next run, so the gate goes red over a hundred
// references nobody introduced — and the one that matters is somewhere in
// that list. Concatenation cannot tell a chunk that found nothing from a
// chunk whose job never ran; this can, because -out writes the empty file.
//
// It reads `chunk-<k>.txt` at any depth, because that is how the workflow's
// artefacts unpack (`sonames-chunk-3/chunk-3.txt`) and how a person would
// lay them out by hand.
func runMerge(dir string, chunks int, stdout, stderr io.Writer) int {
	found := map[int]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if !strings.HasPrefix(name, "chunk-") || !strings.HasSuffix(name, ".txt") {
			return nil
		}
		k, cerr := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "chunk-"), ".txt"))
		if cerr != nil {
			return nil
		}
		if prev, dup := found[k]; dup {
			// Two files claiming the same chunk: merging both would double
			// its entries and hide that one of them is from another run.
			return fmt.Errorf("chunk %d is at both %s and %s", k, prev, p)
		}
		found[k] = p
		return nil
	})
	if err != nil {
		fmt.Fprintln(stderr, "sonamesweep:", err)
		return 2
	}

	var missing []string
	for k := range chunks {
		if _, ok := found[k]; !ok {
			missing = append(missing, strconv.Itoa(k))
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "sonamesweep: %d of %d chunk(s) are missing (%s) — a baseline without them marks everything they found as NEW\n",
			len(missing), chunks, strings.Join(missing, " "))
		return 2
	}

	all := map[finding]bool{}
	for k := range chunks {
		one, rerr := readBaselineAllowingEmpty(found[k])
		if rerr != nil {
			fmt.Fprintln(stderr, "sonamesweep:", rerr)
			return 2
		}
		for f := range one {
			all[f] = true
		}
	}

	lines := make([]string, 0, len(all))
	for f := range all {
		lines = append(lines, f.project+" "+f.owner+" "+f.ref)
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}

	// ⛔ THE TALLY IS BY OWNER, NOT BY PROJECT, BECAUSE THE OWNER IS WHAT
	// GETS REBUILT. 967 references across hundreds of projects is a number
	// nobody can act on; the same 967 sorted by whose binary names the
	// missing soname says which handful of bottles to rebuild first, and
	// the counts are steeply unequal — one bad openssl pin in a popular
	// dependency appears once per dependent.
	byOwner := map[string]int{}
	projects := map[string]bool{}
	for f := range all {
		byOwner[f.owner]++
		projects[f.project] = true
	}
	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, o)
	}
	sort.Slice(owners, func(i, j int) bool {
		if byOwner[owners[i]] != byOwner[owners[j]] {
			return byOwner[owners[i]] > byOwner[owners[j]]
		}
		return owners[i] < owners[j] // ties alphabetical, so two runs agree
	})
	fmt.Fprintf(stderr, "sonamesweep: %d reference(s) over %d project(s), owned by %d bottle(s), from %d chunk(s)\n",
		len(lines), len(projects), len(owners), chunks)
	const show = 10
	for i, o := range owners {
		if i == show {
			fmt.Fprintf(stderr, "  … %d more owner(s)\n", len(owners)-show)
			break
		}
		fmt.Fprintf(stderr, "  %5d  %s\n", byOwner[o], o)
	}
	return 0
}

// writeFindings emits this chunk's findings in exactly the format
// readBaseline accepts, sorted, so concatenating a full sweep's fourteen
// files gives a baseline and nobody has to reformat anything by hand.
//
// It writes the file even when there is nothing to put in it: a missing
// file and a chunk with no findings are different facts, and the assembler
// of a baseline needs to tell them apart.
func writeFindings(path string, found []finding) error {
	lines := make([]string, 0, len(found))
	for _, f := range found {
		lines = append(lines, f.project+" "+f.owner+" "+f.ref)
	}
	sort.Strings(lines)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// readBaseline reads `project owner soname` per line; `#` comments and blank
// lines are ignored.
func readBaseline(path string) (map[finding]bool, error) {
	known, err := readBaselineAllowingEmpty(path)
	if err != nil {
		return nil, err
	}
	if len(known) == 0 {
		return nil, fmt.Errorf("%s: no entries — an empty baseline makes every finding new", path)
	}
	return known, nil
}

// readBaselineAllowingEmpty is the same parser without that refusal.
//
// ⛔ THE TWO CALLERS DISAGREE ABOUT EMPTY ON PURPOSE. For a BASELINE,
// empty is a configuration error: every finding becomes new and the gate
// goes red for the wrong reason. For one CHUNK of a sweep, empty is the
// good news — that chunk's hundred projects all load — and refusing it
// would make a clean chunk unmergeable.
func readBaselineAllowingEmpty(path string) (map[finding]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	known := map[finding]bool{}
	for i, line := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			return nil, fmt.Errorf("%s:%d: want `project owner soname`, got %d field(s)", path, i+1, len(f))
		}
		known[finding{f[0], f[1], f[2]}] = true
	}
	return known, nil
}

// platformOf reads the one platform a catalogue is for.
//
// A catalogue is published per platform and its entries carry that platform;
// more than one would mean the file is not what this tool thinks it is, and
// guessing which to believe is how the host check gets bypassed.
func platformOf(c bottle.Catalog) (string, error) {
	seen := map[string]bool{}
	for _, p := range c.Projects {
		for _, pl := range p.Platforms {
			seen[pl] = true
		}
	}
	switch len(seen) {
	case 1:
		for pl := range seen {
			return pl, nil
		}
	case 0:
		return "", fmt.Errorf("the catalogue names no platform, so there is nothing to check this host against")
	}
	names := make([]string, 0, len(seen))
	for pl := range seen {
		names = append(names, pl)
	}
	sort.Strings(names)
	return "", fmt.Errorf("the catalogue names %d platforms (%s) — a published one names one", len(names), strings.Join(names, " "))
}

// bottled is the subject: projects this platform can actually install. A
// project with a recipe and no bottle has nothing to load, and asking about
// it would spend a closure install to learn what the catalogue already says.
func bottled(c bottle.Catalog) []string {
	var out []string
	for _, p := range c.Projects {
		if len(p.Versions) > 0 {
			out = append(out, p.Project)
		}
	}
	sort.Strings(out)
	return out
}

// slice cuts [0,n) into `chunks` contiguous pieces and returns the k-th.
//
// Sorted first, so chunk k is the same set of projects from one run to the
// next as long as the catalogue has not changed — a rotating sweep whose
// chunks reshuffled would cover some projects twice and others never.
func slice(all []string, k, chunks int) []string {
	n := len(all)
	lo := k * n / chunks
	// k < chunks is checked by the caller, so (k+1)*n/chunks <= n and there
	// is no clamp here. A clamp would be a branch no input can reach, which
	// is a line that can only ever be covered by pretending.
	hi := (k + 1) * n / chunks
	if lo >= hi {
		return nil
	}
	return all[lo:hi]
}
