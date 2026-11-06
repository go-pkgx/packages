package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentifiedReadsTheHashAndNotTheShape(t *testing.T) {
	for _, c := range []struct {
		tag  string
		want bool
	}{
		{"1.3.2-hmjgwjhkpeyruxsts", true},
		{"1.3.2-hmjgwjhkpeyruxsts--wasip1-wasm", true},
		{"1.3.2-glibc2.28-hmjgwjhkpeyruxsts--linux-x86-64", true},
		{"1.3.2", false},
		{"1.3.2--wasip1-wasm", false},
		// Near misses: the marker is `-h` and exactly sixteen base32
		// characters. A version that merely contains a dash and an h is not
		// an identity, and a truncated hash is not one either.
		{"1.3.2-hotfix", false},
		{"1.3.2-hmjgwjhkpeyruxs", false},
		{"2.0.7-hmjgwjhkpeyruxstsx", false},
	} {
		if got := identified(c.tag); got != c.want {
			t.Errorf("identified(%q) = %v, want %v", c.tag, got, c.want)
		}
	}
}

func v(id int64, digest string, tags ...string) pkgVersion {
	var p pkgVersion
	p.ID = id
	p.Name = "sha256:" + digest
	p.Metadata.Container.Tags = tags
	return p
}

const (
	digBad  = "aaaa000000000000000000000000000000000000000000000000000000000001"
	digGood = "bbbb000000000000000000000000000000000000000000000000000000000002"
	digBoth = "cccc000000000000000000000000000000000000000000000000000000000003"
)

func deleted(vds []verdict) map[int64]string {
	out := map[int64]string{}
	for _, vd := range vds {
		if vd.delete {
			out[vd.v.ID] = vd.why
		}
	}
	return out
}

func TestClassifyDeletesTheUnidentifiedAndItsAttachments(t *testing.T) {
	got := deleted(classify([]pkgVersion{
		v(1, digBad, "1.3.2"),
		v(2, "aaaa000000000000000000000000000000000000000000000000000000000011", "1.3.2--wasip1-wasm"),
		v(3, digGood, "1.3.2-hmjgwjhkpeyruxsts"),
		v(4, "bbbb000000000000000000000000000000000000000000000000000000000022", "1.3.2-hmjgwjhkpeyruxsts--wasip1-wasm"),
		v(5, "dddd000000000000000000000000000000000000000000000000000000000004", "sha256-"+digBad),
		v(6, "eeee000000000000000000000000000000000000000000000000000000000005", "sha256-"+digGood),
		v(7, "ffff000000000000000000000000000000000000000000000000000000000006"),
	}))

	for _, id := range []int64{1, 2, 5} {
		if _, ok := got[id]; !ok {
			t.Errorf("version %d should have been deleted", id)
		}
	}
	for _, id := range []int64{3, 4, 6, 7} {
		if why, ok := got[id]; ok {
			t.Errorf("version %d should have been kept, deleted as %q", id, why)
		}
	}
	if len(got) != 3 {
		t.Errorf("deleted %d version(s), want 3", len(got))
	}
}

// ⛔⛔ THE QUESTION IS ASKED OF THE VERSION, NEVER OF THE TAG. The bottles are
// built reproducibly, so a republish can produce byte-identical bytes and the
// registry then holds ONE version wearing both the bare tag and the hashed
// one. Deciding per tag would delete the artefact the hashed tag addresses —
// and the deletion would look entirely correct in the plan, because the bare
// tag really does carry no identity.
func TestAVersionWearingBothTagsIsKept(t *testing.T) {
	vds := classify([]pkgVersion{
		v(1, digBoth, "1.3.2", "1.3.2-hmjgwjhkpeyruxsts"),
		v(2, "dddd000000000000000000000000000000000000000000000000000000000007", "sha256-"+digBoth),
	})
	if got := deleted(vds); len(got) != 0 {
		t.Errorf("deleted %v; a version that also wears an identified tag must be kept, "+
			"and so must the signature attached to it", got)
	}
}

// An attachment is matched to its subject by DIGEST. A `sha256-…` tag whose
// subject is not in the listing at all is not evidence of anything, and is
// left where it is.
func TestAnAttachmentWithNoSubjectHereIsLeftAlone(t *testing.T) {
	orphan := "9999000000000000000000000000000000000000000000000000000000009999"
	vds := classify([]pkgVersion{
		v(1, digBad, "1.3.2"),
		v(2, "dddd000000000000000000000000000000000000000000000000000000000008", "sha256-"+orphan),
	})
	got := deleted(vds)
	if _, ok := got[2]; ok {
		t.Error("deleted an attachment whose subject is not in this listing")
	}
	if _, ok := got[1]; !ok {
		t.Error("the unidentified version itself should still go")
	}
}

func TestTheLaneMatrixIsTheInventory(t *testing.T) {
	got, err := projects(filepath.Join("..", ".github", "workflows", "wasip1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// ⛔ THE DENOMINATOR IS ASSERTED. A parser that stops matching returns an
	// empty inventory, and an empty inventory sweeps nothing and passes.
	const want = 30
	if len(got) != want {
		t.Errorf("read %d project(s) from the wasip1 matrix, want %d — if the lane "+
			"changed, move this deliberately", len(got), want)
	}
	var age string
	for _, e := range got {
		if e.Slug == "filippo.io/age" {
			age = e.Version
		}
		if e.Version == "" {
			t.Errorf("%s has no version", e.Slug)
		}
	}
	if age != "1.3.2" {
		t.Errorf("filippo.io/age reads version %q, want 1.3.2", age)
	}
}

func TestItRefusesTheRegistryClientsRead(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"-org", liveRegistry}, &out)
	if err == nil {
		t.Fatal("ran against the live registry")
	}
	if !strings.Contains(err.Error(), liveRegistry) {
		t.Errorf("the refusal does not name the org: %v", err)
	}
}

// ⛔ A FILTER THAT MATCHES NOTHING IS INDISTINGUISHABLE FROM A CLEAN
// REGISTRY: both print no work to do. A mistyped slug must refuse, not pass.
func TestItRefusesASlugItDoesNotPublish(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{
		"-workflow", filepath.Join("..", ".github", "workflows", "wasip1.yml"),
		"-only", "filippo.io/aeg",
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "filippo.io/aeg") {
		t.Fatalf("want a refusal naming the unknown slug, got %v", err)
	}
}

func TestItRefusesAnInventoryOfTheWrongSize(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{
		"-workflow", filepath.Join("..", ".github", "workflows", "wasip1.yml"),
		"-expect", "29",
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "expected 29") {
		t.Fatalf("want a refusal naming the expected count, got %v", err)
	}
}
