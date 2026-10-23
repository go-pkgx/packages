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
	// ⛔ AND NOT WITH -quiet. That flag suppresses the per-reference lines,
	// and the sweep's first live run went out carrying it: chunk 0 reported
	// 64 unresolvable references and named not one soname.
	if got := *argv; len(got) != 4 || got[0] != "bk" || got[1] != "unresolved" || got[2] != "curl.se" {
		t.Fatalf("bk was not asked about a chunk of two: %q", got)
	}
	for _, a := range *argv {
		if a == "-quiet" {
			t.Error("-quiet strips the soname names the finding is made of")
		}
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
			for _, a := range args[1:] { // args[0] is "unresolved"
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

// What `bk unresolved` really printed, chunk 0 of the first live sweep,
// trimmed. The indented lines are the finding; the header's count is not.
const bkOutput = `curl.se                            ok
python.org                         python.org                         2 unresolvable
    libssl.so.1.1
    libcrypto.so.1.1
gnu.org/bash                       tcl-lang.org                       2 unresolvable
    libtcl8.6.so
    libtk8.6.so

4 unresolvable reference(s)
`

// ⛔ RED ON SOMETHING NEW IS ACTIONABLE; RED ON EVERYTHING IS FURNITURE.
// The first live sweep found 64 unresolvable references in its first 100
// projects, so without a baseline every weekly run is red and the one that
// matters is lost in it. This is vulnfetch's -fail-on-new, in the shape
// this measurement has.
func TestOnlyANewReferenceIsRed(t *testing.T) {
	onLinuxX86(t)
	base := filepath.Join(t.TempDir(), "baseline.txt")
	if err := os.WriteFile(base, []byte(
		"# known, being rebuilt\npython.org python.org libssl.so.1.1\npython.org python.org libcrypto.so.1.1\n"+
			"gnu.org/bash tcl-lang.org libtcl8.6.so\ngnu.org/bash tcl-lang.org libtk8.6.so\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stub(t, func(string, ...string) (string, error) { return bkOutput, errors.New("exit status 1") })

	var out, errb strings.Builder
	// bk exits 1 because there ARE references; the baseline says none is new.
	if code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "1", "-baseline", base}, &out, &errb); code != 0 {
		t.Fatalf("code=%d, want 0 — every reference is in the baseline\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "4 reference(s), 4 known, 0 new") {
		t.Errorf("the tally is wrong:\n%s", out.String())
	}

	// Now one of them is not recorded.
	if err := os.WriteFile(base, []byte(
		"python.org python.org libssl.so.1.1\ngnu.org/bash tcl-lang.org libtcl8.6.so\ngnu.org/bash tcl-lang.org libtk8.6.so\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errb.Reset()
	if code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "1", "-baseline", base}, &out, &errb); code != 1 {
		t.Fatalf("code=%d, want 1 — one reference is new\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "NEW python.org") || !strings.Contains(out.String(), "libcrypto.so.1.1") {
		t.Errorf("the new reference is not named:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "3 known, 1 new") {
		t.Errorf("the tally is wrong:\n%s", out.String())
	}
}

// A reference that got fixed should leave the baseline, so the file does not
// become a list of things that used to be true.
//
// ⛔ COUNTED ONLY WITHIN THIS CHUNK. The baseline is global and a run
// installs a fourteenth of it, so "in the baseline and not seen" says
// nothing about the thirteen fourteenths this job never touched — counting
// those would report the whole backlog as fixed, every run.
func TestFixedIsCountedOnlyInsideTheChunk(t *testing.T) {
	onLinuxX86(t)
	base := filepath.Join(t.TempDir(), "baseline.txt")
	if err := os.WriteFile(base, []byte(
		"python.org python.org libssl.so.1.1\npython.org python.org libcrypto.so.1.1\n"+
			"gnu.org/bash tcl-lang.org libtcl8.6.so\ngnu.org/bash tcl-lang.org libtk8.6.so\n"+
			// curl.se is in this chunk and came back clean: genuinely fixed.
			"curl.se openssl.org libssl.so.1.1\n"+
			// zlib.net is NOT in chunk 0 of 2, so nothing was learnt about it.
			"zlib.net zlib.net libz.so.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stub(t, func(string, ...string) (string, error) { return bkOutput, errors.New("exit status 1") })

	var out, errb strings.Builder
	if code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "2", "-baseline", base}, &out, &errb); code != 0 {
		t.Fatalf("code=%d, want 0\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "1 fixed since the baseline") {
		t.Errorf("want exactly one fixed (curl.se), not zlib.net which this chunk never installed:\n%s", out.String())
	}
}

// ⛔ A BASELINE THAT CANNOT BE READ IS NOT AN EMPTY ONE. Empty makes every
// finding new, so the run goes red for the wrong reason and the real new
// one is lost in the noise — the exact failure mode the baseline exists to
// prevent.
func TestAnUnreadableBaselineIsAnError(t *testing.T) {
	onLinuxX86(t)
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte("# nothing but a comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	malformed := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(malformed, []byte("python.org python.org\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, path, want string }{
		{"absent", filepath.Join(dir, "nope.txt"), "nope.txt"},
		{"empty", empty, "an empty baseline makes every finding new"},
		{"two fields, not three", malformed, "want `project owner soname`"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub(t, func(string, ...string) (string, error) { return bkOutput, errors.New("exit status 1") })
			var out, errb strings.Builder
			if code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "1", "-baseline", c.path}, &out, &errb); code != 2 {
				t.Fatalf("code=%d, want 2\n%s%s", code, out.String(), errb.String())
			}
			if !strings.Contains(errb.String(), c.want) {
				t.Errorf("the refusal does not say %q: %s", c.want, errb.String())
			}
		})
	}
}

// ⛔ THE INDENTED LINES ARE THE FINDING, NOT THE HEADER'S COUNT. A header
// that disagreed with the lines under it would otherwise inflate the total,
// and the two come from different code paths in bk.
func TestTheCountComesFromTheLinesNotTheHeader(t *testing.T) {
	got := parseFindings("a.org   b.org   99 unresolvable\n    libone.so.1\n\n1 unresolvable reference(s)\n")
	if len(got) != 1 {
		t.Fatalf("read %d finding(s) from one line, want 1: %+v", len(got), got)
	}
	if got[0] != (finding{"a.org", "b.org", "libone.so.1"}) {
		t.Errorf("the finding lost a field: %+v", got[0])
	}
	// An "ok" line ends the group, so a later indented line is not attributed
	// to the project above it.
	if g := parseFindings("a.org   b.org   1 unresolvable\nc.org   ok\n    libstray.so.1\n"); len(g) != 0 {
		t.Errorf("a stray indented line after `ok` became a finding: %+v", g)
	}
}

// ⛔ THE FOURTEEN FILES A FULL SWEEP WRITES *ARE* THE BASELINE. If -out
// emitted anything readBaseline could not read, assembling one would mean
// reformatting fourteen files by hand — and that is where a baseline stops
// being regenerated and starts being a list of things that used to be true.
// So this test takes the round trip, not the format.
func TestWhatItWritesIsWhatItReads(t *testing.T) {
	onLinuxX86(t)
	stub(t, func(string, ...string) (string, error) { return bkOutput, errors.New("exit status 1") })
	emitted := filepath.Join(t.TempDir(), "chunk0.txt")

	var out, errb strings.Builder
	if code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "1", "-out", emitted}, &out, &errb); code != 1 {
		t.Fatalf("code=%d, want 1 — recording does not make findings go away\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "4 reference(s) written to") {
		t.Errorf("it did not say what it wrote:\n%s", out.String())
	}

	known, err := readBaseline(emitted)
	if err != nil {
		t.Fatalf("what -out wrote, -baseline cannot read: %v", err)
	}
	if len(known) != 4 {
		t.Errorf("the round trip lost entries: %d of 4", len(known))
	}
	if !known[finding{"python.org", "python.org", "libssl.so.1.1"}] {
		t.Errorf("a finding did not survive the round trip: %v", known)
	}

	// Fed back as the baseline, the same sweep is green: that is the whole
	// point of recording, and it is the only check that the two formats
	// really are one format.
	out.Reset()
	errb.Reset()
	if code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "1", "-baseline", emitted}, &out, &errb); code != 0 {
		t.Fatalf("code=%d, want 0 — its own output is not an acceptable baseline\n%s%s", code, out.String(), errb.String())
	}
}

