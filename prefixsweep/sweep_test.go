package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pkgx/bottle"
)

// fakeRegistry drives run() through the two seams, so everything the sweep
// does with an installed tree is tested without reaching ghcr.io.
//
// Each project maps to the files its bottle "unpacks", keyed by the path
// under <store>/<project>/.
type fakeRegistry struct {
	versions map[string]string            // project -> version the registry offers
	trees    map[string]map[string]string // project -> {relative path: content}
	pickErr  map[string]error
	instErr  map[string]error
	// unreadable names, per project, one installed file to make unreadable
	// AFTER a successful install.
	unreadable map[string]string
}

func (f *fakeRegistry) install(t *testing.T) {
	t.Helper()
	oldPick, oldInstall := pickVersion, install
	t.Cleanup(func() { pickVersion, install = oldPick, oldInstall })

	pickVersion = func(project, _, _, _ string) (bottle.Ver, error) {
		if err := f.pickErr[project]; err != nil {
			return bottle.Ver{}, err
		}
		v, ok := f.versions[project]
		if !ok {
			return bottle.Ver{}, fmt.Errorf("no such project %q", project)
		}
		return bottle.Ver{Raw: v}, nil
	}
	install = func(r bottle.Resolved, dir, _, _ string) (bool, error) {
		if err := f.instErr[r.Project]; err != nil {
			return false, err
		}
		for rel, content := range f.trees[r.Project] {
			path := filepath.Join(dir, filepath.FromSlash(r.Project), filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return false, err
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return false, err
			}
			if f.unreadable[r.Project] == rel {
				// ⛔ THE FILE IS MADE UNREADABLE BY THE INSTALL ITSELF, so the
				// install SUCCEEDS and only the later read fails. An earlier
				// version of this test chmod'd the file before running, which
				// made the install fail instead — it still returned an error,
				// so the test passed, while the branch it named was never
				// executed. A check that passes for the wrong reason.
				if err := os.Chmod(path, 0o000); err != nil {
					return false, err
				}
			}
		}
		return true, nil
	}
}

func sweep(t *testing.T, f *fakeRegistry, args ...string) (string, error) {
	t.Helper()
	f.install(t)
	var out, errOut strings.Builder
	all := append([]string{"-platform", "darwin/aarch64", "-store", t.TempDir()}, args...)
	err := run(all, &out, &errOut)
	return out.String(), err
}

// The whole report, on a store holding one baked bottle and one clean one.
func TestTheReportNamesWhatBakesAndWhatDoesNot(t *testing.T) {
	f := &fakeRegistry{
		versions: map[string]string{"baked.example": "1.0.0", "clean.example": "2.0.0"},
		trees: map[string]map[string]string{
			"baked.example": {"v1.0.0/lib/libb.dylib": "@rpath/baked.example/v1.0.0/lib/libb.dylib\x00"},
			"clean.example": {"v2.0.0/bin/c": "no path in here, just v2.0.0 as a token\n"},
		},
	}
	out, err := sweep(t, f, "baked.example", "clean.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"⛔ BAKED", "baked.example", "v1.0.0/lib/libb.dylib",
		"@rpath/baked.example/v1.0.0/lib/libb.dylib",
		"clean", "clean.example",
		"2 swept · 1 bake their own prefix · 1 clean · 0 unreadable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not contain %q:\n%s", want, out)
		}
	}
}

// ⛔ MORE THAN FIVE HITS MUST SAY SO. Truncating the list silently would make
// a project with forty baked files look like one with five, and this tool's
// output is quoted in issues.
func TestALongHitListSaysHowManyItTruncated(t *testing.T) {
	tree := map[string]string{}
	for i := range 8 {
		tree[fmt.Sprintf("v1.0.0/bin/t%d", i)] = "/opt/p/v1.0.0/lib/x\n"
	}
	f := &fakeRegistry{
		versions: map[string]string{"many.example": "1.0.0"},
		trees:    map[string]map[string]string{"many.example": tree},
	}
	out, err := sweep(t, f, "many.example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "8 file(s)") {
		t.Errorf("the total was not reported:\n%s", out)
	}
	if !strings.Contains(out, "… and 3 more") {
		t.Errorf("the truncation was not reported:\n%s", out)
	}
}

// ⛔ A PROJECT THAT COULD NOT BE READ IS NOT A CLEAN ONE. Each failure path
// has to land in `unreadable`, never in the `clean` column, or the sweep
// becomes a census that could not read reporting zero.
func TestEveryFailurePathCountsAsUnreadableNotClean(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		name   string
		reg    *fakeRegistry
		marker string
	}{
		{
			name:   "the registry has no version",
			reg:    &fakeRegistry{pickErr: map[string]error{"p": boom}},
			marker: "SKIP",
		},
		{
			name: "the bottle does not install",
			reg: &fakeRegistry{
				versions: map[string]string{"p": "1.0.0"},
				instErr:  map[string]error{"p": boom},
			},
			marker: "UNREAD",
		},
		{
			name: "it installs nothing recognisable",
			reg: &fakeRegistry{
				versions: map[string]string{"p": "1.0.0"},
				trees:    map[string]map[string]string{"p": {"README": "no version dir"}},
			},
			marker: "UNREAD",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := sweep(t, tc.reg, "p")
			if err == nil {
				t.Fatal("a sweep where nothing could be read returned success")
			}
			if !strings.Contains(out, tc.marker) {
				t.Errorf("want %s in:\n%s", tc.marker, out)
			}
			if !strings.Contains(out, "0 swept · 0 bake their own prefix · 0 clean · 1 unreadable") {
				t.Errorf("the failure was not counted as unreadable:\n%s", out)
			}
		})
	}
}

