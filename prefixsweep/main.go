// Command prefixsweep answers one question about the published set: does a
// bottle's content name its own version directory?
//
// ⛔ IT DECIDES WHETHER A BOTTLE CAN BE RENAMED. `bottle.InstallFor` installs
// a bottle under the directory the bottle CARRIES rather than under the
// version that was requested, and the reason given is that "absolute paths
// baked inside it point there". That premise decides go-zpkg/bk#386: if no
// bottle bakes its own prefix, a rebuild published as `3.3.11_1` can simply be
// installed under that name; if some do, renaming them is unsafe and the
// rebuild counter has to be decided before the build instead.
//
// fixup exists to remove exactly these paths — .pc/.cmake rewriting, libtool
// .la cleanup, an ELF RUNPATH rewriter — so the premise may describe a hazard
// that has since been closed. Measured here rather than argued.
//
// It installs each named project into a THROWAWAY store and greps the
// extracted tree for its own `v<version>` component. A hit is a file that
// would be wrong if the directory were renamed; the file is named, because
// "some bottles do" is not a worklist.
//
//	prefixsweep -platform darwin/aarch64 curl.se zlib.net …
//	prefixsweep -platform linux/x86-64 -list recipes.txt -n 20
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-zpkg/bottle"
)

var osExit = os.Exit