// A chunk with no findings still writes its file: missing and empty are
// different facts, and whoever assembles a baseline needs to tell a chunk
// that found nothing from one that never ran.
func TestACleanChunkStillWritesItsFile(t *testing.T) {
	onLinuxX86(t)
	stub(t, func(string, ...string) (string, error) { return "curl.se   ok\n\n0 unresolvable reference(s)\n", nil })
	emitted := filepath.Join(t.TempDir(), "chunk0.txt")

	var out, errb strings.Builder
	if code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "1", "-out", emitted}, &out, &errb); code != 0 {
		t.Fatalf("code=%d, want 0\n%s%s", code, out.String(), errb.String())
	}
	b, err := os.ReadFile(emitted)
	if err != nil {
		t.Fatalf("a clean chunk wrote no file: %v", err)
	}
	if len(b) != 0 {
		t.Errorf("a clean chunk wrote %q", b)
	}
}

// Somewhere unwritable is an error, not a footnote under a green tick: the
// file is the deliverable of a recording run.
func TestAnUnwritableOutIsAnError(t *testing.T) {
	onLinuxX86(t)
	stub(t, func(string, ...string) (string, error) { return bkOutput, errors.New("exit status 1") })
	var out, errb strings.Builder
	code := run([]string{"-catalog", write(t, published), "-chunk", "0", "-chunks", "1",
		"-out", filepath.Join(t.TempDir(), "no-such-dir", "chunk0.txt")}, &out, &errb)
	if code != 2 {
		t.Fatalf("code=%d, want 2\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "chunk0.txt") {
		t.Errorf("the refusal does not name the file: %s", errb.String())
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
