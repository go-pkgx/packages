// Command seedpins checks that the seed order pins every base-toolchain
// member bk constrains, to the constraint bk itself states.
//
// The seed reached 77 of 77 on 2026-09-29 and was not a usable toolchain.
// The first attempt to build anything on top of it without --bootstrap died
// before it compiled a line:
//
//	pkgx: no version of gnu.org/gawk satisfies "~5.3" (available: 1)
//
// The seed had built gawk 5.4.1 — the newest — and bk pins the base
// toolchain to ~5.3 ON PURPOSE, because 5.4.1 mishandles autoconf's
// option-resolution scripts: 186 `/*#undef` where 5.3.2 writes 202
// `#define`, so a libpng built under it would be a libpng with no features
// (see bk's build.ToolchainGawk). The seed order names projects and not
// versions, so it took the latest and nothing said otherwise.
//
// "Complete" answered the wrong question. Every project in the order was
// published, and the one thing the order exists to produce — a toolchain
// other builds can use — did not work.
//
// The constraint is READ FROM bk, never copied. A second copy of "~5.3" in
// this repository would be right until bk moved, and then wrong silently,
// which is the same failure one layer up.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/go-pkgx/bk/build"
)

// osExit is a seam so a test can exercise main without ending the process,
// as overlaycheck does in this repository.
var osExit = os.Exit

func main() {
	// Its own FlagSet rather than the global one: a test that calls main()
	// twice would otherwise redefine a registered flag and panic.
	fs := flag.NewFlagSet("seedpins", flag.ContinueOnError)
	order := fs.String("order", "seed/order.txt", "the seed build order")
	if err := fs.Parse(os.Args[1:]); err != nil {
		osExit(2)
		return
	}
	osExit(run(*order, build.BaseToolchain(), os.Stdout, os.Stderr))
}

// run takes the toolchain rather than fetching it, for the same reason
// constrainedToolchain does: the refusal below is about a malformed entry,
// and a refusal that cannot be reached is one nobody has read.
func run(orderPath string, toolchain []string, stdout, stderr io.Writer) int {
	pinned, err := constrainedToolchain(toolchain)
	if err != nil {
		fmt.Fprintln(stderr, "seedpins:", err)
		return 2
	}
	named, err := readOrder(orderPath)
	if err != nil {
		fmt.Fprintln(stderr, "seedpins:", err)
		return 2
	}

	var wrong []string
	for _, proj := range sortedKeys(pinned) {
		want := pinned[proj]
		got, present := named[proj]
		switch {
		case !present:
			// Not a failure: the order need not build every toolchain member
			// (a machine may already have one). It is only wrong to build a
			// member and build a version the toolchain refuses.
			continue
		case got != want:
			wrong = append(wrong, fmt.Sprintf("  %s is in the order as %q and bk's base toolchain asks for %q\n    write: %s",
				proj, spec(proj, got), proj+want, spec(proj, want)))
		}
	}
	if len(wrong) > 0 {
		fmt.Fprintf(stderr, "%d seed order entr(y/ies) would build a toolchain version the toolchain refuses:\n", len(wrong))
		for _, w := range wrong {
			fmt.Fprintln(stderr, w)
		}
		return 1
	}
	fmt.Fprintf(stdout, "%d constrained base-toolchain member(s), every one the order builds is pinned as bk asks\n", len(pinned))
	return 0
}

// constrainedToolchain maps each base-toolchain project that carries a
// constraint onto it. A member with no constraint is not this check's
// business: any version of gnu.org/m4 will do, and saying so in the order
// would be a pin nobody asked for.
//
// It takes the list rather than calling build.BaseToolchain itself, so the
// refusal below can be exercised. That refusal guards against a bk entry
// that is a bare constraint with no project — it cannot happen today, and a
// check that cannot be tested is a check nobody has read.
func constrainedToolchain(toolchain []string) (map[string]string, error) {
	out := map[string]string{}
	for _, s := range toolchain {
		proj := build.SpecProject(s)
		if proj == s {
			continue
		}
		if proj == "" {
			return nil, fmt.Errorf("base toolchain entry %q has no project", s)
		}
		out[proj] = strings.TrimPrefix(s, proj)
	}
	return out, nil
}

// readOrder maps each project the order names onto its constraint, "" when
// it names none.
//
// The order's form is `project@constraint`, with the `@` and the operator
// AFTER it — `gnu.org/gawk@~5.3`. That is not a cosmetic choice: bk's own
// readOrder cuts a line at the first `@` and splitPin does the same for a
// dispatch, so `gnu.org/gawk~5.3` would be read as a PROJECT of that name
// and the closure gate would look for a recipe nobody has. The base
// toolchain writes the pkgspec form (`gnu.org/gawk~5.3`) instead, so the
// two are compared after the `@` is taken off.
func readOrder(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseOrder(f, path)
}

// parseOrder is readOrder over an open reader, so a reader that FAILS
// mid-file can be given to it. A scanner error that went unchecked would
// turn a truncated order into a shorter one and this check into agreement.
func parseOrder(r io.Reader, name string) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		proj := build.SpecProject(line)
		out[proj] = strings.TrimPrefix(strings.TrimPrefix(line, proj), "@")
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s names no project — an empty order agrees with everything", name)
	}
	return out, nil
}

// spec renders a project and its constraint the way the ORDER writes it, so
// the message shows the line to write rather than describing it.
func spec(proj, constraint string) string {
	if constraint == "" {
		return proj + " (no pin)"
	}
	return proj + "@" + constraint
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
