package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/mod/semver"
)

// ⛔⛔ A LANE PINNED BEHIND THE FACTORY PUBLISHES BOTTLES THE REGISTRY CANNOT
// DESCRIBE, AND NOTHING SAID SO.
//
// This is the second time a pin in this repository has drifted behind what
// the registry needs, and the first time cost a whole platform's worth of
// artefacts: three lanes — wasip1, wasip1-c, windows-c — sat at zbk v0.59.0
// while the rest ran @main. v0.59.0 was chosen as a FLOOR, for a reason that
// had nothing to do with features: Go verifies the path a module declares,
// and every earlier tag declares `github.com/go-pkgx/bk`. Nobody revisited it
// when the floor stopped being enough.
//
// What those lanes would have published:
//
//   - no build identity at all (builds gained a hash in zbk v0.62.0);
//   - or an identity that is not a Merkle hash of its closure (v0.63.0 fixed
//     a composed dependency path that made every edge contribute an empty
//     hash);
//   - or a signature referrer with no signer key id (v0.64.0), which a
//     verifier holding several trusted keys has to brute-force.
//
// `zpkgs builds` can only report such a bottle as `nothing recorded`. That is
// indistinguishable, to a reader, from a bottle we never built — which is the
// whole failure this gate exists to stop.
//
// ⛔ A REPEATED LESSON BECOMES A GATE, NOT A COMMENT. The comments beside
// those pins were already accurate and already explained the floor; being
// right in prose is what let them stay wrong in fact.
//
// ⛔⛔ THE FLOOR IS "WHAT A PUBLISHING LANE MUST BE ABLE TO DO", NOT "WHEN THE
// HASH APPEARED". It moved to v0.65.0 for a reason that is not about
// identity at all: from that tag the factory REFUSES to publish bytes that
// differ from what a hashed tag already holds. A lane pinned at v0.64.0
// computes the right identity and will still overwrite an address, rewriting
// what every existing lock, install and signature refers to — the exact
// failure the hashed layout was introduced to make impossible, through the
// one door the hash does not cover (go-zpkg/zbk#416).
//
// So this constant rises whenever a zbk release adds something a publishing
// lane must not be without. It is not a "latest" check and must never become
// one: a lane pinned behind for a reason of its own is fine until it is
// behind THIS.
const zbkIdentityFloor = "v0.65.0"

// zbkFloorReason says what THIS floor buys, and moves with it.
//
// ⛔⛔ A FIXED SENTENCE WOULD HAVE LIED THE FIRST TIME THE FLOOR MOVED. The
// message used to read "a bottle built by it carries no usable build
// identity", which was exactly right while the floor was v0.64.0 and became
// false the moment it was v0.65.0 — a lane at v0.64.0 computes a perfectly
// good identity and simply will not refuse to overwrite one. A failure that
// names the wrong cause sends the reader to fix the wrong thing, and it reads
// as authoritative because it came from a gate.
const zbkFloorReason = "from v0.65.0 the factory refuses to publish bytes that differ from " +
	"what a hashed tag already holds, so a lane below it can still overwrite an address " +
	"(go-zpkg/zbk#416)"

// zbkPinRE matches an install or run of our factory at an explicit version.
// `@main` is deliberately NOT matched: this repository runs most lanes from
// main on purpose, and main is by definition at or ahead of any tag.
var zbkPinRE = regexp.MustCompile(`github\.com/go-zpkg/zbk/cmd/zbk@(v[0-9][^\s"']*)`)

// ⛔ THE DENOMINATOR IS ASSERTED. A regex that stops matching reports zero
// pins and passes, which is how a scan that could not read reports a clean
// result. If the spelling of these invocations changes, this fails and says
// so rather than going quietly green.
const zbkPinsExpected = 3

func TestEveryPinnedFactoryCanRecordABuildIdentity(t *testing.T) {
	dir := filepath.Join(".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	found := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range zbkPinRE.FindAllStringSubmatch(string(body), -1) {
			found++
			got := m[1]
			if !semver.IsValid(got) {
				t.Errorf("%s pins zbk at %q, which is not a version", path, got)
				continue
			}
			if semver.Compare(got, zbkIdentityFloor) < 0 {
				t.Errorf("%s pins zbk at %s, below %s — %s", path, got, zbkIdentityFloor, zbkFloorReason)
			}
		}
	}

	if found != zbkPinsExpected {
		t.Errorf("found %d pinned zbk invocation(s), expected %d — if a lane was added, removed or respelt, "+
			"move zbkPinsExpected deliberately; a count that drifts on its own is how this gate stops looking",
			found, zbkPinsExpected)
	}
}

// The module's own dependency on zbk must clear the same floor: the tools in
// this repository link it, and one that computed an identity a different way
// from the factory would be a second definition of the same thing.
func TestTheModuleDependsOnAFactoryThatRecordsIdentities(t *testing.T) {
	body, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`github\.com/go-zpkg/zbk (v[^\s]+)`)
	m := re.FindStringSubmatch(string(body))
	if m == nil {
		t.Fatal("go.mod does not require github.com/go-zpkg/zbk — if that is deliberate, this test should go with it")
	}
	if semver.Compare(m[1], zbkIdentityFloor) < 0 {
		t.Errorf("go.mod requires zbk %s, below %s — %s", m[1], zbkIdentityFloor, zbkFloorReason)
	}
}
