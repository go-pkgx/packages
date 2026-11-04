// pecheck asserts that a Windows bottle actually contains a Windows binary.
//
// ⛔ WHY A TOOL AND NOT A `find` AND A `grep`. The windows-c lane's first
// version did
//
//	exe=$(find "$RUNNER_TEMP/rootfs/dist" -name 'minigzip.exe' | head -1)
//	fmt=$(… objdump -f "$exe" | grep -o 'file format .*')
//
// and reported "minigzip.exe was not built" for a build that had just
// succeeded: `dist` holds the PACKAGED bottle, so there is no loose .exe to
// find. A pipeline cannot tell "the build produced nothing" from "I looked in
// the wrong place", and that is the whole question this step exists to answer.
//
// So it looks INSIDE the bottles, and it reports its denominator: how many
// archives it opened and how many members they hold. "Not found" is only
// meaningful next to "and here is what was there".
//
// A cross build that silently fell back to the host compiler produces a
// working ELF and a green job, and the first person to find out runs it on
// Windows. The PE machine type is the claim, and `debug/pe` reads it from the
// header rather than from a tool's prose.
package main

import (
	"archive/tar"
	"bytes"
	"debug/pe"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-zpkg/bottle"
)

// maxMember bounds what one archive member may be read into memory. A bottle
// is untrusted input here exactly as it is in the installer: a tar header can
// declare any size at all, and this step runs in CI with the rest of the job
// behind it. 512 MiB is far above any binary we publish and far below the
// runner's memory.
//
// A var so a test can shrink it: a test that had to build half a gigabyte to
// prove the bound works would never be run, and a bound nothing exercises is
// a comment.
var maxMember int64 = 512 << 20

// osExit is a seam so a test can exercise main without ending the process.
var osExit = os.Exit

// machines maps our arch spelling to the PE machine type that proves it.
var machines = map[string]uint16{
	"x86-64":  pe.IMAGE_FILE_MACHINE_AMD64,
	"aarch64": pe.IMAGE_FILE_MACHINE_ARM64,
}

// archiveExts are the bottle extensions bottle.Decompressor understands,
// longest first so ".tar.gz" is not read as ".gz".
var archiveExts = []string{bottle.ExtTarXz, bottle.ExtTarZst, bottle.ExtTarGz}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "::error::%v\n", err)
		osExit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("pecheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "directory holding the built bottle(s)")
	member := fs.String("member", "", "archive member to check, eg. bin/minigzip.exe")
	arch := fs.String("arch", "x86-64", "expected arch: "+archList())
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *member == "" {
		return fmt.Errorf("usage: pecheck -dir <dist> -member <bin/x.exe> [-arch %s]", archList())
	}
	want, ok := machines[*arch]
	if !ok {
		return fmt.Errorf("unknown arch %q (known: %s)", *arch, archList())
	}
	return check(*dir, *member, *arch, want, stdout)
}

func archList() string {
	out := make([]string, 0, len(machines))
	for a := range machines {
		out = append(out, a)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// check finds member inside the bottles under dir and asserts it is a PE for
// want. It prints what it opened either way.
func check(dir, member, arch string, want uint16, out io.Writer) error {
	var archives, members int
	var found []byte
	var foundIn string

	// Every archive is opened, including those after the hit: see memberOf
	// for why a count that stops at the answer is not a count. dist holds a
	// handful of bottles, so the whole walk is cheaper than the explanation.
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := archiveExt(p)
		if ext == "" {
			return nil
		}
		archives++
		b, n, err := memberOf(p, ext, member)
		members += n
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if b != nil && found == nil {
			found, foundIn = b, p
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "pecheck: opened %d archive(s) under %s, examined %d member(s)\n", archives, dir, members)
	if archives == 0 {
		// Distinguished from "found nothing": an empty dist means the BUILD
		// produced nothing, which is a different defect from a bad artefact.
		return fmt.Errorf("no bottle archive under %s — the build published nothing", dir)
	}
	if found == nil {
		return fmt.Errorf("no member %q in any of the %d archive(s) under %s", member, archives, dir)
	}
	fmt.Fprintf(out, "pecheck: found %s in %s (%d bytes)\n", member, foundIn, len(found))

	f, err := pe.NewFile(bytes.NewReader(found))
	if err != nil {
		return fmt.Errorf("%s in %s is not a PE at all (%v) — a cross build that fell back to the host compiler produces an ELF here", member, foundIn, err)
	}
	defer f.Close()
	if f.Machine != want {
		return fmt.Errorf("%s is a PE for machine 0x%04x, want 0x%04x (%s)", member, f.Machine, want, arch)
	}
	fmt.Fprintf(out, "pecheck: OK — PE, machine 0x%04x (%s)\n", f.Machine, arch)
	return nil
}

// archiveExt returns the bottle extension p ends in, or "".
func archiveExt(p string) string {
	for _, e := range archiveExts {
		if strings.HasSuffix(p, e) {
			return e
		}
	}
	return ""
}

// memberOf returns the bytes of the member whose path ends in want, and how
// many members the archive holds. A bottle's paths are prefixed by the
// install prefix, so the match is on the SUFFIX: "bin/minigzip.exe" must find
// "zlib.net/v1.3.2/bin/minigzip.exe".
//
// ⛔⛔ IT READS TO THE END EVEN AFTER A HIT, because the count is the point.
// The first version returned as soon as it matched, so "examined N member(s)"
// meant "N seen BEFORE the match" — a number that moves with the order the
// archive happens to list its entries, which is precisely the kind of figure
// this tool exists to replace. CI caught it: the same archive reported 1 or 2
// depending on which member the writer emitted first.
//
// A denominator that depends on where the answer was found is not a
// denominator. Scanning on costs one pass over a tar we have already
// decompressed.
func memberOf(p, ext, want string) ([]byte, int, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	dec, closeFn, err := bottle.Decompressor(ext, f)
	if err != nil {
		return nil, 0, err
	}
	defer closeFn()

	n := 0
	var found []byte
	tr := tar.NewReader(dec)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return found, n, nil
		}
		if err != nil {
			return nil, n, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		n++
		if found != nil || !matches(h.Name, want) {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxMember+1))
		if err != nil {
			return nil, n, err
		}
		if int64(len(b)) > maxMember {
			return nil, n, fmt.Errorf("member %s exceeds %d bytes", h.Name, maxMember)
		}
		found = b
	}
}

// matches reports whether a tar member path is the wanted one: equal, or
// ending at a path boundary so "bin/x.exe" does not match "sbin/x.exe".
func matches(name, want string) bool {
	name = path.Clean(strings.TrimPrefix(name, "./"))
	return name == want || strings.HasSuffix(name, "/"+want)
}
