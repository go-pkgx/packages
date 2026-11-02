package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ⛔⛔ THE NEEDLE COMES FROM WHAT LANDED, NOT FROM WHAT WAS ASKED FOR.
//
// A bottle is installed under the directory it CARRIES, which is not always
// the version that was requested: the rebuild published as `3.3.11_1` unpacks
// to `v3.3.11`. The first form of this tool searched for `v3.3.11_1` inside a
// tree named `v3.3.11`, found nothing, and printed "clean" — a scan that could
// not read reporting zero, in the very tool written to stop that happening.
//
// fftw bakes its prefix in 9 files.
func TestTheNeedleIsTakenFromTheInstalledDirectoryNotTheRequestedVersion(t *testing.T) {
	home := t.TempDir()
	// What the registry was asked for: 3.3.11_1. What landed: v3.3.11.
	landed := filepath.Join(home, "v3.3.11")
	mustWrite(t, filepath.Join(landed, "lib", "libfftw3.dylib"),
		"\x00/opt/fftw.org/v3.3.11/lib/libfftw3.dylib\x00")

	component, err := installedComponent(home)
	if err != nil {
		t.Fatal(err)
	}
	if component != "v3.3.11" {
		t.Fatalf("installedComponent = %q, want the directory that landed", component)
	}

	hits, err := bakedPrefixes(home, component)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want the baked dylib: %v", len(hits), hits)
	}

	// And the mistake it replaces: the requested version finds nothing.
	vacuous, err := bakedPrefixes(home, "v3.3.11_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vacuous) != 0 {
		t.Fatal("the fixture no longer reproduces the vacuous zero")
	}
}

// ⛔ A BARE VERSION TOKEN IS NOT A BAKED PREFIX. `#define VERSION "v1.10.0"`
// in a header says nothing about where the package was built. Counting it
// would inflate the sweep with files that survive a rename perfectly well —
// and the sweep's number is what decides go-pkgx/bk#386.
func TestAVersionStringIsNotCountedAsABakedPath(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "v1.10.0", "include", "lz4.h"),
		"#define LZ4_VERSION_STRING \"v1.10.0\"\n/* released as v1.10.0 */\n")

	hits, err := bakedPrefixes(home, "v1.10.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("a bare version token was counted as a baked path: %v", hits)
	}

	// The same file gains a real path, and now it counts.
	mustWrite(t, filepath.Join(home, "v1.10.0", "include", "lz4.h"),
		"#define LZ4_PREFIX \"/opt/lz4.org/v1.10.0/include\"\n")
	hits, err = bakedPrefixes(home, "v1.10.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("a real baked path was missed: %v", hits)
	}
	if want := "/opt/lz4.org/v1.10.0/include"; hits[0].Context != want {
		t.Errorf("context = %q, want %q", hits[0].Context, want)
	}
}

// A neighbouring version must not be read as this one: `v1.2` inside `v1.20`.
func TestANeighbouringVersionIsNotThisOne(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "v1.2", "bin", "tool"),
		"/opt/acme.org/v1.20/lib:/opt/acme.org/v1.200/lib\n")

	hits, err := bakedPrefixes(home, "v1.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("v1.20 was read as v1.2: %v", hits)
	}
}

// ⛔ BINARY CONTENT IS WHERE BAKED PREFIXES LIVE. The measurement I first
// published on go-pkgx/bk#386 — "fftw's bottle bakes nothing" — came from a
// `grep -rl` that resolved to ugrep with `-I`, skipping binary files. Every
// one of fftw's 9 hits is a Mach-O dylib, an executable or a -config script.
// A text-only search cannot see a baked prefix at all.
func TestBinaryFilesAreSearched(t *testing.T) {
	home := t.TempDir()
	// NUL bytes on both sides, as in a Mach-O install_name.
	mustWrite(t, filepath.Join(home, "v1.3.2", "lib", "libz.1.3.2.dylib"),
		"\x7fELF\x00\x00\x01/opt/zlib.net/v1.3.2/lib/libz.1.3.2.dylib\x00\x00\xff\xfe")

	hits, err := bakedPrefixes(home, "v1.3.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("a baked path inside a binary was missed: %v", hits)
	}
	if !strings.HasPrefix(hits[0].Context, "/opt/zlib.net/v1.3.2/") {
		t.Errorf("context did not recover the whole path: %q", hits[0].Context)
	}
}

// ⛔ UNREADABLE IS NOT CLEAN. A project whose tree cannot be read has told us
// nothing about whether it bakes its prefix; folding it into "clean" is a
// census that could not read reporting zero.
func TestATreeWithNoVersionDirectoryIsRefusedNotCalledClean(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "README"), "nothing unpacked here\n")

	if _, err := installedComponent(home); err == nil {
		t.Fatal("a tree with no unpacked version directory was accepted")
	}
}

// Two real version directories mean the store was shared with an earlier
// project in the sweep. Picking one would attribute its files to the wrong
// package, so it refuses.
func TestTwoVersionDirectoriesAreRefusedRatherThanPicked(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "v1.0.0", "bin", "a"), "x")
	mustWrite(t, filepath.Join(home, "v2.0.0", "bin", "a"), "x")

	_, err := installedComponent(home)
	if err == nil {
		t.Fatal("two version directories were silently reduced to one")
	}
	for _, want := range []string{"v1.0.0", "v2.0.0", "-store"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

// A `v*` alias symlink is not the unpacked directory. Following one would
// make the needle depend on which alias sorted first.
func TestVersionAliasSymlinksAreNotMistakenForTheUnpackedTree(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "v1.3.2", "bin", "a"), "x")
	for _, alias := range []string{"v1", "v1.3", "v*"} {
		if err := os.Symlink("v1.3.2", filepath.Join(home, alias)); err != nil {
			t.Fatal(err)
		}
	}

	component, err := installedComponent(home)
	if err != nil {
		t.Fatal(err)
	}
	if component != "v1.3.2" {
		t.Fatalf("installedComponent = %q, want v1.3.2", component)
	}
}

// ⛔ A SWEEP THAT SWEPT NOTHING MUST NOT EXIT 0. "0 bake their own prefix" and
// "nothing was measured" print almost the same line, and only one of them is
// an answer.
func TestASweepThatMeasuredNothingFails(t *testing.T) {
	var out, errOut strings.Builder
	err := run([]string{"-platform", "darwin/aarch64", "no.such.project.invalid"}, &out, &errOut)
	if err == nil {
		t.Fatal("a sweep that swept nothing returned success")
	}
	if !strings.Contains(err.Error(), "measured nothing") {
		t.Errorf("the error does not say nothing was measured: %v", err)
	}
	if !strings.Contains(out.String(), "0 swept") {
		t.Errorf("the totals were not printed: %q", out.String())
	}
}

func TestPlatformIsRequiredAndMustBeOsSlashArch(t *testing.T) {
	var out, errOut strings.Builder
	if err := run([]string{"zlib.net"}, &out, &errOut); err == nil {
		t.Error("a sweep without -platform was accepted")
	}
	if err := run([]string{"-platform", "darwin", "zlib.net"}, &out, &errOut); err == nil {
		t.Error("a -platform without an arch was accepted")
	}
}

func TestReadListIgnoresBlankLinesAndComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipes.txt")
	mustWrite(t, path, "# a comment\n\nzlib.net\n  curl.se  \n# another\n")

	got, err := readList(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"zlib.net", "curl.se"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
