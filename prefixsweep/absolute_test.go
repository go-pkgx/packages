package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ⛔⛔ THE POINT OF -absolute: IT CATCHES A LANE NOBODY SPELLED.
//
// A `-needle` certifies the lane it was written for. `/Users/runner/...` is
// right on darwin and finds nothing on linux, which builds with
// HOME=/github/home — measured 2026-11-03, four pre-fix linux bottles swept
// "4 clean" against `/home/runner` and it meant nothing at all.
//
// This fixture holds three builder roots nobody would think to enumerate,
// including the Xcode SDK, which the first real run turned up in bzip2 and
// which no needle anyone had written would have caught.
func TestAnyBuilderRootIsCaughtWithoutBeingNamed(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "v1/bin/tool"), strings.Join([]string{
		"/Users/runner/.pkgx",
		"/github/home/.local/share/brewkit/linux+x86-64/x/v1/build",
		"/Applications/Xcode_15.4.app/Contents/Developer/SDKs/MacOSX.sdk",
		"/home/someone-nobody-listed/work/build",
	}, "\x00")+"\x00")

	hits, err := absoluteHits(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 4 {
		t.Fatalf("got %d hits, want all four builder roots: %v", len(hits), hits)
	}
}

// ⛔ A LOOSE ALPHABET MAKES THIS USELESS. isPathByte accepts anything above a
// space, which is right for recovering the path around a KNOWN match and
// wrong for deciding what IS one: the first run reported `sed` expressions
// out of bzgrep and C comment rules out of bzlib.h as baked absolute paths.
// A check whose output is mostly false positives gets switched off.
func TestShellAndCommentFragmentsAreNotPaths(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "v1/bin/script"), strings.Join([]string{
		`sed "s/&/\\&/g"`,
		`name=${f%/.bz2$//}`,
		"/*-------------------------------------------------------------*/",
		"see //groups.google.com/forum/ for details",
		"rpath @loader_path/../../../../..",
	}, "\n"))

	hits, err := absoluteHits(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("non-paths reported as absolute paths: %v", hits)
	}
}

// Platform paths a shipped binary may legitimately name are forgiven — a
// binary that loads /usr/lib/libSystem.B.dylib is not carrying a builder path.
func TestPlatformPathsAreAllowed(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "v1/bin/tool"), strings.Join([]string{
		"/usr/lib/libSystem.B.dylib",
		"/System/Library/Frameworks/Foundation.framework",
		"/dev/null/placeholder",
		"/private/var/tmp/thing",
	}, "\x00"))

	hits, err := absoluteHits(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("platform paths were reported: %v", hits)
	}
}

// ⛔ THE ALLOWLIST IS PRINTED ON EVERY RUN. It is the part of this check most
// likely to rot into a rubber stamp, and a clean result cannot be read
// without seeing what was forgiven — which is exactly how the `/home/runner`
// zero passed for a question nobody could see was wrong.
func TestTheAllowlistIsPrinted(t *testing.T) {
	var b strings.Builder
	reportAllowed(&b)
	for _, want := range []string{"/usr/", "/System/", "unless they start with"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("the allowlist output does not mention %q: %q", want, b.String())
		}
	}
}

// A run must not be asked both questions at once: they report different
// things and the combined output would say neither clearly.
func TestNeedleAndAbsoluteAreRefusedTogether(t *testing.T) {
	var out, errOut strings.Builder
	err := run([]string{"-platform", "darwin/aarch64", "-needle", "x", "-absolute", "p"}, &out, &errOut)
	if err == nil {
		t.Fatal("-needle and -absolute together were accepted")
	}
	if !strings.Contains(err.Error(), "different questions") {
		t.Errorf("the refusal does not explain itself: %v", err)
	}
}

