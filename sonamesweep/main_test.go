package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The field names the FACTORY writes, taken from a published catalogue: it
// is `deps` and `versions`, not the names a reader might expect. catstat was
// about to report "0% have dependencies" over a catalogue in which half did,
// for exactly this reason.
const published = `{"generated":"2026-10-23T05:00:00Z","projects":[
 {"project":"python.org","versions":["3.14.7"],"platforms":["linux/x86-64"]},
 {"project":"curl.se","versions":["8.20.0"],"platforms":["linux/x86-64"]},
 {"project":"gnu.org/bash","versions":["5.3.0"],"platforms":["linux/x86-64"]},
 {"project":"zlib.net","versions":["1.3.2"],"platforms":["linux/x86-64"]},
 {"project":"aerc-mail.org","platforms":["linux/x86-64"]}
]}`

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// onLinuxX86 makes the host match the fixture's platform, so every test
// below is about the thing it names rather than about which machine the
// suite happens to run on. The s390x lane caught a fixture that assumed its
// host once already.
func onLinuxX86(t *testing.T) {
	t.Helper()
	prev := hostSlug
	hostSlug = func() (string, string) { return "linux", "x86-64" }
	t.Cleanup(func() { hostSlug = prev })
}

func stub(t *testing.T, f func(string, ...string) (string, error)) *[]string {
	t.Helper()
	var got []string
	prev := runCmd
	runCmd = func(name string, args ...string) (string, error) {
		got = append([]string{name}, args...)
		return f(name, args...)
	}
	t.Cleanup(func() { runCmd = prev })
	return &got
}

// ⛔ A SWEEP MUST SAY WHAT IT COVERED. "clean" over a chunk is a different
// claim from "clean", and only one of them is true here.
func TestItNamesTheChunkItSwept(t *testing.T) {
	onLinuxX86(t)
	argv := stub(t, func(string, ...string) (string, error) { return "", nil })

	var out, errb strings.Builder
	if code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "2"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d, want 0\n%s%s", code, out.String(), errb.String())
	}
	for _, want := range []string{
		"linux/x86-64: chunk 0 of 2",
		// Four of five projects carry a bottle; aerc-mail.org does not, and
		// asking about it would spend a closure install to learn what the
		// catalogue already says.
		"2 of 4 bottled project(s)",
		"chunk 0 of 2 clean over 2 project(s)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q from:\n%s", want, out.String())
		}
	}
	// And the subject is what it said it was.
	if got := *argv; len(got) != 5 || got[0] != "bk" || got[1] != "unresolved" || got[3] != "curl.se" {
		t.Fatalf("bk was not asked about a chunk of two: %q", got)
	}
}

// Sorted and contiguous: the two chunks together are every bottled project,
// once each. A rotating sweep whose chunks overlapped would cover some twice
// and others never, and would still print "clean" for each of them.
func TestTheChunksCoverEverythingOnce(t *testing.T) {
	onLinuxX86(t)
	seen := map[string]int{}
	for k := range 2 {
		stub(t, func(_ string, args ...string) (string, error) {
			for _, a := range args[2:] { // args[0] "unresolved", args[1] "-quiet"
				seen[a]++
			}
			return "", nil
		})
		var out, errb strings.Builder
		if code := run([]string{"-catalog", write(t, published), "-chunk", itoa(k), "-chunks", "2"}, &out, &errb); code != 0 {
			t.Fatalf("chunk %d: code=%d\n%s%s", k, code, out.String(), errb.String())
		}
	}
	want := []string{"curl.se", "gnu.org/bash", "python.org", "zlib.net"}
	if len(seen) != len(want) {
		t.Errorf("the chunks covered %d project(s), want %d: %v", len(seen), len(want), seen)
	}
	for _, p := range want {
		if seen[p] != 1 {
			t.Errorf("%s was swept %d time(s), want exactly 1", p, seen[p])
		}
	}
	// aerc-mail.org has no bottle and must never be installed.
	if seen["aerc-mail.org"] != 0 {
		t.Error("a project with no bottle was swept")
	}
}

// ⛔ THE REFUSAL THAT KEEPS THE ANSWER HONEST. bk unresolved installs for the
// HOST. Handed another platform's catalogue it would install this machine's
// bottles and report on them under the other platform's name — a judge and a
// subject counting differently.
func TestItRefusesAPlatformItIsNotOn(t *testing.T) {
	prev := hostSlug
	hostSlug = func() (string, string) { return "darwin", "aarch64" }
	t.Cleanup(func() { hostSlug = prev })
	ran := stub(t, func(string, ...string) (string, error) {
		t.Error("bk was run against a catalogue for another platform")
		return "", nil
	})

	var out, errb strings.Builder
	if code := run([]string{"-catalog", write(t, published)}, &out, &errb); code != 2 {
		t.Fatalf("code=%d, want 2\n%s%s", code, out.String(), errb.String())
	}
	if len(*ran) != 0 {
		t.Fatalf("bk ran anyway: %q", *ran)
	}
	for _, want := range []string{"linux/x86-64", "darwin/aarch64", "installs for the HOST"} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("the refusal does not say %q: %s", want, errb.String())
		}
	}
}

