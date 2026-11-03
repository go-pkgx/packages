package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pkgx/bottle"
)

// fakePE builds the smallest byte sequence debug/pe accepts: a DOS stub whose
// e_lfanew points at "PE\0\0", a COFF header naming the machine, and a PE32+
// optional header. The optional header is not optional for the parser — with
// SizeOfOptionalHeader 0 it stops at EOF — and its size must match the COFF
// field exactly, which is the part worth stating since a wrong pair reads as
// a corrupt file rather than a short one.
func fakePE(machine uint16) []byte {
	const (
		lfanew   = 0x40
		optSize  = 240 // sizeof(IMAGE_OPTIONAL_HEADER64)
		pe32Plus = 0x20b
	)
	b := make([]byte, lfanew)
	b[0], b[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(b[0x3c:], lfanew)
	b = append(b, 'P', 'E', 0, 0)

	var coff [20]byte
	binary.LittleEndian.PutUint16(coff[0:], machine)
	// NumberOfSections stays 0; there is nothing to look at inside.
	binary.LittleEndian.PutUint16(coff[16:], optSize)
	b = append(b, coff[:]...)

	opt := make([]byte, optSize)
	binary.LittleEndian.PutUint16(opt[0:], pe32Plus)
	// NumberOfRvaAndSizes at offset 108 of the 64-bit optional header: 16 is
	// the standard count, and the 16 data directories are the zero bytes that
	// follow inside optSize.
	binary.LittleEndian.PutUint32(opt[108:], 16)
	return append(b, opt...)
}

// fakeELF is the control: the exact shape a cross build that fell back to the
// host compiler leaves behind.
func fakeELF() []byte {
	b := make([]byte, 64)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	return b
}

// bottleWith writes a .tar.gz holding the given members, as the factory's
// dist directory does.
func bottleWith(t *testing.T, dir, name string, members map[string][]byte) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for n, body := range members {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name+bottle.ExtTarGz)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The ordinary case: the member is inside the packaged bottle, under the
// install prefix, which is why the match is on the suffix.
func TestFindsThePEInsideTheBottle(t *testing.T) {
	dir := t.TempDir()
	bottleWith(t, dir, "zlib.net-1.3.2", map[string][]byte{
		"zlib.net/v1.3.2/bin/minigzip.exe": fakePE(pe.IMAGE_FILE_MACHINE_AMD64),
		"zlib.net/v1.3.2/lib/libz.a":       []byte("!<arch>\n"),
	})
	var out bytes.Buffer
	if err := run(dir, "bin/minigzip.exe", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &out); err != nil {
		t.Fatalf("run() = %v\n%s", err, out.String())
	}
	for _, want := range []string{"opened 1 archive(s)", "examined 2 member(s)", "OK — PE"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q, want it to contain %q", out.String(), want)
		}
	}
}

// ⛔⛔ THE DEFECT THIS TOOL REPLACES: an ELF passing for a Windows build. A
// cross build that fell back to the host compiler produces a working ELF and
// a green job, and the first person to find out runs it on Windows.
func TestAnELFIsRefused(t *testing.T) {
	dir := t.TempDir()
	bottleWith(t, dir, "zlib.net-1.3.2", map[string][]byte{
		"zlib.net/v1.3.2/bin/minigzip.exe": fakeELF(),
	})
	err := run(dir, "bin/minigzip.exe", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &bytes.Buffer{})
	if err == nil {
		t.Fatal("an ELF passed as a Windows binary")
	}
	if !strings.Contains(err.Error(), "not a PE") {
		t.Errorf("err = %v, want it to name the format", err)
	}
}

// A PE for the WRONG machine is also a failure: aarch64 output in an x86-64
// lane runs nowhere, and both are PEs, so the format check alone would pass.
func TestAPEForTheWrongMachineIsRefused(t *testing.T) {
	dir := t.TempDir()
	bottleWith(t, dir, "zlib.net-1.3.2", map[string][]byte{
		"zlib.net/v1.3.2/bin/minigzip.exe": fakePE(pe.IMAGE_FILE_MACHINE_ARM64),
	})
	err := run(dir, "bin/minigzip.exe", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &bytes.Buffer{})
	if err == nil {
		t.Fatal("an arm64 PE passed in an x86-64 lane")
	}
	if !strings.Contains(err.Error(), "0xaa64") {
		t.Errorf("err = %v, want it to name the machine it found", err)
	}
}

// ⛔ "NOT FOUND" AND "NOTHING WAS BUILT" ARE DIFFERENT DEFECTS, and the old
// shell check reported the first for the second. An empty dist means the
// build published nothing; a missing member means it published the wrong
// thing. They are fixed in different places.
func TestAnEmptyDistIsNotAMissingMember(t *testing.T) {
	err := run(t.TempDir(), "bin/minigzip.exe", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &bytes.Buffer{})
	if err == nil {
		t.Fatal("an empty dist passed")
	}
	if !strings.Contains(err.Error(), "published nothing") {
		t.Errorf("err = %v, want it to say the build published nothing", err)
	}
}

func TestAMissingMemberNamesTheDenominator(t *testing.T) {
	dir := t.TempDir()
	bottleWith(t, dir, "zlib.net-1.3.2", map[string][]byte{
		"zlib.net/v1.3.2/lib/libz.a": []byte("!<arch>\n"),
	})
	var out bytes.Buffer
	err := run(dir, "bin/minigzip.exe", "x86-64", pe.IMAGE_FILE_MACHINE_AMD64, &out)
	if err == nil {
		t.Fatal("a bottle without the binary passed")
	}
	if !strings.Contains(err.Error(), "1 archive(s)") {
		t.Errorf("err = %v, want it to name how many archives it opened", err)
	}
	if !strings.Contains(out.String(), "examined 1 member(s)") {
		t.Errorf("output %q, want the member count", out.String())
	}
}

// The suffix match stops at a path boundary, so a same-named file elsewhere
// in the tree is not mistaken for the one asked for.
func TestTheMatchStopsAtAPathBoundary(t *testing.T) {
	if matches("zlib.net/v1.3.2/sbin/minigzip.exe", "bin/minigzip.exe") {
		t.Error("sbin/minigzip.exe matched bin/minigzip.exe")
	}
	if !matches("zlib.net/v1.3.2/bin/minigzip.exe", "bin/minigzip.exe") {
		t.Error("the real member did not match")
	}
	if !matches("./bin/minigzip.exe", "bin/minigzip.exe") {
		t.Error("a ./-prefixed member did not match")
	}
}
