package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// calls records what the sweep asked for, so a test can assert that it asked
// about EVERY platform rather than merely exiting 0.
type calls struct {
	seen []string
	fail map[string]error
	out  map[string]string
	list string
	err  error
}

func (c *calls) install(t *testing.T) {
	t.Helper()
	prev := runCmd
	runCmd = func(name string, args ...string) (string, error) {
		key := name + " " + strings.Join(args, " ")
		c.seen = append(c.seen, key)
		if name == "pkgs" && len(args) == 2 && args[1] == "platforms" {
			return c.list, c.err
		}
		if err, bad := c.fail[key]; bad {
			return c.out[key], err
		}
		return c.out[key], nil
	}
	t.Cleanup(func() { runCmd = prev })
}

func (c *calls) asked(platform string) bool {
	for _, s := range c.seen {
		if strings.Contains(s, "vulnfetch") && strings.Contains(s, platform) {
			return true
		}
	}
	return false
}

const six = "darwin/aarch64\ndarwin/x86-64\nlinux/aarch64\nlinux/s390x\nlinux/x86-64\nwindows/x86-64\n"

// ⛔ THE WHOLE POINT: every platform the registry names is scanned, and the
// list is not written down anywhere. Until 2026-10-20 the lane named two of
// six by hand; the four it missed were clean, so nothing ever went red to
// say so.
func TestEveryPublishedPlatformIsScanned(t *testing.T) {
	c := &calls{list: six, out: map[string]string{}}
	c.install(t)
	var out, errb bytes.Buffer
	if code := run([]string{"-pantry", "p", "-store", "s"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d\n%s%s", code, out.String(), errb.String())
	}
	for _, p := range []string{
		"darwin-aarch64", "darwin-x86-64", "linux-aarch64",
		"linux-s390x", "linux-x86-64", "windows-x86-64",
	} {
		if !c.asked(p) {
			t.Errorf("%s was never scanned; asked: %v", p, c.seen)
		}
	}
	if !strings.Contains(out.String(), "6 of 6 platform(s) clean") {
		t.Errorf("the tally does not say what was covered:\n%s", out.String())
	}
}

// A failure names WHICH platform. Six separate CI steps gave that for free;
// one step has to earn it, and a tally that says "something failed" sends
// the reader to the logs to find out what.
func TestAFailureNamesThePlatform(t *testing.T) {
	c := &calls{list: six, out: map[string]string{}, fail: map[string]error{}}
	key := "bk vulnfetch -dir p -advisories advisories -catalog s/catalog/linux-s390x.json -fail-on-new"
	c.fail[key] = errors.New("exit 1")
	c.out[key] = "GHSA-xxxx affects something we publish"
	c.install(t)
	var out, errb bytes.Buffer
	if code := run([]string{"-pantry", "p", "-store", "s"}, &out, &errb); code != 1 {
		t.Fatalf("code=%d, want 1\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "linux/s390x") {
		t.Errorf("stderr does not name the platform: %q", errb.String())
	}
	// And the scan's own words travel with the verdict: "failed" and "could
	// not be fetched" are indistinguishable in a tally.
	if !strings.Contains(out.String(), "GHSA-xxxx") {
		t.Errorf("the finding's text was dropped:\n%s", out.String())
	}
	// ⛔ THE OTHER FIVE STILL RAN. A sweep that stops at the first red
	// platform reports one problem and hides four.
	if !c.asked("windows-x86-64") {
		t.Errorf("the sweep stopped early; asked: %v", c.seen)
	}
}

// ⛔ A SWEEP OVER NOTHING IS NOT A CLEAN SWEEP. Three ways to get there and
// all three are an error: an empty list, a list that is only noise, and a
// list that could not be obtained at all. A CVE lane is the worst possible
// place to print a reassuring zero.
func TestItRefusesToSweepNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		call *calls
	}{
		{"an empty list", &calls{list: "", out: map[string]string{}}},
		{"only blank lines", &calls{list: "\n  \n", out: map[string]string{}}},
		{"nothing that looks like os/arch", &calls{list: "note: nothing here\n", out: map[string]string{}}},
		{"the question itself failed", &calls{list: "boom", err: errors.New("exit 1"), out: map[string]string{}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.call.install(t)
			var out, errb bytes.Buffer
			if code := run([]string{"-pantry", "p", "-store", "s"}, &out, &errb); code != 2 {
				t.Errorf("code=%d, want 2\n%s%s", code, out.String(), errb.String())
			}
			if strings.Contains(out.String(), "clean") {
				t.Errorf("it reported a clean sweep over nothing:\n%s", out.String())
			}
		})
	}
}

// A catalogue that will not fetch is a platform NOT scanned, and must not
// pass as one that was.
func TestAPlatformWhoseCatalogueWillNotFetchFails(t *testing.T) {
	c := &calls{list: "linux/x86-64\n", out: map[string]string{}, fail: map[string]error{}}
	key := "pkgs catalog update --platform linux/x86-64"
	c.fail[key] = errors.New("exit 1")
	c.out[key] = "platform not in index"
	c.install(t)
	var out, errb bytes.Buffer
	if code := run([]string{"-pantry", "p", "-store", "s"}, &out, &errb); code != 1 {
		t.Errorf("code=%d, want 1", code)
	}
	if c.asked("linux-x86-64") {
		t.Error("it scanned a platform whose catalogue it never got")
	}
}

func TestUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != 2 {
		t.Errorf("no arguments = %d, want 2", code)
	}
	if code := run([]string{"-pantry", "p"}, &out, &errb); code != 2 {
		t.Errorf("without -store = %d, want 2", code)
	}
}

// An unparseable flag is a usage error, not a sweep of nothing.
func TestABadFlagIsRefused(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-nope"}, &out, &errb); code != 2 {
		t.Errorf("code=%d, want 2", code)
	}
}

// osExit is replaced in main so the binary can exit; naming it here keeps
// the coverage gate honest about the one line main() holds.
func TestMainExitsWithTheCode(t *testing.T) {
	prev := osExit
	prevArgs := os.Args
	defer func() { osExit, os.Args = prev, prevArgs }()
	got := -1
	osExit = func(code int) { got = code }
	os.Args = []string{"vulnsweep"} // no -pantry: a usage error, which is 2
	main()
	if got != 2 {
		t.Errorf("main() exited %d, want 2", got)
	}
}

// ⛔ THE REAL runCmd IS EXERCISED, not only the seam that replaces it. Every
// test above swaps it out, so without this the one piece that actually
// starts a process — and the stdin it deliberately closes — would be
// covered by nothing and asserted by nobody.
func TestRunCmdRunsAndReportsFailure(t *testing.T) {
	out, err := runCmd("go", "env", "GOOS")
	if err != nil {
		t.Fatalf("running a command that exists: %v (%s)", err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("the child's output was not captured")
	}
	// A command that is not there must come back as an error with its words,
	// because the sweep prints those words beside the platform that failed.
	if _, err := runCmd("vulnsweep-no-such-command-exists"); err == nil {
		t.Error("a missing command reported success")
	}
}

// The path a catalogue lands at is derived here, and an almost-right one
// reads as "no catalogue" rather than as a typo.
func TestCatalogFile(t *testing.T) {
	for in, want := range map[string]string{
		"linux/x86-64":   "linux-x86-64.json",
		"darwin/aarch64": "darwin-aarch64.json",
		"linux/arm/v7":   "linux-arm-v7.json",
	} {
		if got := catalogFile(in); got != want {
			t.Errorf("catalogFile(%q) = %q, want %q", in, got, want)
		}
	}
}
