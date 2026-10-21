// Command vulnsweep asks the registry which platforms it publishes, and runs
// the vulnerability scan against every one of them.
//
// # WHY IT IS A PROGRAM AND NOT SIX STEPS
//
// ⛔ BECAUSE THE SIX WERE WRITTEN DOWN BY HAND, AND TWO OF THEM WERE MISSING.
// Until 2026-10-20 vulnscan.yml named `linux/x86-64` and `darwin/aarch64`,
// and the registry published six: the four nobody asked about were clean, so
// the cost was coverage rather than an exposure, but an enumeration copied
// into a file freezes while the population grows. Adding the four by hand
// closed that day's gap and left the mechanism in place.
//
// `pkgs catalog platforms` (go-pkgx/pkgx#119) is the half that makes the list
// derivable; this is the half that uses it. A seventh platform is scanned the
// day it is published, by nobody's decision.
//
//	vulnsweep -pantry <dir> -advisories advisories
//
// # WHAT IT REFUSES TO DO
//
// ⛔ REPORT A CLEAN SWEEP OVER NOTHING. Three ways that could happen and all
// three are an error here, not a zero: the platform list is empty, a platform
// is named whose catalogue will not fetch, and a scan that cannot run. This
// fleet has published a zero that meant "could not read", and a CVE lane is
// the worst place to do it again.
//
// It prints one line per platform and a total, so a failure names WHICH
// platform — the thing six separate CI steps gave for free and a single step
// has to earn.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// runCmd is the seam: a test drives this without pkgs, bk or a registry.
var runCmd = func(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	// ⛔ A child that reads stdin eats the caller's: the shell loop this
	// replaces had exactly that defect elsewhere in the fleet, where `unzip`
	// inside `while read` consumed the list and stopped after three of
	// sixty-two.
	cmd.Stdin = nil
	err := cmd.Run()
	return buf.String(), err
}

func run(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vulnsweep", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pantry := fs.String("pantry", "", "the pantry checkout bk reads recipes from (required)")
	advisories := fs.String("advisories", "advisories", "the advisory tree decisions are recorded in")
	store := fs.String("store", "", "PKGX_DIR the catalogues are fetched into (required)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *pantry == "" || *store == "" {
		fmt.Fprintln(stderr, "vulnsweep: -pantry and -store are required")
		return 2
	}

	plats, err := platforms()
	if err != nil {
		fmt.Fprintf(stderr, "vulnsweep: asking which platforms are published: %v\n", err)
		return 2
	}
	// Belt and braces: `pkgs catalog platforms` already refuses to print an
	// empty list, and a sweep over nothing must not be this program's answer
	// either if that ever changes.
	if len(plats) == 0 {
		fmt.Fprintln(stderr, "vulnsweep: no platform was named, so nothing would be scanned")
		return 2
	}

	fmt.Fprintf(stdout, "%d platform(s) published\n", len(plats))
	var failed []string
	for _, p := range plats {
		out, err := scan(p, *pantry, *advisories, *store)
		switch {
		case err == nil:
			fmt.Fprintf(stdout, "  ok   %-16s %s\n", p, lastLine(out))
		default:
			// ⛔ The output goes with the verdict. A platform that failed and
			// a platform that could not be fetched look identical in a tally.
			fmt.Fprintf(stdout, "  FAIL %-16s %v\n%s\n", p, err, indent(out))
			failed = append(failed, p)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(stderr, "vulnsweep: %d of %d platform(s) need attention: %s\n",
			len(failed), len(plats), strings.Join(failed, " "))
		return 1
	}
	fmt.Fprintf(stdout, "%d of %d platform(s) clean\n", len(plats), len(plats))
	return 0
}

// platforms asks the registry, through pkgs, which platforms it publishes.
func platforms() ([]string, error) {
	out, err := runCmd("pkgs", "catalog", "platforms")
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	var plats []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// Each line is handed back to --platform, so anything that is not
		// os/arch is not one: a stray note would become a platform nobody
		// publishes and a scan of whatever that produced.
		if line == "" || !strings.Contains(line, "/") {
			continue
		}
		plats = append(plats, line)
	}
	return plats, nil
}

// scan fetches one platform's catalogue and runs bk against it.
func scan(platform, pantry, advisories, store string) (string, error) {
	if out, err := runCmd("pkgs", "catalog", "update", "--platform", platform); err != nil {
		return out, fmt.Errorf("fetching the catalogue: %w", err)
	}
	out, err := runCmd("bk", "vulnfetch",
		"-dir", pantry,
		"-advisories", advisories,
		"-catalog", filepath.Join(store, "catalog", catalogFile(platform)),
		"-fail-on-new")
	if err != nil {
		return out, errors.New("a finding has no decision recorded in the advisory tree")
	}
	return out, nil
}

// catalogFile is the name `pkgs catalog update` writes for a platform:
// os/arch becomes os-arch.json. Kept here rather than guessed at the call
// site, because an almost-right path reads as "no catalogue" rather than as
// a typo.
func catalogFile(platform string) string {
	return strings.ReplaceAll(platform, "/", "-") + ".json"
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func indent(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("       " + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