// ⛔ AN UNREADABLE FILE INSIDE AN OTHERWISE FINE TREE FAILS THAT PROJECT. The
// file we could not read might be the one baked file, so "clean" would be a
// scan that could not read reporting zero.
//
// The install here SUCCEEDS and the file becomes unreadable as part of it, so
// the failure can only come from the read. That distinction is the test: see
// the comment on the fake's install.
func TestAnUnreadableFileFailsTheProjectAfterASuccessfulInstall(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file, so the mode cannot be exercised")
	}
	f := &fakeRegistry{
		versions:   map[string]string{"p": "1.0.0"},
		trees:      map[string]map[string]string{"p": {"v1.0.0/bin/t": "x"}},
		unreadable: map[string]string{"p": "v1.0.0/bin/t"},
	}
	out, err := sweep(t, f, "p")
	if err == nil {
		t.Fatal("an unreadable file did not fail the sweep")
	}
	if !strings.Contains(out, "UNREAD") {
		t.Errorf("want UNREAD in:\n%s", out)
	}
	if !strings.Contains(out, "0 swept · 0 bake their own prefix · 0 clean · 1 unreadable") {
		t.Errorf("an unreadable file was not counted as unreadable:\n%s", out)
	}
}

// -n stops after the given number, so a sweep can be sized before it is run.
func TestTheProjectCountCanBeCapped(t *testing.T) {
	f := &fakeRegistry{
		versions: map[string]string{"a.example": "1.0.0", "b.example": "1.0.0"},
		trees: map[string]map[string]string{
			"a.example": {"v1.0.0/bin/t": "/opt/a.example/v1.0.0/bin/t\n"},
			"b.example": {"v1.0.0/bin/t": "/opt/b.example/v1.0.0/bin/t\n"},
		},
	}
	out, err := sweep(t, f, "-n", "1", "a.example", "b.example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1 swept") {
		t.Errorf("-n did not cap the sweep:\n%s", out)
	}
	if strings.Contains(out, "b.example") {
		t.Errorf("-n swept past the cap:\n%s", out)
	}
}

// Names on the command line and names in -list are both swept.
func TestListedAndNamedProjectsAreBothSwept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipes.txt")
	if err := os.WriteFile(path, []byte("b.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeRegistry{
		versions: map[string]string{"a.example": "1.0.0", "b.example": "1.0.0"},
		trees: map[string]map[string]string{
			"a.example": {"v1.0.0/bin/t": "/opt/a.example/v1.0.0/bin/t\n"},
			"b.example": {"v1.0.0/bin/t": "/opt/b.example/v1.0.0/bin/t\n"},
		},
	}
	out, err := sweep(t, f, "-list", path, "a.example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2 swept") {
		t.Errorf("want both projects swept:\n%s", out)
	}
}

func TestAMissingListFileIsReported(t *testing.T) {
	f := &fakeRegistry{}
	if _, err := sweep(t, f, "-list", filepath.Join(t.TempDir(), "absent.txt")); err == nil {
		t.Error("a missing -list file was accepted")
	}
}

func TestNamingNoProjectIsRefused(t *testing.T) {
	var out, errOut strings.Builder
	if err := run([]string{"-platform", "darwin/aarch64"}, &out, &errOut); err == nil {
		t.Error("a sweep with no projects was accepted")
	}
}

func TestAnUnknownFlagIsReported(t *testing.T) {
	var out, errOut strings.Builder
	if err := run([]string{"-nonesuch"}, &out, &errOut); err == nil {
		t.Error("an unknown flag was accepted")
	}
}

// Without -store the sweep makes its own temporary directory and removes it.
func TestWithoutAStoreItMakesAndRemovesATemporaryOne(t *testing.T) {
	f := &fakeRegistry{
		versions: map[string]string{"p": "1.0.0"},
		trees:    map[string]map[string]string{"p": {"v1.0.0/bin/t": "/opt/p/v1.0.0/bin/t\n"}},
	}
	f.install(t)

	var seen string
	oldInstall := install
	install = func(r bottle.Resolved, dir, osn, arch string) (bool, error) {
		seen = dir
		return oldInstall(r, dir, osn, arch)
	}
	t.Cleanup(func() { install = oldInstall })

	var out, errOut strings.Builder
	if err := run([]string{"-platform", "darwin/aarch64", "p"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if seen == "" {
		t.Fatal("no store directory was created")
	}
	if _, err := os.Stat(seen); !os.IsNotExist(err) {
		t.Errorf("the temporary store %s was left behind", seen)
	}
}

// A project directory that is not a directory at all: the sweep says so
// rather than treating the project as having nothing baked.
func TestAProjectHomeThatIsNotADirectoryIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installedComponent(path); err == nil {
		t.Error("a file was accepted as a project home")
	}
}

// A directory the walk cannot descend into stops that project. Skipping it
// would leave part of the tree unexamined while still printing a verdict.
func TestADirectoryThatCannotBeWalkedFailsTheProject(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root descends into every directory, so the mode cannot be exercised")
	}
	home := t.TempDir()
	locked := filepath.Join(home, "v1.0.0", "lib")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if _, err := bakedPrefixes(home, "v1.0.0"); err == nil {
		t.Error("an undescendable directory was reported as holding nothing")
	}
}