// The registry is reached through these two seams so the sweep's reporting can
// be driven without a network: which version to take, and putting it on disk.
//
// Everything interesting in this tool is in what it does with the tree
// AFTERWARDS — which needle it chooses, what it counts, what it refuses to
// call clean — and none of that should be testable only against ghcr.io.
// They hold the upstream functions directly rather than wrappers around them:
// a wrapper body is code that only a network call can execute, so it would sit
// uncovered forever and the 100% gate would be bought by excusing it.
var (
	pickVersion = bottle.PickVersionFor
	install     = bottle.InstallFor
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "prefixsweep:", err)
		osExit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs0 := flag.NewFlagSet("prefixsweep", flag.ContinueOnError)
	fs0.SetOutput(stderr)
	platform := fs0.String("platform", "", "os/arch to sweep, e.g. darwin/aarch64 (required)")
	list := fs0.String("list", "", "file of project names, one per line, '#' comments ignored")
	max := fs0.Int("n", 0, "stop after this many projects (0 = all)")
	store := fs0.String("store", "", "install into this directory instead of a temporary one")
	needle := fs0.String("needle", "", "sweep for this literal (e.g. the builder's cache root) instead of the package's own version directory; any hit fails the run")
	absolute := fs0.Bool("absolute", false, "sweep for ANY absolute path outside an allowlist of platform paths, instead of a literal; any hit fails the run. A spelled -needle certifies only the lane it was written for — this asserts the property, so a lane nobody anticipated fails rather than passes")
	compiledOnly := fs0.Bool("compiled", false, "with -absolute, search only artefacts a path is COMPILED into (bin/, *.so*, *.dylib, *.a). Measured over six linux bottles: 38 hits become 8, the 30 dropped are all documentation — man-page examples and header comments — and not one of them is a builder path")
	constraint := fs0.String("constraint", "*", "which build to sweep, as a pkgx constraint (`=3.3.11` for one exact build). The default sweeps the newest, which is what a user gets; an exact pin is how to ask whether a REBUILD came out clean, since the newest is by then the one you are trying to replace")
	if err := fs0.Parse(args); err != nil {
		return err
	}
	if *compiledOnly && !*absolute {
		return fmt.Errorf("-compiled narrows -absolute, so it needs -absolute")
	}
	if *needle != "" && *absolute {
		// They answer different questions and the stricter one would hide the
		// other's result: -needle says "this string is gone", -absolute says
		// "no builder path of any shape is left".
		return fmt.Errorf("-needle and -absolute ask different questions; run them separately")
	}
	if *platform == "" {
		return fmt.Errorf("-platform is required (os/arch)")
	}
	osn, arch, ok := strings.Cut(*platform, "/")
	if !ok {
		return fmt.Errorf("bad -platform %q, want os/arch", *platform)
	}

	projects := fs0.Args()
	if *list != "" {
		named, err := readList(*list)
		if err != nil {
			return err
		}
		projects = append(projects, named...)
	}
	if len(projects) == 0 {
		return fmt.Errorf("name at least one project, or pass -list")
	}
	if *max > 0 && len(projects) > *max {
		projects = projects[:*max]
	}

	dir := *store
	if dir == "" {
		d, err := os.MkdirTemp("", "prefixsweep-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(d)
		dir = d
	}

	// ⛔ SAY WHAT WAS SEARCHED FOR, BEFORE ANY RESULT. A clean run is only
	// readable if the reader can see the question: `-needle /home/runner`
	// printed "4 clean" against four linux bottles that were full of builder
	// paths, because the lane builds with HOME=/github/home.
	if *absolute {
		reportAllowed(stdout)
	}

	var swept, baked, unreadable int
	for _, project := range projects {
		v, err := pickVersion(project, *constraint, osn, arch)
		if err != nil {
			fmt.Fprintf(stdout, "SKIP      %-34s %v\n", project, err)
			unreadable++
			continue
		}
		if _, err := install(bottle.Resolved{Project: project, Version: v}, dir, osn, arch); err != nil {
			fmt.Fprintf(stdout, "UNREAD    %-34s %v\n", project, err)
			unreadable++
			continue
		}
		// ⛔ THE NEEDLE COMES FROM WHAT LANDED, NOT FROM WHAT WAS ASKED FOR.
		// A bottle is installed under the directory it CARRIES, which is not
		// always the requested version — a rebuild published as `3.3.11_1`
		// unpacks to `v3.3.11`. Searching for `v3.3.11_1` in a tree that calls
		// itself `v3.3.11` finds nothing and reports "clean", which is a scan
		// that could not read reporting zero. fftw.org did exactly that here
		// before this line existed.
		home := filepath.Join(dir, filepath.FromSlash(project))
		var hits []hit
		switch {
		case *absolute:
			hits, err = absoluteHitsIn(home, *compiledOnly)
		case *needle != "":
			// ⛔ A CALLER-SUPPLIED NEEDLE REPLACES THE VERSION-DIRECTORY ONE. That
			// one cannot see a builder path lacking the package's own directory,
			// and a path normalised to /build/... no longer contains it at all, so
			// re-running the default sweep on a fixed bottle would print a vacuous
			// zero (go-zpkg/bk#387).
			hits, err = findNeedle(home, []byte(*needle))
		default:
			var component string
			component, err = installedComponent(home)
			if err == nil {
				hits, err = bakedPrefixes(home, component)
			}
		}
		if err != nil {
			fmt.Fprintf(stdout, "UNREAD    %-34s %v\n", project, err)
			unreadable++
			continue
		}
		swept++
		if len(hits) == 0 {
			fmt.Fprintf(stdout, "clean     %-34s %s\n", project, v.Raw)
			continue
		}
		baked++
		fmt.Fprintf(stdout, "⛔ BAKED  %-34s %s — %d file(s)\n", project, v.Raw, len(hits))
		for i, h := range hits {
			if i == 5 {
				fmt.Fprintf(stdout, "            … and %d more\n", len(hits)-5)
				break
			}
			fmt.Fprintf(stdout, "            %s\n                -> %s\n", h.Rel, h.Context)
		}
	}

	// ⛔ THE TOTALS ALWAYS, INCLUDING THE ZEROS, and "could not be read" never
	// folded into "clean": a bottle that failed to install has told us nothing
	// about whether it bakes its prefix, and counting it as clean would be a
	// census that could not read reporting zero.
	fmt.Fprintf(stdout, "\n%d swept · %d bake their own prefix · %d clean · %d unreadable\n",
		swept, baked, swept-baked, unreadable)
	if swept == 0 {
		return fmt.Errorf("no project was swept, so this measured nothing")
	}
	// With -needle or -absolute the sweep is an ASSERTION: any hit is a
	// failure, so "no builder path is shipped" is a test rather than an
	// observation.
	switch {
	case *absolute && baked > 0:
		return fmt.Errorf("%d project(s) bake an absolute path outside the allowlist", baked)
	case *needle != "" && baked > 0:
		return fmt.Errorf("%d project(s) contain %q", baked, *needle)
	}
	return nil
}

