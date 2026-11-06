package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeAPI stands in for GitHub's packages API. It answers the two calls this
// tool makes and records the deletions, so the whole flow can be exercised
// without a registry and without a credential that could reach one.
type fakeAPI struct {
	versions map[string][]pkgVersion // package name → its versions
	deleted  []string                // "<pkg> <id>", in order
	pages    int                     // if >0, always return a full page, this many times
	served   int
	status   int    // if non-zero, answer every request with it
	body     string // if non-empty, the raw body to answer with
}

func (f *fakeAPI) start(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })
}

// pkgOf reads the package name back out of the escaped path.
func pkgOf(escaped string) string {
	const mark = "/container/"
	i := strings.Index(escaped, mark)
	rest := escaped[i+len(mark):]
	j := strings.Index(rest, "/versions")
	return strings.ReplaceAll(rest[:j], "%2F", "/")
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	if f.status != 0 {
		w.WriteHeader(f.status)
		fmt.Fprint(w, f.body)
		return
	}
	pkg := pkgOf(r.URL.EscapedPath())
	if r.Method == http.MethodDelete {
		id := r.URL.EscapedPath()[strings.LastIndex(r.URL.EscapedPath(), "/")+1:]
		f.deleted = append(f.deleted, pkg+" "+id)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if f.pages > 0 {
		// A full page every time, so the client keeps asking and runs into
		// its own ceiling.
		full := make([]pkgVersion, 100)
		for i := range full {
			full[i] = v(int64(i+1), fmt.Sprintf("%064d", i+1))
		}
		f.served++
		_ = json.NewEncoder(w).Encode(full)
		return
	}
	vs, ok := f.versions[pkg]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
		return
	}
	if page := r.URL.Query().Get("page"); page != "1" {
		_ = json.NewEncoder(w).Encode([]pkgVersion{})
		return
	}
	_ = json.NewEncoder(w).Encode(vs)
}

// tokenFile writes a credential the tool may read. It is a fixture, not a
// secret: it never leaves the temporary directory and reaches only the
// httptest server above.
func tokenFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// laneFile writes a workflow with the matrix entries given.
func laneFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lane.yml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func lane(entries ...string) string {
	var b strings.Builder
	b.WriteString("jobs:\n  build:\n    strategy:\n      matrix:\n        include:\n")
	for _, e := range entries {
		b.WriteString("          " + e + "\n")
	}
	return b.String()
}

const oneProject = `- { slug: example.org/tool, version: "1.0.0" }`

func TestItDeletesWhatItPlannedAndSaysSo(t *testing.T) {
	const pkg = "packages/example.org/tool"
	bad := "aaaa000000000000000000000000000000000000000000000000000000000001"
	f := &fakeAPI{versions: map[string][]pkgVersion{pkg: {
		v(1, bad, "1.0.0"),
		v(2, "bbbb000000000000000000000000000000000000000000000000000000000002", "1.0.0-hmjgwjhkpeyruxsts"),
		v(3, "cccc000000000000000000000000000000000000000000000000000000000003", "sha256-"+bad),
		v(4, "dddd000000000000000000000000000000000000000000000000000000000004"),
	}}}
	f.start(t)

	var out bytes.Buffer
	err := run([]string{
		"-workflow", laneFile(t, lane(oneProject)),
		"-token", tokenFile(t, "fixture-credential\n"),
		"-expect", "1", "-yes",
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 2 {
		t.Fatalf("deleted %v, want the unidentified version and its attachment", f.deleted)
	}
	for _, want := range []string{pkg + " 1", pkg + " 3"} {
		if !containsString(f.deleted, want) {
			t.Errorf("%q was not deleted; got %v", want, f.deleted)
		}
	}
	got := out.String()
	for _, want := range []string{
		"1 of 1 project(s)", "4 version(s) read", "1 identified",
		"1 untagged (left alone)", "2 unidentified", "deleted 2 version(s)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not say %q:\n%s", want, got)
		}
	}
}

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func TestWithoutYesNothingIsDeleted(t *testing.T) {
	const pkg = "packages/example.org/tool"
	f := &fakeAPI{versions: map[string][]pkgVersion{pkg: {
		v(1, "aaaa000000000000000000000000000000000000000000000000000000000001", "1.0.0"),
	}}}
	f.start(t)

	var out bytes.Buffer
	if err := run([]string{
		"-workflow", laneFile(t, lane(oneProject)),
		"-token", tokenFile(t, "fixture-credential"),
	}, &out); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 0 {
		t.Errorf("a dry run deleted %v", f.deleted)
	}
	if !strings.Contains(out.String(), "nothing done") {
		t.Errorf("a dry run did not say so:\n%s", out.String())
	}
}