// If the store cannot be created there is nowhere to install, and the sweep
// must say that rather than report a tidy "0 baked".
//
// ⛔ THE REGISTRY IS STUBBED TO SUCCEED, so the only thing that can fail is
// the store. Without that, this passed with TMPDIR untouched — the sweep
// errored because a made-up project does not resolve, and the branch it
// claimed to test never ran.
func TestAStoreThatCannotBeCreatedIsReported(t *testing.T) {
	f := &fakeRegistry{
		versions: map[string]string{"p": "1.0.0"},
		trees:    map[string]map[string]string{"p": {"v1.0.0/bin/t": "/opt/p/v1.0.0/bin/t\n"}},
	}
	f.install(t)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "absent", "deeper"))

	var out, errOut strings.Builder
	err := run([]string{"-platform", "darwin/aarch64", "p"}, &out, &errOut)
	if err == nil {
		t.Fatal("a sweep with no usable store returned success")
	}
	if strings.Contains(err.Error(), "measured nothing") {
		t.Errorf("the sweep ran and found nothing instead of failing on the store: %v", err)
	}
}

func TestMainReportsFailureThroughTheExitCode(t *testing.T) {
	oldArgs, oldExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = oldArgs, oldExit })

	code := 0
	osExit = func(c int) { code = c }
	os.Args = []string{"prefixsweep"} // no -platform
	main()

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// ⛔ THE NEEDLE SWEEP (go-pkgx/bk#387). Each test below fails for the reason it
// names: the positive one gives hits > 0 on a fixture that holds the cache
// root, so the zero in the negative one means something.
const cacheRoot = "/Users/runner/Library/Caches/brewkit"

func needleRegistry() *fakeRegistry {
	return &fakeRegistry{
		versions: map[string]string{"leaky.example": "1.0.0", "fixed.example": "1.0.0"},
		trees: map[string]map[string]string{
			// the shape in the issue: an assert string in a binary, a path in debug info
			"leaky.example": {
				"v1.0.0/bin/tool":   "assert: " + cacheRoot + "/darwin+aarch64/leaky.example/v1.0.0/build/x.c\x00",
				"v1.0.0/lib/liba.a": "clean bytes",
			},
			// normalised, plus a legitimate @rpath load command (bk#386), which is
			// out of scope and must NOT count.
			"fixed.example": {
				"v1.0.0/bin/tool": "assert: /build/darwin+aarch64/fixed.example/v1.0.0/build/x.c\x00@rpath/fixed.example/v1.0.0/lib/liba.dylib\x00",
			},
		},
	}
}

func TestTheNeedleIsFoundOnAFixtureThatContainsIt(t *testing.T) {
	out, err := sweep(t, needleRegistry(), "-needle", cacheRoot, "leaky.example")
	if err == nil {
		t.Fatalf("a bottle holding the cache root passed the needle sweep:\n%s", out)
	}
	for _, want := range []string{"⛔ BAKED", "leaky.example", "v1.0.0/bin/tool", cacheRoot, "1 swept · 1 bake their own prefix"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestTheNeedleSweepIsCleanOnANormalisedBottleAndIgnoresRpath(t *testing.T) {
	out, err := sweep(t, needleRegistry(), "-needle", cacheRoot, "fixed.example")
	if err != nil {
		t.Fatalf("a normalised bottle failed the needle sweep: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 swept · 0 bake their own prefix · 1 clean") {
		t.Errorf("want one clean project:\n%s", out)
	}
	// Control on the SAME fixture: the default sweep does flag the @rpath
	// version path, so the zero above comes from the needle, not from an empty tree.
	out, _ = sweep(t, needleRegistry(), "fixed.example")
	if !strings.Contains(out, "⛔ BAKED") {
		t.Errorf("control: the version-directory sweep should flag the @rpath entry:\n%s", out)
	}
}

// A needle sweep needs no v<version> directory: the layout is not its business.
func TestTheNeedleSweepDoesNotNeedAVersionDirectory(t *testing.T) {
	f := &fakeRegistry{
		versions: map[string]string{"p": "1.0.0"},
		trees:    map[string]map[string]string{"p": {"bin/t": "x"}},
	}
	if out, err := sweep(t, f, "-needle", cacheRoot, "p"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