// installedComponent is the `v<something>` directory the bottle actually
// unpacked as — the real one, never a `v*`/`v3`/`v3.3` convenience symlink
// and never the alias a rebuild leaves behind.
//
// Taking it from the filesystem rather than from the requested version is the
// whole point: the two disagree exactly in the case this sweep exists to
// judge.
func installedComponent(home string) (string, error) {
	ents, err := os.ReadDir(home)
	if err != nil {
		return "", err
	}
	var found string
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, "v") || strings.HasPrefix(name, "v*") {
			continue
		}
		// A real directory, not one of the version aliases, which are symlinks.
		if e.Type()&fs.ModeSymlink != 0 || !e.IsDir() {
			continue
		}
		if found != "" {
			// Two real version directories means an earlier project in this
			// sweep shared the store. Refuse rather than pick one.
			return "", fmt.Errorf("%s holds both %s and %s; use a fresh -store", home, found, name)
		}
		found = name
	}
	if found == "" {
		return "", fmt.Errorf("%s has no unpacked version directory", home)
	}
	return found, nil
}

// hit is a file that names its own version directory, and the text that says
// so. The evidence travels with the finding: "this file matched" cannot be
// told apart from a false positive, and the whole point of this sweep is that
// a count nobody can check is worth nothing.
type hit struct {
	Rel     string
	Context string
}

// bakedPrefixes returns the files under root whose CONTENT holds a PATH
// ending in component — `…/v1.10.0/`, not the bare token `v1.10.0`.
//
// Only regular files, and the whole file is read: a prefix can appear in a
// .pc file, a libtool archive, a shell wrapper or an ELF's RUNPATH, and
// guessing which by extension is how a sweep misses the one that matters.
func bakedPrefixes(root, component string) ([]hit, error) {
	return findNeedle(root, []byte("/"+component+"/"))
}

// findNeedle returns the regular files under root whose content holds needle.
func findNeedle(root string, needle []byte) ([]hit, error) {
	var hits []hit
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			// Unreadable is not clean: say so by failing the project.
			return fmt.Errorf("%s: %w", path, rerr)
		}
		if i := bytes.Index(b, needle); i >= 0 {
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, hit{Rel: rel, Context: pathAround(b, i)})
		}
		return nil
	})
	return hits, err
}

// pathAround recovers the whole path the match sits in, so the report shows an
// absolute store path rather than the fragment that matched.
//
// ⛔ THE LEADING AND TRAILING SLASHES ARE THE DISCRIMINATOR. A baked prefix is
// a path COMPONENT: `/opt/zlib.net/v1.3.2/lib/libz.dylib`. A bare `v1.3.2` in
// a header comment or a `--version` string is not one, and an earlier form of
// this check — trailing delimiter only — would have counted it. Requiring a
// slash on both sides also settles `v1.2` inside `v1.20` for free.
func pathAround(b []byte, i int) string {
	start := i
	for start > 0 && isPathByte(b[start-1]) {
		start--
	}
	end := i
	for end < len(b) && isPathByte(b[end]) {
		end++
	}
	return string(b[start:end])
}

func isPathByte(c byte) bool {
	return c > 0x20 && c != 0x7f && c != '"' && c != '\'' && c != ';' && c != ','
}

func readList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}
