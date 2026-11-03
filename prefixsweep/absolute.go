package main

// `-absolute` asserts a PROPERTY instead of searching for a remembered string.
//
// ⛔⛔ A SPELLED NEEDLE CERTIFIES THE LANE IT WAS WRITTEN FOR AND NO OTHER.
// The check agreed for go-pkgx/bk#387 was
// `-needle /Users/runner/Library/Caches/brewkit`, which is right on darwin.
// Run on linux it returns clean on every bottle whatever they contain,
// because that lane builds with `HOME=/github/home` and the string was never
// going to be there. Measured 2026-11-03: four pre-fix linux bottles swept
// "4 clean" against `/home/runner`, which meant nothing at all.
//
// A derivation table — darwin is /Users/runner, linux is /github/home — only
// moves the guess one level out. The s390x LinuxONE box is neither, and it is
// the lane where a stray absolute path actually costs something, because that
// runner is ours and its paths are not constant between rebuilds.
//
// The property wanted is not "this string is absent". It is:
//
//	no absolute path outside the store is baked into a shipped artefact.
//
// That needs no needle, so a lane nobody anticipated FAILS rather than passes.
// It also finds the CLASS rather than the instance: the first run of this on
// sourceware.org/bzip2 turned up
// `/Applications/Xcode_15.4.app/.../MacOSX.sdk`, a builder path pinning an
// Xcode version, which no needle anyone had written would have caught.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// allowedPrefixes are absolute paths a shipped binary may legitimately name:
// the platform's own, which exist on the machine that runs it.
//
// ⛔ KEPT TIGHT ON PURPOSE. Every entry here is a place a builder path could
// hide, so the list is the part of this check most likely to rot into a
// rubber stamp. It is printed on every run for that reason: a reader has to
// be able to see what was forgiven, not just what was found.
var allowedPrefixes = []string{
	"/usr/",
	"/bin/",
	"/sbin/",
	"/etc/",
	"/dev/",
	"/var/",
	"/private/var/",
	"/System/",
	"/Library/Frameworks/",
	"/lib/",
	"/lib64/",
	"/proc/",
}

// absoluteHits returns the absolute paths under root that are not allowed.
func absoluteHits(root string) ([]hit, error) {
	found := map[string]string{} // path -> first file it was seen in
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return fmt.Errorf("%s: %w", p, rerr)
		}
		rel, _ := filepath.Rel(root, p)
		for _, s := range absolutePaths(b) {
			if allowedAbsolute(s) {
				continue
			}
			if _, seen := found[s]; !seen {
				found[s] = rel
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []hit
	for s, rel := range found {
		out = append(out, hit{Rel: rel, Context: s})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Context < out[j].Context })
	return out, nil
}

// absolutePaths pulls absolute-looking byte runs out of a file.
//
// The thresholds exist to keep the result readable rather than to be clever,
// and each one was chosen against a real bottle:
//
//   - at least two separators and eight characters, so `/x` and `/dev` do not
//     flood the report;
//   - a run starting `/..` is a RELATIVE rpath fragment (`@loader_path/../..`)
//     caught mid-string, not an absolute path;
//   - a run starting `//` is almost always a URL remnant (`//groups.google.com/`).
func absolutePaths(b []byte) []string {
	var out []string
	for i := 0; i < len(b); i++ {
		if b[i] != '/' {
			continue
		}
		// ⛔ A SLASH IS ONLY THE START OF AN ABSOLUTE PATH IF WHAT PRECEDES IT
		// IS NOT PART OF A PATH. Scanning from the slash alone turns
		// `@rpath/p/v1/lib/libp.dylib` into `/p/v1/lib/libp.dylib` — a
		// RELATIVE load path reported as an absolute one, and `@rpath` is in
		// every well-formed bottle, so the check would fail the clean case it
		// exists to certify. Same for `@loader_path/../..` and `$ORIGIN/..`.
		if i > 0 && absPathByte(b[i-1]) {
			continue
		}
		j := i
		for j < len(b) && absPathByte(b[j]) {
			j++
		}
		s := string(b[i:j])
		i = j
		if len(s) < 8 || strings.Count(s, "/") < 2 {
			continue
		}
		if strings.HasPrefix(s, "/..") || strings.HasPrefix(s, "//") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// absPathByte is a STRICT path alphabet, unlike isPathByte.
//
// ⛔ THE LOOSE ONE PRODUCED NONSENSE HERE. `isPathByte` accepts anything above
// a space, which is right for recovering the path AROUND a known match but
// wrong for deciding what IS a path: it swallowed `sed` expressions out of
// bzgrep (`/&/\\&/g`, `/.bz2$//`) and C comment rules out of bzlib.h
// (`/*------`), and reported them as baked absolute paths. A check whose
// output is mostly false positives gets switched off, so the alphabet is the
// conservative one and a real path with an exotic character is missed rather
// than a comment being named.
func absPathByte(c byte) bool {
	return c == '/' || c == '.' || c == '-' || c == '_' || c == '+' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func allowedAbsolute(s string) bool {
	for _, p := range allowedPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// reportAllowed prints what the run forgave, so a zero can be read.
//
// ⛔ A CHECK THAT DOES NOT SAY WHAT IT SEARCHED FOR CANNOT BE TRUSTED WHEN IT
// FINDS NOTHING. That is the whole lesson of the `/home/runner` zero: the
// output said "clean" and the reader had no way to see that the question was
// wrong.
func reportAllowed(w interface{ Write([]byte) (int, error) }) {
	fmt.Fprintf(w, "absolute paths are reported unless they start with one of:\n  %s\n\n",
		strings.Join(allowedPrefixes, "  "))
}