// End to end through the command, so the assertion half is exercised: a hit
// must fail the run, not merely be printed.
func TestAbsoluteFailsTheRunOnAHit(t *testing.T) {
	f := &fakeRegistry{
		versions: map[string]string{"p": "1.0.0"},
		trees: map[string]map[string]string{
			"p": {"v1.0.0/bin/t": "/Users/runner/.pkgx/p/v1.0.0\x00"},
		},
	}
	out, err := sweep(t, f, "-absolute", "p")
	if err == nil {
		t.Fatal("a baked builder path did not fail the run")
	}
	if !strings.Contains(out, "/Users/runner/.pkgx") {
		t.Errorf("the path was not named in the report:\n%s", out)
	}
	if !strings.Contains(out, "unless they start with") {
		t.Errorf("the allowlist was not printed:\n%s", out)
	}
}

// And a tree with nothing to report passes, so the mode is usable as a gate.
func TestAbsolutePassesACleanTree(t *testing.T) {
	f := &fakeRegistry{
		versions: map[string]string{"p": "1.0.0"},
		trees: map[string]map[string]string{
			"p": {"v1.0.0/bin/t": "@rpath/p/v1.0.0/lib/libp.dylib\x00/usr/lib/libSystem.B.dylib\x00"},
		},
	}
	out, err := sweep(t, f, "-absolute", "p")
	if err != nil {
		t.Fatalf("a clean tree failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "clean") {
		t.Errorf("want a clean verdict:\n%s", out)
	}
}

func TestAnUnreadableFileFailsTheAbsoluteSweep(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	home := t.TempDir()
	p := filepath.Join(home, "v1", "bin", "t")
	mustWrite(t, p, "x")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })

	if _, err := absoluteHits(home); err == nil {
		t.Error("an unreadable file was treated as holding nothing")
	}
}

// A directory the walk cannot descend into stops the project rather than
// leaving part of the tree unexamined under a clean verdict.
func TestADirectoryThatCannotBeWalkedFailsTheAbsoluteSweep(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root descends into every directory")
	}
	home := t.TempDir()
	locked := filepath.Join(home, "v1", "lib")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if _, err := absoluteHits(home); err == nil {
		t.Error("an undescendable directory was reported as holding nothing")
	}
}

// ⛔⛔ THE FILTER THAT MAKES -absolute GATEABLE, and the measurement that says
// it is safe: over six linux bottles it turned 38 hits into 8, and not one of
// the 30 dropped was a builder path. They were curl man-page examples,
// a comment in sqlite3.h and a Romanian man page.
func TestCompiledOnlyDropsDocumentationAndKeepsBuilderPaths(t *testing.T) {
	home := t.TempDir()
	// A builder path compiled into a binary, and the same SHAPE of path
	// written by an author into a man page and a header.
	mustWrite(t, filepath.Join(home, "v1/bin/tool"),
		"/github/home/.pkgx/x/v1+brewing/share/locale\x00")
	mustWrite(t, filepath.Join(home, "v1/lib/libx.so.1"),
		"/opt/x/v1+brewing\x00")
	mustWrite(t, filepath.Join(home, "v1/share/man/man1/tool.1"),
		"for example /home/clarkkent/.ssh/id_rsa is read\n")
	mustWrite(t, filepath.Join(home, "v1/include/x.h"),
		"/* e.g. /home/fred/data.db */\n")

	all, err := absoluteHitsIn(home, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("unfiltered got %d hits, want all four: %v", len(all), all)
	}

	only, err := absoluteHitsIn(home, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 2 {
		t.Fatalf("compiled-only got %d hits, want the binary and the library: %v", len(only), only)
	}
	for _, h := range only {
		if strings.Contains(h.Rel, "man") || strings.Contains(h.Rel, "include") {
			t.Errorf("documentation survived the filter: %v", h)
		}
	}
}

// -compiled on its own is a request nobody can answer: it narrows -absolute.
func TestCompiledNeedsAbsolute(t *testing.T) {
	var out, errOut strings.Builder
	err := run([]string{"-platform", "darwin/aarch64", "-compiled", "p"}, &out, &errOut)
	if err == nil {
		t.Fatal("-compiled without -absolute was accepted")
	}
	if !strings.Contains(err.Error(), "needs -absolute") {
		t.Errorf("the refusal does not explain itself: %v", err)
	}
}
