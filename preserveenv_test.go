package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ⛔⛔ A VARIABLE THE INNER SCRIPT READS BUT sudo DOES NOT PRESERVE IS DROPPED
// SILENTLY, AND THE BUILD DOES SOMETHING ELSE.
//
// `builder/sovereign-build.sh` runs the real work through
// `sudo --preserve-env=A,B,C unshare … chroot …`. sudo clears the environment
// except the names on that list. A workflow that sets a variable missing from
// it does not fail — the inner script simply sees it unset and takes the
// other branch.
//
// Found the hard way: `NO_PUBLISH=1`, added so a recipe could be built twice
// and the two bottles compared without pushing, was set in the workflow and
// absent from the list. The run would have PUBLISHED. Every other name on
// that line changes what gets built; that one changes whether the result is
// irreversible.
//
// So the invariant is derived from the script, not from a list somebody
// maintains beside it: whatever the inner script READS, sudo must PRESERVE.
func TestEveryVariableTheInnerScriptReadsIsPreserved(t *testing.T) {
	b, err := os.ReadFile("builder/sovereign-build.sh")
	if err != nil {
		t.Fatalf("no sovereign-build.sh to check: %v", err)
	}
	src := string(b)

	preserved := preserveList(t, src)
	if len(preserved) < 10 {
		// A parse that found almost nothing would make this test pass by
		// having no expectations, which is the way a guard like this rots.
		t.Fatalf("only %d preserved name(s) parsed — the --preserve-env line has moved", len(preserved))
	}

	read := readVars(innerScript(t, src))
	if len(read) == 0 {
		t.Fatal("no variables parsed out of the inner script — the quoting has changed")
	}

	var missing []string
	for _, v := range read {
		if !preserved[v] {
			missing = append(missing, v)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("the inner script reads %v, which sudo does not preserve — "+
			"they will be UNSET inside the namespace and the script will take the other branch",
			missing)
	}
	t.Logf("%d variable(s) read by the inner script, %d preserved", len(read), len(preserved))
}

// ⛔⛔ THE TEST ABOVE IS NARROWER THAN IT LOOKS, AND SAYING SO IS THE POINT.
// It finds what the SHELL expands inside the namespace. It does NOT find what
// the Go program reads from its own environment — `bk factory` takes most of
// its settings through `envOr`/`envBool`, and those names never appear in the
// script at all.
//
// `NO_PUBLISH` is exactly that: read by `bk factory`, invisible to a shell
// parse, and the variable whose loss is irreversible. The test above would
// not have caught it. This one is the other half.
//
// ⛔ MAINTAINED BY HAND, BECAUSE IT CROSSES A REPOSITORY. The list comes from
// `cmd/zbk/factory.go`'s envOr/envBool calls in go-zpkg/zbk; nothing here can
// read that file, so this is a copy and will drift. It is still worth having:
// a copy that fails loudly when the factory gains a knob the builder cannot
// pass beats no check at all, and the failure names the variable.
func TestTheFactorysEnvironmentKnobsSurviveSudo(t *testing.T) {
	b, err := os.ReadFile("builder/sovereign-build.sh")
	if err != nil {
		t.Fatalf("no sovereign-build.sh to check: %v", err)
	}
	preserved := preserveList(t, string(b))

	// From cmd/zbk/factory.go, read 2026-11-04. Deliberately NOT every name:
	// PANTRY, RECIPES-by-file and PANTRY_OVERLAY_DIR are passed as explicit
	// flags by this script, and DIST is set as ZPKG_DIST.
	for _, v := range []string{
		"FORCE", "NO_CLOSURE", "NO_PUBLISH", "REBUILD", "TEST_ONLY",
		"KEEP_BUILD_TREES", "PIN_TO_DEPENDENTS",
		"RECIPES", "LOCK", "VERSIONS", "COMPRESS",
		"SOURCE_MIRROR", "ADVISORIES_DIR", "PLATFORM",
	} {
		if !preserved[v] {
			t.Errorf("`bk factory` reads %s from the environment and sudo does not preserve it — "+
				"setting it in a workflow would be silently ignored", v)
		}
	}
}

var preserveRe = regexp.MustCompile(`--preserve-env=([A-Z0-9_,]+)`)

func preserveList(t *testing.T, src string) map[string]bool {
	t.Helper()
	// ⛔ THE LONGEST MATCH, NOT THE FIRST. The script QUOTES an old failure in
	// a comment —
	//
	//   builder/sovereign-build.sh: line 94: --preserve-env=OCI_USERNAME,…
	//
	// — and that line is a record of an incident, not the real list. Taking
	// the first match parsed one name out of a comment and this test then
	// "passed" by expecting almost nothing, which is how a guard rots into
	// decoration.
	all := preserveRe.FindAllStringSubmatch(src, -1)
	if all == nil {
		t.Fatal("no --preserve-env= found in sovereign-build.sh")
	}
	best := ""
	for _, m := range all {
		if len(m[1]) > len(best) {
			best = m[1]
		}
	}
	out := map[string]bool{}
	for _, n := range strings.Split(best, ",") {
		if n != "" {
			out[n] = true
		}
	}
	return out
}

// innerScript returns the single-quoted bash program handed to unshare. Only
// THAT text runs on the far side of sudo; a variable used before it is on the
// host and needs no preserving.
func innerScript(t *testing.T, src string) string {
	t.Helper()
	i := strings.Index(src, "unshare --mount --pid --fork")
	if i < 0 {
		t.Fatal("the unshare invocation has moved")
	}
	rest := src[i:]
	start := strings.Index(rest, "'")
	if start < 0 {
		t.Fatal("the inner script is no longer single-quoted")
	}
	rest = rest[start+1:]
	end := strings.Index(rest, "'")
	if end < 0 {
		t.Fatal("the inner script's closing quote has gone")
	}
	return rest[:end]
}

// readVars finds `$NAME` and `${NAME...}` uses of SHOUTED names.
//
// ⛔ POSITIONALS AND LOCALS ARE NOT ENVIRONMENT. The inner script takes its
// paths as `$1`..`$5` and assigns lowercase locals (`r`, `pantry`, `ov`);
// neither crosses sudo and neither belongs on the preserve list. Requiring
// them would make this test fail for a reason that is not a defect, and a
// test that cries wolf gets deleted.
var varRe = regexp.MustCompile(`\$\{?([A-Z][A-Z0-9_]{2,})\}?`)

func readVars(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range varRe.FindAllStringSubmatch(s, -1) {
		n := m[1]
		// Set by the shell or by the chroot itself, never by a caller.
		switch n {
		case "PATH", "PWD", "IFS", "HOSTNAME", "SHLVL":
			continue
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
