package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-zpkg/bottle"
)

// askedFor records the constraint the resolver was handed, and stages an
// install of one innocuous file so the sweep has a tree to walk: the
// question here is what was ASKED, not what was found.
func askedFor(t *testing.T, reportVersion string) *[]string {
	t.Helper()
	oldPick, oldInstall := pickVersion, install
	t.Cleanup(func() { pickVersion, install = oldPick, oldInstall })

	var asked []string
	pickVersion = func(_, constraint, _, _ string) (bottle.Ver, error) {
		asked = append(asked, constraint)
		return bottle.Ver{Raw: reportVersion}, nil
	}
	install = func(r bottle.Resolved, dir, _, _ string) (bool, error) {
		p := filepath.Join(dir, filepath.FromSlash(r.Project), "v"+r.Version.Raw, "bin", "tool")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return false, err
		}
		return true, os.WriteFile(p, []byte("nothing absolute in here\n"), 0o644)
	}
	return &asked
}

// ⛔⛔ THE CONSTRAINT MUST REACH THE RESOLVER. A flag parsed and then ignored
// is this repository's recurring defect, and here it would be a wrong yes in
// the worst direction: the run would report "clean" about the NEWEST build
// while the operator believed they had asked about a specific one — reading
// as "the rebuild worked" when nothing of the sort was measured.
//
// The measured need: fftw.org publishes 3.3.11_1, which bakes
// /Users/runner/.pkgx into bin/fftw-wisdom, and 3.3.11, which is clean.
// Sweeping "the newest" can only ever report the broken one, so asking
// whether the REBUILD came out clean was not expressible at all.
func TestTheConstraintReachesTheResolver(t *testing.T) {
	asked := askedFor(t, "3.3.11")
	var out, errb strings.Builder
	err := run([]string{
		"-platform", "darwin/aarch64", "-store", t.TempDir(),
		"-constraint", "=3.3.11", "fftw.org",
	}, &out, &errb)
	if err != nil {
		t.Fatalf("run() = %v\n%s%s", err, out.String(), errb.String())
	}
	if len(*asked) != 1 || (*asked)[0] != "=3.3.11" {
		t.Errorf("resolver was asked %v, want [=3.3.11]", *asked)
	}
}

// And the DEFAULT is still the newest, because that is what a user gets: an
// exact pin is the exception a rebuild needs, never the ordinary sweep.
func TestTheDefaultConstraintIsTheNewest(t *testing.T) {
	asked := askedFor(t, "3.3.11_1")
	var out, errb strings.Builder
	err := run([]string{
		"-platform", "darwin/aarch64", "-store", t.TempDir(), "fftw.org",
	}, &out, &errb)
	if err != nil {
		t.Fatalf("run() = %v\n%s%s", err, out.String(), errb.String())
	}
	if len(*asked) != 1 || (*asked)[0] != "*" {
		t.Errorf("resolver was asked %v, want [*]", *asked)
	}
	// And the version reported is the one the RESOLVER returned, not the one
	// the operator typed: the sweep reports what landed.
	if !strings.Contains(out.String(), "3.3.11_1") {
		t.Errorf("output %q, want the resolved version named", out.String())
	}
}
