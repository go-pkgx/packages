package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ⛔⛔ .git IS NOT COPIED, AND THAT IS THE FIX FOR A RACE, not tidiness.
//
// copyTree shelled out to `cp -R src/. dst` and copied the whole clone, so it
// raced git's own background maintenance:
//
//	cp: cannot stat '…/.git/objects/maintenance.lock': No such file or directory
//
// git created the lock and removed it between cp's readdir and its stat — a
// flake in CI, and in the field a whole run failed for a file nothing here
// was ever going to read. Not copying the one directory that mutates
// underneath the copy removes the race at its source.
func TestCopyTreeSkipsGit(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "projects", "acme.org", "package.yml"), "x: 1\n", 0o644)
	// The shape that broke it: a .git with a file that would vanish.
	mustWrite(t, filepath.Join(src, ".git", "objects", "maintenance.lock"), "", 0o644)
	mustWrite(t, filepath.Join(src, ".git", "HEAD"), "ref: refs/heads/main\n", 0o644)

	dst, err := copyTree(src)
	if err != nil {
		t.Fatalf("copyTree() = %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dst) })

	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Errorf(".git was copied (stat err = %v)", err)
	}
	// And the thing it exists to copy is there.
	if b, err := os.ReadFile(filepath.Join(dst, "projects", "acme.org", "package.yml")); err != nil || string(b) != "x: 1\n" {
		t.Errorf("recipe = %q, %v", b, err)
	}
}

// A pantry carries symlinks, and following one would copy its target into the
// tree under the link's name — quietly changing what a recipe says.
func TestCopyTreeRecreatesSymlinks(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "real.yml"), "real\n", 0o644)
	if err := os.Symlink("real.yml", filepath.Join(src, "link.yml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	dst, err := copyTree(src)
	if err != nil {
		t.Fatalf("copyTree() = %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dst) })

	got, err := os.Readlink(filepath.Join(dst, "link.yml"))
	if err != nil {
		t.Fatalf("the link was not recreated as a link: %v", err)
	}
	if got != "real.yml" {
		t.Errorf("link → %q, want real.yml", got)
	}
}

// The executable bit survives: a pantry ships scripts, and a copy that
// dropped the mode would fail at a step far from here.
func TestCopyTreeKeepsTheMode(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "run.sh"), "#!/bin/sh\n", 0o755)

	dst, err := copyTree(src)
	if err != nil {
		t.Fatalf("copyTree() = %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dst) })

	fi, err := os.Stat(filepath.Join(dst, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode = %v, want the executable bit kept", fi.Mode().Perm())
	}
}

// An unreadable source file is an error naming the copy, not a silently
// short tree that would then "prove" the conversion changed nothing.
func TestCopyTreeReportsAnUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads anything, so the refusal cannot be staged")
	}
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "secret.yml"), "x\n", 0o000)

	dst, err := copyTree(src)
	if err == nil {
		os.RemoveAll(dst)
		t.Fatal("an unreadable file was copied over in silence")
	}
	if !strings.Contains(err.Error(), "copy pantry") {
		t.Errorf("err = %v, want it to name the copy", err)
	}
}

func mustWrite(t *testing.T, p, body string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), perm); err != nil {
		t.Fatal(err)
	}
}