// ⛔ A FINDING IS RED, AND THE OUTPUT GOES WITH IT. A verdict with the
// evidence dropped is a line nobody can act on, and this fleet has published
// one before.
func TestAFindingIsRedAndKeepsItsOutput(t *testing.T) {
	onLinuxX86(t)
	stub(t, func(string, ...string) (string, error) {
		return "python.org    python.org    2 unresolvable\n", errors.New("exit status 1")
	})

	var out, errb strings.Builder
	if code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "1"}, &out, &errb); code != 1 {
		t.Fatalf("code=%d, want 1\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "2 unresolvable") {
		t.Errorf("the evidence was dropped:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "chunk 0 of 1") {
		t.Errorf("the failure does not name the chunk: %s", errb.String())
	}
	if strings.Contains(out.String(), "clean") {
		t.Errorf("a run with findings claimed to be clean:\n%s", out.String())
	}
}

// ⛔ EVERY WAY OF SWEEPING NOTHING IS AN ERROR, NEVER A CLEAN ZERO. This
// fleet has published a zero that meant "could not read".
func TestItRefusesToSweepNothing(t *testing.T) {
	onLinuxX86(t)
	for _, c := range []struct {
		name string
		args []string
		body string
		want string
	}{
		{"no catalogue named", []string{}, published, "-catalog is required"},
		{"a chunk outside the range", []string{"-chunk", "9", "-chunks", "2"}, published, "not in 0..1"},
		{"a negative chunk", []string{"-chunk", "-1", "-chunks", "2"}, published, "not in 0..1"},
		{"a catalogue with no bottle at all", []string{}, `{"generated":"x","projects":[{"project":"aerc-mail.org","platforms":["linux/x86-64"]}]}`, "none of them has a bottle"},
		{"a catalogue that names no platform", []string{}, `{"generated":"x","projects":[{"project":"curl.se","versions":["8.20.0"]}]}`, "names no platform"},
		// 10 chunks over 4 projects leaves most of them empty: chunk 1
		// spans [0,0). That is a misconfiguration, not a clean sweep, and a
		// rotation that printed "clean" for six of ten days would be the
		// most convincing wrong answer this file could give.
		{"more chunks than projects", []string{"-chunk", "1", "-chunks", "10"}, published, "is empty over 4 project(s)"},
		{"not a catalogue", []string{}, "not json at all", "catalog.json"},
		// A catalogue is published per platform. One naming two is not the
		// file this tool thinks it is, and picking which to believe is how
		// the host check above gets bypassed.
		{"a catalogue naming two platforms", []string{}, `{"generated":"x","projects":[
		   {"project":"curl.se","versions":["8.20.0"],"platforms":["linux/x86-64"]},
		   {"project":"zlib.net","versions":["1.3.2"],"platforms":["darwin/aarch64"]}]}`,
			"names 2 platforms (darwin/aarch64 linux/x86-64)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub(t, func(string, ...string) (string, error) {
				t.Error("bk ran although there was nothing to sweep")
				return "", nil
			})
			args := c.args
			if !contains(args, "-catalog") && c.name != "no catalogue named" {
				args = append([]string{"-catalog", write(t, c.body)}, args...)
			}
			var out, errb strings.Builder
			if code := run(args, &out, &errb); code != 2 {
				t.Fatalf("code=%d, want 2\n%s%s", code, out.String(), errb.String())
			}
			if !strings.Contains(errb.String(), c.want) {
				t.Errorf("the refusal does not say %q: %s", c.want, errb.String())
			}
		})
	}
}

// A catalogue that is not there is not a platform with nothing to sweep.
func TestAMissingCatalogueIsAnError(t *testing.T) {
	onLinuxX86(t)
	stub(t, func(string, ...string) (string, error) {
		t.Error("bk ran without a catalogue")
		return "", nil
	})
	var out, errb strings.Builder
	if code := run([]string{"-catalog", filepath.Join(t.TempDir(), "absent.json")}, &out, &errb); code != 2 {
		t.Fatalf("code=%d, want 2\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "absent.json") {
		t.Errorf("the refusal does not name the file: %s", errb.String())
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// main() is reachable, and a bad flag is exit 2 rather than a panic.
func TestMainExits(t *testing.T) {
	prevArgs, prevExit := os.Args, osExit
	code := -1
	osExit = func(c int) { code = c }
	os.Args = []string{"sonamesweep", "-nope"}
	t.Cleanup(func() { os.Args, osExit = prevArgs, prevExit })
	main()
	if code != 2 {
		t.Errorf("a bad flag exited %d, want 2", code)
	}
}
