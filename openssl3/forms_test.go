package main

import "testing"

// TestDepLineCoversEveryStaleForm pins the six ways the pantry names the
// openssl 1.x line, and the one form that only LOOKS stale.
//
// The set is not guessed: each was checked against the published registry with
// bottle's own semver, and `>=1.1` came back satisfiable — it is unbounded
// above, so openssl 3.x meets it and the recipe already resolves. Matching it
// would rewrite a working pin.
func TestDepLineCoversEveryStaleForm(t *testing.T) {
	stale := []string{
		"  openssl.org: ^1.1",
		"  openssl.org: ^1",
		"  openssl.org: ^1.1.1",
		"  openssl.org: ^1.0.1",
		"  openssl.org: ^1.1.1k",
		"  openssl.org: 1.1",  // bare: a version is a range too
		"  openssl.org: 1",    //
		"  openssl.org: ~1",   // tilde
		"  openssl.org: ~1.1", //
		"    openssl.org: '1.1'",
		`    openssl.org: "1.1"`,
		"  openssl.org: ^1.1 # keep an eye on this",
	}
	for _, line := range stale {
		if !depLine.MatchString(line) {
			t.Errorf("stale pin not matched: %q", line)
		}
	}

	fine := []string{
		"  openssl.org: '*'",
		"  openssl.org: ^3",
		"  openssl.org: ^3.1.0",
		"  openssl.org: '>=3.0.0'",
		// Unbounded above: openssl 3.x satisfies it, so the recipe resolves and
		// the pin must be left alone.
		"  openssl.org: '>=1.1'",
		`  openssl.org: ">=1.1"`,
		// A different project that merely ends in the same name.
		"  libopenssl.org: ^1.1",
	}
	for _, line := range fine {
		if depLine.MatchString(line) {
			t.Errorf("a pin that needs no change was matched: %q", line)
		}
	}
}