// A package the lane names but the registry does not hold is reported, not
// an error: the lane publishes to several registries over its life.
func TestAPackageTheRegistryDoesNotHoldIsReported(t *testing.T) {
	f := &fakeAPI{versions: map[string][]pkgVersion{}}
	f.start(t)

	var out bytes.Buffer
	if err := run([]string{
		"-workflow", laneFile(t, lane(oneProject)),
		"-token", tokenFile(t, "fixture-credential"),
	}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not published in go-zpkg") {
		t.Errorf("the absence was not reported:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "1 not published here") {
		t.Errorf("the denominator does not count it:\n%s", out.String())
	}
}

func TestACleanPackageSaysClean(t *testing.T) {
	const pkg = "packages/example.org/tool"
	f := &fakeAPI{versions: map[string][]pkgVersion{pkg: {
		v(1, "bbbb000000000000000000000000000000000000000000000000000000000002", "1.0.0-hmjgwjhkpeyruxsts"),
	}}}
	f.start(t)

	var out bytes.Buffer
	if err := run([]string{
		"-workflow", laneFile(t, lane(oneProject)),
		"-token", tokenFile(t, "fixture-credential"),
	}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "clean (1 version(s))") {
		t.Errorf("a clean package was not reported clean:\n%s", out.String())
	}
}

func TestAFailingListingStopsTheRun(t *testing.T) {
	f := &fakeAPI{status: http.StatusInternalServerError, body: "boom"}
	f.start(t)

	var out bytes.Buffer
	err := run([]string{
		"-workflow", laneFile(t, lane(oneProject)),
		"-token", tokenFile(t, "fixture-credential"),
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "packages/example.org/tool") {
		t.Fatalf("want a failure naming the package, got %v", err)
	}
}

func TestAFailingDeleteStopsTheRun(t *testing.T) {
	const pkg = "packages/example.org/tool"
	f := &fakeAPI{versions: map[string][]pkgVersion{pkg: {
		v(1, "aaaa000000000000000000000000000000000000000000000000000000000001", "1.0.0"),
	}}}
	f.start(t)
	// The listing succeeds, then the registry refuses the deletion.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"denied"}`)
			return
		}
		f.serve(w, r)
	}))
	t.Cleanup(srv.Close)
	apiBase = srv.URL

	var out bytes.Buffer
	err := run([]string{
		"-workflow", laneFile(t, lane(oneProject)),
		"-token", tokenFile(t, "fixture-credential"), "-yes",
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "version 1") {
		t.Fatalf("want a failure naming the version, got %v", err)
	}
}

func TestTheListingIsPagedAndBounded(t *testing.T) {
	f := &fakeAPI{pages: 1}
	f.start(t)
	c := client{token: "fixture-credential", http: http.DefaultClient}
	_, err := c.versions("go-zpkg", "packages/example.org/tool")
	if err == nil || !strings.Contains(err.Error(), "refusing to guess") {
		t.Fatalf("want a refusal rather than an unbounded walk, got %v", err)
	}
	if f.served != 20 {
		t.Errorf("asked for %d page(s), want the stated ceiling of 20", f.served)
	}
}

func TestAListingThatIsNotJSONIsAnError(t *testing.T) {
	f := &fakeAPI{status: http.StatusOK, body: "not json"}
	f.start(t)
	c := client{token: "fixture-credential", http: http.DefaultClient}
	if _, err := c.versions("go-zpkg", "packages/example.org/tool"); err == nil {
		t.Fatal("accepted a body that is not a listing")
	}
}

func TestAnUnreachableRegistryIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now
	old := apiBase
	apiBase = url
	t.Cleanup(func() { apiBase = old })

	c := client{token: "fixture-credential", http: http.DefaultClient}
	if _, err := c.versions("go-zpkg", "packages/example.org/tool"); err == nil {
		t.Fatal("reported success against a registry that is not there")
	}
}

func TestAMalformedRequestIsAnError(t *testing.T) {
	c := client{token: "fixture-credential", http: http.DefaultClient}
	if err := c.do("BAD METHOD", "http://example.invalid/", nil); err == nil {
		t.Fatal("built a request from a method that is not one")
	}
}

func TestTheCredentialIsReadFromAFile(t *testing.T) {
	p := tokenFile(t, "  fixture-credential\n")
	got, err := tokenFrom(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != "fixture-credential" {
		t.Errorf("read %q, want the trimmed contents", got)
	}

	if _, err := tokenFrom(tokenFile(t, "   \n")); err == nil {
		t.Error("accepted an empty credential file")
	}
	if _, err := tokenFrom(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("accepted a credential file that is not there")
	}

	// With no -token, it falls back to the home directory.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".github-token"), []byte("from-home"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = tokenFrom("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-home" {
		t.Errorf("read %q from the default path, want from-home", got)
	}
}

