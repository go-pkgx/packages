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
	if code := run(p, bkOnly(), &out, &errb); code != 1 {
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
	if code := run(p, bkOnly(), &out, &errb); code != 0 {
		t.Fatalf("code = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "every one the order builds is pinned as asked") {
		t.Errorf("out = %q", out.String())
	}
}

// A pin that is not bk's is worse than none: it reads as deliberate.
func TestTheWrongPinIsRefused(t *testing.T) {
	p := writeOrder(t, "gnu.org/gawk@~5.4\n")
	var out, errb bytes.Buffer
	if code := run(p, bkOnly(), &out, &errb); code != 1 {
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
	if code := run(p, bkOnly(), &out, &errb); code != 0 {
		t.Fatalf("code = %d: %s", code, errb.String())
	}
}

func TestAnUnreadableOrderIsAnError(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(filepath.Join(t.TempDir(), "absent"), bkOnly(), &out, &errb); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

// An empty order agrees with everything, which is not agreement.
func TestAnEmptyOrderIsAnError(t *testing.T) {
	p := writeOrder(t, "# nothing but a comment\n")
	var out, errb bytes.Buffer
	if code := run(p, bkOnly(), &out, &errb); code != 2 {
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
	// "" is bk's list alone — what this command read before the sovereign
	// toolchain was a second source.
	os.Args = []string{"seedpins", "--order", p, "--builder-toolchain", ""}
	main()
	if got != 0 {
		t.Errorf("a pinned order exited %d", got)
	}

	// And with the second list, whose pin the order also carries.
	tc := filepath.Join(t.TempDir(), "toolchain.txt")
	if err := os.WriteFile(tc, []byte("kernel.org/linux-headers@~7.2  # why\nllvm.org\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2 := writeOrder(t, "gnu.org/gawk@~5.3\nperl.org@~5.44\nkernel.org/linux-headers@~7.2\n")
	os.Args = []string{"seedpins", "--order", p2, "--builder-toolchain", tc}
	main()
	if got != 0 {
		t.Errorf("an order pinned for BOTH lists exited %d", got)
	}

	// A toolchain list that is not there is an operator error, not a verdict
	// about the order: exit 2, like an unreadable order.
	os.Args = []string{"seedpins", "--order", p, "--builder-toolchain",
		filepath.Join(t.TempDir(), "absent.txt")}
	main()
	if got != 2 {
		t.Errorf("a missing toolchain list exited %d, want 2", got)
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
	if code := run(p, []toolchainSource{{name: "a list", specs: []string{"~1.2"}}}, &out, &errb); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

// bkOnly is what this command checked before builder/toolchain.txt was read
// too: bk's base toolchain alone. The tests that predate the second source
// use it, so they go on asking exactly the question they were written for.
func bkOnly() []toolchainSource {
	return []toolchainSource{{name: "bk's base toolchain", specs: build.BaseToolchain()}}
}

// builder/toolchain.txt is mostly comment: entries carry a trailing `# why`,
// and several explanations run over lines of their own. A parser that took a
// comment line for a package would demand a pin on `#`.
func TestParseBuilderToolchainIgnoresComments(t *testing.T) {
	const in = `# the build toolchain, bottles only

llvm.org                    # clang, lld, compiler-rt
kernel.org/linux-headers@~7.2
                            # PINNED so every architecture stages the SAME
                            # headers.
gnu.org/tar
`
	got, err := parseBuilderToolchain(strings.NewReader(in), "toolchain.txt")
	if err != nil {
		t.Fatal(err)
	}
	// pkgspec form: the `@` comes off, because bk's list writes it that way
	// and the comparison has to see one shape.
	want := []string{"llvm.org", "kernel.org/linux-headers~7.2", "gnu.org/tar"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// All comment and no package: an empty toolchain agrees with every order,
// which is the silent pass this whole command exists to prevent.
func TestParseBuilderToolchainRefusesAnEmptyList(t *testing.T) {
	_, err := parseBuilderToolchain(strings.NewReader("# nothing here\n\n"), "toolchain.txt")
	if err == nil || !strings.Contains(err.Error(), "names no package") {
		t.Errorf("err = %v, want a refusal naming the empty list", err)
	}
}

// A read that fails mid-file must not be read as a shorter toolchain.
func TestParseBuilderToolchainSurfacesAReadFailure(t *testing.T) {
	_, err := parseBuilderToolchain(iotest.TimeoutReader(strings.NewReader("llvm.org\ngnu.org/tar\n")), "toolchain.txt")
	if err == nil {
		t.Error("a truncated toolchain read without error")
	}
}

// Two lists pinning one package to two versions. It cannot happen today — bk
// constrains gawk and perl, builder/toolchain.txt constrains linux-headers —
// and it is the next thing to go wrong when somebody pins the same package
// twice, in two files, for two reasons. Reported against the LISTS, not the
// order: the order cannot satisfy both and it is not the order's fault.
func TestTwoListsThatDisagreeAreReported(t *testing.T) {
	p := writeOrder(t, "gnu.org/gawk@~5.3\n")
	var out, errb bytes.Buffer
	code := run(p, []toolchainSource{
		{name: "list A", specs: []string{"gnu.org/gawk~5.3"}},
		{name: "list B", specs: []string{"gnu.org/gawk~5.4"}},
	}, &out, &errb)
	if code != 1 {
		t.Fatalf("code = %d, want 1: %s", code, errb.String())
	}
	for _, w := range []string{"constrained twice and differently", "list A", "list B", "~5.3", "~5.4"} {
		if !strings.Contains(errb.String(), w) {
			t.Errorf("missing %q in:\n%s", w, errb.String())
		}
	}
}

// The same package in both lists, pinned the SAME way, is not a disagreement
// — it is two files that happen to agree, and refusing it would make keeping
// them consistent an error.
func TestTwoListsThatAgreeAreFine(t *testing.T) {
	p := writeOrder(t, "gnu.org/gawk@~5.3\n")
	var out, errb bytes.Buffer
	if code := run(p, []toolchainSource{
		{name: "list A", specs: []string{"gnu.org/gawk~5.3"}},
		{name: "list B", specs: []string{"gnu.org/gawk~5.3"}},
	}, &out, &errb); code != 0 {
		t.Fatalf("code = %d: %s", code, errb.String())
	}
}

// A malformed entry in the SECOND list must name that list. The message is
// the whole point of carrying a name: the two files are maintained by
// different people for different reasons.
func TestAMalformedEntryNamesItsList(t *testing.T) {
	p := writeOrder(t, "gnu.org/gawk@~5.3\n")
	var out, errb bytes.Buffer
	if code := run(p, []toolchainSource{
		{name: "bk's base toolchain", specs: []string{"gnu.org/gawk~5.3"}},
		{name: "builder/toolchain.txt", specs: []string{"~1.2"}},
	}, &out, &errb); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "builder/toolchain.txt") {
		t.Errorf("the refusal does not name the list:\n%s", errb.String())
	}
}
