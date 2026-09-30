package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/go-pkgx/bk/build"
)

func writeOrder(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "order.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The order the seed actually shipped: gawk unpinned, so it built 5.4.1 and
// bk's base toolchain refused it.
func TestAnUnpinnedToolchainMemberIsRefused(t *testing.T) {
	p := writeOrder(t, "zlib.net\ngnu.org/gawk\nperl.org\n")
	var out, errb bytes.Buffer
	if code := run(p, build.BaseToolchain(), &out, &errb); code != 1 {
		t.Fatalf("code = %d, want 1 (%s)", code, errb.String())
	}
	for _, want := range []string{"gnu.org/gawk", "perl.org", "write: gnu.org/gawk@~5.3"} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("want %q in:\n%s", want, errb.String())
		}
	}
}

// Pinned as bk asks: nothing to say.
func TestAPinnedOrderPasses(t *testing.T) {
	p := writeOrder(t, "# a comment\n\nzlib.net\ngnu.org/gawk@~5.3\nperl.org@~5.44\n")
	var out, errb bytes.Buffer
	if code := run(p, build.BaseToolchain(), &out, &errb); code != 0 {
		t.Fatalf("code = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "every one the order builds is pinned as bk asks") {
		t.Errorf("out = %q", out.String())
	}
}

// A pin that is not bk's is worse than none: it reads as deliberate.
func TestTheWrongPinIsRefused(t *testing.T) {
	p := writeOrder(t, "gnu.org/gawk@~5.4\n")
	var out, errb bytes.Buffer
	if code := run(p, build.BaseToolchain(), &out, &errb); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), `"gnu.org/gawk@~5.4"`) || !strings.Contains(errb.String(), `"gnu.org/gawk~5.3"`) {
		t.Errorf("both sides must be shown: %q", errb.String())
	}
}

// A toolchain member the order does not build is not this check's business:
// the machine may already have one, and demanding a line would be inventing
// a requirement.
func TestAMemberTheOrderDoesNotBuildIsNotDemanded(t *testing.T) {
	p := writeOrder(t, "zlib.net\n")
	var out, errb bytes.Buffer
	if code := run(p, build.BaseToolchain(), &out, &errb); code != 0 {
		t.Fatalf("code = %d: %s", code, errb.String())
	}
}

func TestAnUnreadableOrderIsAnError(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(filepath.Join(t.TempDir(), "absent"), build.BaseToolchain(), &out, &errb); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

// An empty order agrees with everything, which is not agreement.
func TestAnEmptyOrderIsAnError(t *testing.T) {
	p := writeOrder(t, "# nothing but a comment\n")
	var out, errb bytes.Buffer
	if code := run(p, build.BaseToolchain(), &out, &errb); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "agrees with everything") {
		t.Errorf("errb = %q", errb.String())
	}
}

// The constraints come from bk, never from a copy here — a second "~5.3" in
// this repository would be right until bk moved and then wrong silently.
func TestTheConstraintsComeFromBk(t *testing.T) {
	got, err := constrainedToolchain(build.BaseToolchain())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("bk's base toolchain declares no constraint at all — either it changed or this is reading the wrong thing")
	}
	for proj, c := range got {
		if c == "" || !strings.ContainsAny(c, "@^~<>=") {
			t.Errorf("%s: %q is not a constraint", proj, c)
		}
	}
}

func TestSpecRendersTheLineToWrite(t *testing.T) {
	if got := spec("a.org", "~1.2"); got != "a.org@~1.2" {
		t.Errorf("got %q", got)
	}
	if got := spec("a.org", ""); got != "a.org (no pin)" {
		t.Errorf("got %q", got)
	}
}

// main() through the seam, as overlaycheck does in this repository.
func TestMainReportsThroughOsExit(t *testing.T) {
	oldArgs, oldExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = oldArgs, oldExit })
	var got int
	osExit = func(c int) { got = c }

	p := writeOrder(t, "gnu.org/gawk@~5.3\nperl.org@~5.44\n")
	os.Args = []string{"seedpins", "--order", p}
	main()
	if got != 0 {
		t.Errorf("a pinned order exited %d", got)
	}

	os.Args = []string{"seedpins", "--not-a-flag"}
	main()
	if got != 2 {
		t.Errorf("a bad flag exited %d, want 2", got)
	}
}

// A bk entry that is a bare constraint names no project. It cannot happen
// today; a refusal nobody can exercise is a refusal nobody has read.
func TestABaseToolchainEntryWithNoProjectIsRefused(t *testing.T) {
	if _, err := constrainedToolchain([]string{"~1.2"}); err == nil {
		t.Error("want a refusal for an entry with no project")
	}
	got, err := constrainedToolchain([]string{"a.org", "b.org~2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["b.org"] != "~2" {
		t.Errorf("got %v", got)
	}
}

// A reader that fails mid-file must not read as a shorter order: truncation
// would turn this check into agreement.
func TestATruncatedOrderIsAnErrorNotAShorterOne(t *testing.T) {
	_, err := parseOrder(iotest.TimeoutReader(strings.NewReader("zlib.net\ngnu.org/gawk@~5.3\n")), "order.txt")
	if err == nil {
		t.Error("want the read error surfaced, not swallowed")
	}
}

// A malformed base toolchain is refused rather than silently yielding an
// empty pin set, which would make every order agree.
func TestRunRefusesAMalformedToolchain(t *testing.T) {
	p := writeOrder(t, "zlib.net\n")
	var out, errb bytes.Buffer
	if code := run(p, []string{"~1.2"}, &out, &errb); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}
