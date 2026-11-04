package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"debug/pe"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-zpkg/bottle"
)

// The command-line surface, which is what CI actually invokes. The check
// itself is tested in main_test.go; these are the ways a run can be wrong
// before any archive is opened.
func TestRunRefusesAnIncompleteInvocation(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", nil, "usage:"},
		{"no member", []string{"-dir", "/tmp"}, "usage:"},
		{"no dir", []string{"-member", "bin/x.exe"}, "usage:"},
		{"unknown arch", []string{"-dir", "/tmp", "-member", "bin/x.exe", "-arch", "sparc"}, `unknown arch "sparc"`},
		{"unknown flag", []string{"-nope"}, "flag provided but not defined"},
	} {
		err := run(c.args, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to contain %q", c.name, err, c.want)
		}
	}
	// And the arch list names the arches rather than leaving the reader to
	// guess the spelling — "amd64" is the mistake actually made.
	if got := archList(); got != "aarch64, x86-64" {
		t.Errorf("archList() = %q", got)
	}
}

// A whole-run success through the flag surface, so the wiring between run and
// check is exercised and not only each half.
func TestRunEndToEnd(t *testing.T) {
	dir := t.TempDir()
	bottleWith(t, dir, "zlib.net-1.3.2",
		member{"zlib.net/v1.3.2/bin/minigzip.exe", fakePE(pe.IMAGE_FILE_MACHINE_AMD64)})
	// dist carries more than bottles — checksums, manifests, logs. They must
	// be stepped over rather than opened, and must not reach the archive
	// count that the "published nothing" verdict rests on.
	for _, n := range []string{"v1.3.2.sha256", "manifest.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	if err := run([]string{"-dir", dir, "-member", "bin/minigzip.exe"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "OK — PE") {
		t.Errorf("output %q", out.String())
	}
	if !strings.Contains(out.String(), "opened 1 archive(s)") {
		t.Errorf("output %q, want the non-bottle files left out of the count", out.String())
	}
}

// main is thin on purpose, but "thin" is a claim: the seam proves it reports
// a failure as a non-zero exit rather than passing one over in silence.
func TestMainExitsNonZeroOnFailure(t *testing.T) {
	oldArgs, oldExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = oldArgs, oldExit })

	code := 0
	osExit = func(c int) { code = c }
	os.Args = []string{"pecheck", "-dir", t.TempDir(), "-member", "bin/minigzip.exe"}
	main()
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for an empty dist", code)
	}

	// And zero when there is nothing wrong, which is the half that would
	// otherwise pass by never being looked at.
	code = 0
	dir := t.TempDir()
	bottleWith(t, dir, "zlib.net-1.3.2",
		member{"zlib.net/v1.3.2/bin/minigzip.exe", fakePE(pe.IMAGE_FILE_MACHINE_AMD64)})
	os.Args = []string{"pecheck", "-dir", dir, "-member", "bin/minigzip.exe"}
	main()
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

// ⛔ EVERY COMPRESSION WE PUBLISH MUST BE OPENED. The factory's bottles are
// .tar.zst today and .tar.xz and .tar.gz exist in the registry; a checker
// that silently skipped one extension would report "the build published
// nothing" for a build that published it in the other.
func TestEveryBottleExtensionIsRecognised(t *testing.T) {
	for _, ext := range []string{bottle.ExtTarXz, bottle.ExtTarZst, bottle.ExtTarGz} {
		if got := archiveExt("dist/zlib.net/v1.3.2" + ext); got != ext {
			t.Errorf("archiveExt(…%s) = %q, want %q", ext, got, ext)
		}
	}
	// And nothing else is mistaken for one: dist carries checksums and
	// manifests beside the bottles.
	for _, p := range []string{"dist/v1.3.2.sha256", "dist/manifest.json", "dist/notes.txt", "dist/v1.3.2.gz"} {
		if got := archiveExt(p); got != "" {
			t.Errorf("archiveExt(%q) = %q, want none", p, got)
		}
	}
}

