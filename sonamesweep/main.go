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
	"os"
	"os/exec"
	"sort"
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

// hostSlug is a seam too: the platform refusal has to be testable from
// whichever machine runs the suite.
var hostSlug = bottle.HostSlug

func run(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sonamesweep", flag.ContinueOnError)
	fs.SetOutput(stderr)
	catalog := fs.String("catalog", "", "the published catalogue to read the project list from (required)")
	chunk := fs.Int("chunk", 0, "which chunk to sweep, 0-based")
	chunks := fs.Int("chunks", 14, "how many chunks the project list is cut into")
	baseline := fs.String("baseline", "", "the references already known to be unresolvable; red only on a NEW one. Named and unreadable is an error, never an empty baseline")
	out := fs.String("out", "", "also write this chunk's findings here, in baseline format — the fourteen files a full sweep produces ARE the baseline")
	if err := fs.Parse(argv); err != nil {
		return 2
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
	osn, arch := hostSlug()
	if host := osn + "/" + arch; platform != host {
		// ⛔ NOT A WARNING. bk unresolved installs for the host, so the
		// answer would be about this machine wearing another platform's
		// name.
		fmt.Fprintf(stderr, "sonamesweep: that catalogue is for %s and this machine is %s — bk unresolved installs for the HOST, so the answer would be about the wrong platform\n", platform, host)
		return 2
	}

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
	bkOut, err := runCmd("bk", append([]string{"unresolved"}, mine...)...)
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
	if len(known) == 0 {
		return nil, fmt.Errorf("%s: no entries — an empty baseline makes every finding new", path)
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