func TestTheInventoryRefusesWhatItCannotRead(t *testing.T) {
	if _, err := projects(filepath.Join(t.TempDir(), "absent.yml")); err == nil {
		t.Error("accepted a workflow that is not there")
	}
	if _, err := projects(laneFile(t, "jobs: [this is not a map]\n")); err == nil {
		t.Error("accepted something that is not a workflow")
	}
	if _, err := projects(laneFile(t, lane(`- { version: "1.0.0" }`))); err == nil {
		t.Error("accepted a matrix in which nothing has a slug")
	}
}

func TestOnlyActsOnTheProjectNamed(t *testing.T) {
	const pkg = "packages/example.org/tool"
	f := &fakeAPI{versions: map[string][]pkgVersion{pkg: {
		v(1, "aaaa000000000000000000000000000000000000000000000000000000000001", "1.0.0"),
	}}}
	f.start(t)

	var out bytes.Buffer
	if err := run([]string{
		"-workflow", laneFile(t, lane(oneProject, `- { slug: example.org/other, version: "2.0.0" }`)),
		"-token", tokenFile(t, "fixture-credential"),
		"-only", "example.org/tool",
	}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 of 2 project(s)") {
		t.Errorf("the report does not say how much of the inventory it looked at:\n%s", out.String())
	}
	if strings.Contains(out.String(), "example.org/other") {
		t.Errorf("-only still touched the other project:\n%s", out.String())
	}
}

func TestBadFlagsAreRefused(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-nosuchflag"}, &out); err == nil {
		t.Fatal("accepted a flag that does not exist")
	}
	out.Reset()
	if err := run([]string{"-h"}, &out); err == nil {
		t.Fatal("-h should report flag.ErrHelp")
	}
	if !strings.Contains(out.String(), "carry no build identity") {
		t.Errorf("the usage does not say what the tool does:\n%s", out.String())
	}
}

// main() is the one path the other tests cannot reach, and it is where the
// exit status is decided.
func TestMainReportsFailureThroughTheExitStatus(t *testing.T) {
	oldArgs, oldExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = oldArgs, oldExit })

	code := -1
	osExit = func(c int) { code = c }
	os.Args = []string{"staletags", "-org", liveRegistry}
	main()
	if code != 1 {
		t.Errorf("exit status %d, want 1", code)
	}

	code = -1
	os.Args = []string{"staletags", "-workflow", laneFile(t, lane(oneProject)), "-expect", "99"}
	main()
	if code != 1 {
		t.Errorf("exit status %d for a wrong denominator, want 1", code)
	}
}

// A response that stops short of the length it declared must be an error,
// not a short listing: a truncated page read as a complete one reports fewer
// versions than the registry holds, and this tool deletes on that reading.
func TestATruncatedResponseIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Length: 4096\r\n\r\n[]")
		conn.Close()
	}))
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })

	c := client{token: "fixture-credential", http: http.DefaultClient}
	if _, err := c.versions("go-zpkg", "packages/example.org/tool"); err == nil {
		t.Fatal("accepted a body shorter than the length it declared")
	}
}

func TestWithNoHomeTheDefaultCredentialPathIsAnError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := tokenFrom(""); err == nil {
		t.Error("found a default credential path with no home directory")
	}
}

func TestRunPassesOnItsInputFailures(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-workflow", filepath.Join(t.TempDir(), "absent.yml")}, &out); err == nil {
		t.Error("ran without an inventory")
	}
	out.Reset()
	if err := run([]string{
		"-workflow", laneFile(t, lane(oneProject)),
		"-token", filepath.Join(t.TempDir(), "absent"),
	}, &out); err == nil {
		t.Error("ran without a credential")
	}
}

func TestShortLeavesAShortDigestAlone(t *testing.T) {
	if got := short("abc"); got != "abc" {
		t.Errorf("short(%q) = %q", "abc", got)
	}
}

func TestKnownFindsNothingInAnEmptyInventory(t *testing.T) {
	if known(nil, "anything") {
		t.Error("found a project in an empty inventory")
	}
	if !known([]entry{{Slug: "a"}, {Slug: "b"}}, "b") {
		t.Error("did not find a project that is there")
	}
}

// The digest in the version listing is `sha256:<hex>`; a version whose name
// is spelt any other way still has to be comparable.
func TestDigestHexStripsThePrefixAndNothingElse(t *testing.T) {
	var p pkgVersion
	p.Name = "sha256:" + strconv.Itoa(1)
	if got := p.digestHex(); got != "1" {
		t.Errorf("digestHex() = %q", got)
	}
	p.Name = "plain"
	if got := p.digestHex(); got != "plain" {
		t.Errorf("digestHex() = %q", got)
	}
}
