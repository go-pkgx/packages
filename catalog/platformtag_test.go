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