// ⛔ A MEMBER MAY NOT BE READ WITHOUT BOUND. A tar header declares its own
// size, and a bottle is untrusted input here exactly as it is in the
// installer. The cap is a var so this test can prove it fires without
// building half a gigabyte — a bound nothing exercises is a comment.
func TestAnOversizedMemberIsRefused(t *testing.T) {
	dir := t.TempDir()
	bottleWith(t, dir, "zlib.net-1.3.2",
		member{"zlib.net/v1.3.2/bin/minigzip.exe", bytes.Repeat([]byte("x"), 4096)})

	old := maxMember
	t.Cleanup(func() { maxMember = old })
	maxMember = 1024

	err := check(dir, "bin/minigzip.exe", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &bytes.Buffer{})
	if err == nil {
		t.Fatal("an oversized member was read")
	}
	if !strings.Contains(err.Error(), "exceeds 1024 bytes") {
		t.Errorf("err = %v, want it to name the bound", err)
	}
}

// A corrupt archive is an error about THAT archive, named, rather than a
// silent "not found" that would send someone to look at the build.
func TestACorruptArchiveIsNamed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "zlib.net-1.3.2"+bottle.ExtTarGz)
	if err := os.WriteFile(p, []byte("this is not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := check(dir, "bin/minigzip.exe", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &bytes.Buffer{})
	if err == nil {
		t.Fatal("a corrupt archive passed")
	}
	if !strings.Contains(err.Error(), "zlib.net-1.3.2") {
		t.Errorf("err = %v, want it to name the archive", err)
	}
}

// A truncated member: the header promises more bytes than the archive holds,
// so the READ fails. Distinct from the next case, which fails in the header.
func TestATruncatedMemberIsAnError(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "a/bin/x", Mode: 0o755, Size: 100, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("short")); err != nil {
		t.Fatal(err)
	}
	gz.Close() // deliberately no tw.Close(): the archive ends early
	p := filepath.Join(dir, "zlib.net-1.3.2"+bottle.ExtTarGz)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := check(dir, "bin/x", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &bytes.Buffer{}); err == nil {
		t.Fatal("a truncated archive passed")
	}
}

// ⛔ AND A BAD HEADER FAILS IN tar.Next, WHICH IS A DIFFERENT LINE. Garbage
// where the next header belongs is what a partially-written or tampered
// bottle looks like, and it must be an error rather than a quiet end of
// archive — a silent stop would report the members seen so far as the whole
// count, which is the denominator defect in yet another costume.
func TestGarbageWhereAHeaderBelongsIsAnError(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("ok")
	if err := tw.WriteHeader(&tar.Header{Name: "a/bin/x", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Flush(); err != nil {
		t.Fatal(err)
	}
	// A full block of 0xFF where the next header would start: not a header,
	// and not the zero blocks that end an archive.
	if _, err := gz.Write(bytes.Repeat([]byte{0xFF}, 512)); err != nil {
		t.Fatal(err)
	}
	gz.Close()
	if err := os.WriteFile(filepath.Join(dir, "z"+bottle.ExtTarGz), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	// Asking for a member that is NOT in the archive, so the walk must reach
	// the bad header rather than stopping at a hit.
	if err := check(dir, "bin/absent", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &bytes.Buffer{}); err == nil {
		t.Fatal("a bad tar header passed as the end of the archive")
	}
}

// Directories and symlinks inside a bottle are not members to check, and
// counting them would inflate the denominator this tool exists to report.
func TestOnlyRegularFilesAreCounted(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range []*tar.Header{
		{Name: "zlib.net/v1.3.2/bin/", Mode: 0o755, Typeflag: tar.TypeDir},
		{Name: "zlib.net/v1.3.2/bin/link", Linkname: "minigzip.exe", Typeflag: tar.TypeSymlink},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	body := fakePE(pe.IMAGE_FILE_MACHINE_AMD64)
	if err := tw.WriteHeader(&tar.Header{Name: "zlib.net/v1.3.2/bin/minigzip.exe", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(filepath.Join(dir, "z"+bottle.ExtTarGz), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := check(dir, "bin/minigzip.exe", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &out); err != nil {
		t.Fatalf("check() = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "examined 1 member(s)") {
		t.Errorf("output %q, want the directory and symlink left out of the count", out.String())
	}
}

// An unreadable directory is reported, not counted as "nothing published":
// a permissions fault and an empty dist lead to different repairs.
func TestAnUnreadableArchiveIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads anything, so the refusal cannot be staged")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "zlib.net-1.3.2"+bottle.ExtTarGz)
	if err := os.WriteFile(p, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	if err := check(dir, "bin/x", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &bytes.Buffer{}); err == nil {
		t.Fatal("an unreadable archive passed")
	}
}
