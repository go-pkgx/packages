package main

import "testing"

// TestSemverTagRejectsPlatformTags: go-pkgx/bottle#37 tags each platform's
// manifest `<ver>--<os>-<arch>` so a version's index can be composed from
// uncontended names. Those tags are NOT versions, and the first measurement
// after that change reported llvm.org's newest version as
// 19.1.0--darwin-aarch64 — every count this tool feeds would have inflated by
// roughly the number of platforms.
func TestSemverTagRejectsPlatformTags(t *testing.T) {
	for _, tag := range []string{
		"19.1.0--darwin-aarch64",
		"1.2.3--linux-x86-64",
		"2026.08.13--linux-aarch64",
	} {
		if semverTag(tag) {
			t.Errorf("%q was counted as a version", tag)
		}
	}
	// Real versions, including the shapes this registry actually carries.
	for _, tag := range []string{"1.2.3", "v1.2.3", "2026.08.13", "0.94n", "1.2.3-rc1"} {
		if !semverTag(tag) {
			t.Errorf("%q was rejected", tag)
		}
	}
	// And the two it already knew to skip.
	for _, tag := range []string{"", "sha256-abc", "latest"} {
		if semverTag(tag) {
			t.Errorf("%q was counted", tag)
		}
	}
}

// splitPlatformTag is the other half of semverTag: the tags it rejects as versions
// are the ones that name a platform's manifest, and they are now EVIDENCE
// rather than junk — the thing that says a lost index entry can be recomposed
// instead of rebuilt.
func TestSplitPlatformTag(t *testing.T) {
	for _, tag := range []string{
		"19.1.0--darwin-aarch64",
		"1.2.3--linux-x86-64",
		"2026.08.13--linux-aarch64",
		"v1.2.3--linux-x86-64",
	} {
		if _, _, ok := splitPlatformTag(tag); !ok {
			t.Errorf("%q was not recognised as a platform manifest", tag)
		}
	}
	// Narrow about the left half on purpose. Each of these would otherwise
	// claim a manifest exists for a version nothing ever published.
	for _, tag := range []string{
		"1.2.3",                    // a version, not a manifest
		"sha256-abc--linux-x86-64", // a digest with a suffix
		"--linux-x86-64",           // no version at all
		"latest--linux-x86-64",     // not a version
		"1.2.3--",                  // no platform
		"1.2.3--linux",             // no arch
		"",
	} {
		if _, _, ok := splitPlatformTag(tag); ok {
			t.Errorf("%q was accepted as a platform manifest", tag)
		}
	}
	// And the two predicates never both claim a tag.
	for _, tag := range []string{"1.2.3", "1.2.3--linux-x86-64", "latest", "sha256-abc"} {
		_, _, isManifest := splitPlatformTag(tag)
		if semverTag(tag) && isManifest {
			t.Errorf("%q counted as both a version and a manifest", tag)
		}
	}
}

// And what it READS, not just what it accepts: the platform comes back as
// `<os>/<arch>`, and only the FIRST hyphen separates them, because an arch is
// spelled x86-64 here. Cutting at the last one would turn linux/x86-64 into
// linux-x86/64 and the audit would compare a platform nothing publishes.
func TestSplitPlatformTagReadsThePlatform(t *testing.T) {
	for _, c := range []struct{ tag, ver, plat string }{
		{"1.2.3--linux-x86-64", "1.2.3", "linux/x86-64"},
		{"19.1.0--darwin-aarch64", "19.1.0", "darwin/aarch64"},
		{"2026.08.13--linux-aarch64", "2026.08.13", "linux/aarch64"},
	} {
		ver, plat, ok := splitPlatformTag(c.tag)
		if !ok || ver != c.ver || plat != c.plat {
			t.Errorf("splitPlatformTag(%q) = %q, %q, %v; want %q, %q", c.tag, ver, plat, ok, c.ver, c.plat)
		}
	}
}
