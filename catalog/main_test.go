package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// swapStdout redirects os.Stdout to /dev/null for the duration of a main() call
// (which writes registry JSON there) and returns a restore func.
func swapStdout(t *testing.T) func() {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = devnull
	return func() {
		os.Stdout = saved
		devnull.Close()
	}
}

// mockDoer routes every request through fn, so a test can serve the ghcr
// pull-token endpoint and the OCI registry from memory.
type mockDoer struct {
	fn func(*http.Request) (*http.Response, error)
}

func (m mockDoer) Do(r *http.Request) (*http.Response, error) { return m.fn(r) }

// resp builds an *http.Response with a string body and optional headers.
func resp(status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     header,
	}
}

// route dispatches by matching substrings of the request URL, in order.
func route(pairs ...func(url string) (*http.Response, bool)) mockDoer {
	return mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		for _, p := range pairs {
			if resp, ok := p(u); ok {
				return resp, nil
			}
		}
		return resp(404, `{"message":"not found"}`, nil), nil
	}}
}

func when(match string, r *http.Response) func(string) (*http.Response, bool) {
	return func(u string) (*http.Response, bool) {
		if strings.Contains(u, match) {
			return r, true
		}
		return nil, false
	}
}

// fakeFiles returns a readFile seam backed by an in-memory map, erroring on any
// path not present.
func fakeFiles(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, fmt.Errorf("no such file %q", p)
	}
}

func TestRunHappyPath(t *testing.T) {
	// Candidates come from the repo files (not an org API). Comments, blank
	// lines and surrounding whitespace are stripped; the windows lists carry the
	// slug in the first '|' field; a blank slug field is ignored; foo repeats
	// across recipes + windows to exercise de-dup.
	files := map[string]string{
		"recipes.txt":               "foo\n# comment\n\n  bar  \n",
		"windows/go-projects.txt":   "foo|foo-repo|foo\nbaz|baz/repo|baz\n",
		"windows/rust-projects.txt": "# header\nqux|q/r|qux\n  |empty-slug\n",
	}

	// foo carries two versions (1.0 multi-arch, 1.1 single) so the sort
	// comparator's name / os / arch / version tiers all get exercised. The 1.0
	// index also carries unknown/empty attestation entries that must be dropped.
	fooManifest10 := `{"manifests":[
	  {"platform":{"os":"linux","architecture":"amd64"}},
	  {"platform":{"os":"linux","architecture":"arm64"}},
	  {"platform":{"os":"darwin","architecture":"arm64"}},
	  {"platform":{"os":"unknown","architecture":"unknown"}},
	  {"platform":{"os":"","architecture":"amd64"}}
	]}`
	fooManifest11 := `{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}`
	barManifest := `{"manifests":[{"platform":{"os":"windows","architecture":"amd64"}}]}`

	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		// Token endpoint: baz is denied (not published); the rest yield a token.
		case strings.Contains(u, "/token?") && strings.Contains(u, "packages/baz:pull"):
			return resp(403, `{"errors":[{"code":"DENIED"}]}`, nil), nil
		case strings.Contains(u, "/token?") && strings.Contains(u, "packages/foo:pull"):
			return resp(200, `{"token":"t-foo"}`, nil), nil
		case strings.Contains(u, "/token?") && strings.Contains(u, "packages/bar:pull"):
			return resp(200, `{"token":"t-bar"}`, nil), nil
		case strings.Contains(u, "/token?") && strings.Contains(u, "packages/qux:pull"):
			return resp(200, `{"token":"t-qux"}`, nil), nil
		// Tags: foo drops sha256-* + "latest" junk; qux 404s (published token but
		// empty repo) so it yields no rows.
		case strings.Contains(u, "/packages/foo/tags/list"):
			return resp(200, `{"name":"acme/packages/foo","tags":["1.1","1.0","sha256-abc","latest"]}`, nil), nil
		case strings.Contains(u, "/packages/bar/tags/list"):
			return resp(200, `{"name":"acme/packages/bar","tags":["2.0"]}`, nil), nil
		case strings.Contains(u, "/packages/qux/tags/list"):
			return resp(404, `{"errors":[{"code":"NAME_UNKNOWN"}]}`, nil), nil
		// Manifests.
		case strings.Contains(u, "/packages/foo/manifests/1.0"):
			return resp(200, fooManifest10, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/1.1"):
			return resp(200, fooManifest11, nil), nil
		case strings.Contains(u, "/packages/bar/manifests/2.0"):
			return resp(200, barManifest, nil), nil
		}
		return resp(404, "nope", nil), nil
	}}

	env := map[string]string{
		"GITHUB_REPOSITORY_OWNER": "acme",
		"GHCR_URL":                "https://ghcr.test",
		"CATALOG_CONCURRENCY":     "2",
	}
	var out, errb strings.Builder
	if err := run(&out, &errb, func(k string) string { return env[k] }, d, fakeFiles(files)); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	want := `[
 {
  "name": "bar",
  "os": "windows",
  "arch": "x86-64",
  "version": "2.0"
 },
 {
  "name": "foo",
  "os": "darwin",
  "arch": "aarch64",
  "version": "1.0"
 },
 {
  "name": "foo",
  "os": "linux",
  "arch": "aarch64",
  "version": "1.0"
 },
 {
  "name": "foo",
  "os": "linux",
  "arch": "x86-64",
  "version": "1.0"
 },
 {
  "name": "foo",
  "os": "linux",
  "arch": "x86-64",
  "version": "1.1"
 }
]
`
	if got != want {
		t.Fatalf("output mismatch:\n got: %s\nwant: %s", got, want)
	}
	if errb.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", errb.String())
	}
}

func TestRunCandidatesError(t *testing.T) {
	// recipes.txt unreadable -> candidateNames errors -> run errors.
	d := route()
	rf := fakeFiles(nil) // every path missing
	if err := run(io.Discard, io.Discard, func(string) string { return "" }, d, rf); err == nil {
		t.Fatal("expected error when recipes.txt is unreadable")
	}
}

func TestRunConcurrencyZeroDefaults(t *testing.T) {
	// CATALOG_CONCURRENCY=0 must fall back to the default worker count, and an
	// empty candidate set encodes as JSON null.
	files := map[string]string{
		"recipes.txt":               "",
		"windows/go-projects.txt":   "",
		"windows/rust-projects.txt": "",
	}
	env := map[string]string{"CATALOG_CONCURRENCY": "0"}
	d := route()
	var out strings.Builder
	if err := run(&out, io.Discard, func(k string) string { return env[k] }, d, fakeFiles(files)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out.String()) != "null" {
		t.Fatalf("want null, got %q", out.String())
	}
}

func TestCandidateNames(t *testing.T) {
	// Mixed case exercises the lower-casing (github.com/AOMediaCodec -> …/aomediacodec),
	// which also collapses A.org (recipes) with a.org into one candidate.
	files := map[string]string{
		"recipes.txt":               "b.org\n#c\n\n  A.org  \n",
		"windows/go-projects.txt":   "a.org|repo|a\ngithub.com/AOMediaCodec/libavif|r|x\n",
		"windows/rust-projects.txt": "# hdr\nM.rs|m/r|m\n |blank\n",
	}
	names, err := candidateNames(fakeFiles(files))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.org", "b.org", "github.com/aomediacodec/libavif", "m.rs"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("candidateNames = %v, want %v", names, want)
	}

	// recipes.txt read error propagates.
	if _, err := candidateNames(fakeFiles(map[string]string{})); err == nil {
		t.Fatal("expected error on missing recipes.txt")
	}
	// A windows list read error propagates too.
	only := fakeFiles(map[string]string{"recipes.txt": "x\n"})
	if _, err := candidateNames(only); err == nil {
		t.Fatal("expected error on missing windows list")
	}
}

func testClient(d doer) *client {
	return &client{
		doer:     d,
		ghcrBase: "https://ghcr.test",
		owner:    "acme",
		stderr:   io.Discard,
	}
}

func TestGhcrToken(t *testing.T) {
	// 200 -> token.
	c := testClient(route(when("/token?", resp(200, `{"token":"T"}`, nil))))
	tok, err := c.ghcrToken("foo")
	if err != nil || tok != "T" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}
	// 403 -> not published: empty token, no error.
	c = testClient(route(when("/token?", resp(403, `denied`, nil))))
	if tok, err := c.ghcrToken("foo"); err != nil || tok != "" {
		t.Fatalf("403: tok=%q err=%v, want empty,nil", tok, err)
	}
	// 404 -> also not published.
	c = testClient(route(when("/token?", resp(404, `missing`, nil))))
	if tok, err := c.ghcrToken("foo"); err != nil || tok != "" {
		t.Fatalf("404: tok=%q err=%v, want empty,nil", tok, err)
	}
	// 400 NAME_INVALID -> not published (an un-nameable slug), skipped silently.
	c = testClient(route(when("/token?", resp(400, `{"errors":[{"code":"NAME_INVALID"}]}`, nil))))
	if tok, err := c.ghcrToken("foo"); err != nil || tok != "" {
		t.Fatalf("400: tok=%q err=%v, want empty,nil", tok, err)
	}
	// Other non-2xx -> error.
	c = testClient(route(when("/token?", resp(500, "boom", nil))))
	if _, err := c.ghcrToken("foo"); err == nil {
		t.Fatal("expected 500 error")
	}
	// 200 with malformed body -> decode error.
	c = testClient(route(when("/token?", resp(200, `oops`, nil))))
	if _, err := c.ghcrToken("foo"); err == nil {
		t.Fatal("expected decode error")
	}
	// Transport error.
	c = testClient(mockDoer{fn: func(*http.Request) (*http.Response, error) {
		return nil, errors.New("net down")
	}})
	if _, err := c.ghcrToken("foo"); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestListTags(t *testing.T) {
	// 200: sha256-* + non-semver junk dropped, remaining sorted.
	body := `{"name":"acme/packages/x","tags":["1.1","1.0","sha256-x","latest","v2.0"]}`
	c := testClient(route(when("/tags/list", resp(200, body, nil))))
	tags, err := c.listTags("x", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tags, []string{"1.0", "1.1", "v2.0"}) {
		t.Fatalf("tags = %v, want [1.0 1.1 v2.0]", tags)
	}
	// 404 -> no tags, no error (not published / empty).
	c = testClient(route(when("/tags/list", resp(404, `{}`, nil))))
	if tags, err := c.listTags("x", "tok"); err != nil || tags != nil {
		t.Fatalf("404: tags=%v err=%v, want nil,nil", tags, err)
	}
	// Other non-2xx -> error.
	c = testClient(route(when("/tags/list", resp(500, "boom", nil))))
	if _, err := c.listTags("x", "tok"); err == nil {
		t.Fatal("expected 500 error")
	}
	// 200 malformed -> decode error.
	c = testClient(route(when("/tags/list", resp(200, `bad`, nil))))
	if _, err := c.listTags("x", "tok"); err == nil {
		t.Fatal("expected decode error")
	}
	// Transport error.
	c = testClient(mockDoer{fn: func(*http.Request) (*http.Response, error) {
		return nil, errors.New("net down")
	}})
	if _, err := c.listTags("x", "tok"); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestPlatforms(t *testing.T) {
	body := `{"manifests":[
	  {"platform":{"os":"linux","architecture":"amd64"}},
	  {"platform":{"os":"linux","architecture":"arm64"}},
	  {"platform":{"os":"unknown","architecture":"unknown"}},
	  {"platform":{"os":"linux","architecture":""}}
	]}`
	c := testClient(route(when("/manifests/1.0", resp(200, body, nil))))
	ps, err := c.platforms("foo", "1.0", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].arch != "amd64" || ps[1].arch != "arm64" {
		t.Fatalf("got %+v", ps)
	}
	// Non-2xx from get -> error.
	c = testClient(route(when("/manifests/1.0", resp(500, "bad", nil))))
	if _, err := c.platforms("foo", "1.0", "tok"); err == nil {
		t.Fatal("expected get error")
	}
	// 200 malformed -> decode error.
	c = testClient(route(when("/manifests/1.0", resp(200, `nope`, nil))))
	if _, err := c.platforms("foo", "1.0", "tok"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestPackageRows(t *testing.T) {
	// token error (non-2xx that is not 403/404) -> logged + skipped.
	c := testClient(route(when("/token?", resp(500, "x", nil))))
	if rows := c.packageRows("foo"); rows != nil {
		t.Fatalf("token error: want nil, got %v", rows)
	}
	// not published (403) -> empty bearer -> skipped silently.
	c = testClient(route(when("/token?", resp(403, "denied", nil))))
	if rows := c.packageRows("foo"); rows != nil {
		t.Fatalf("not published: want nil, got %v", rows)
	}
	// tags error -> skipped.
	c = testClient(route(
		when("/token?", resp(200, `{"token":"t"}`, nil)),
		when("/tags/list", resp(500, "x", nil)),
	))
	if rows := c.packageRows("foo"); rows != nil {
		t.Fatalf("tags error: want nil, got %v", rows)
	}
	// one tag's manifest errors -> that tag skipped, the other yields a row.
	c = testClient(route(
		when("/token?", resp(200, `{"token":"t"}`, nil)),
		when("/tags/list", resp(200, `{"tags":["1.0","2.0"]}`, nil)),
		when("/manifests/2.0", resp(200, `{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}`, nil)),
		when("/manifests/1.0", resp(500, "bad", nil)),
	))
	rows := c.packageRows("foo")
	if len(rows) != 1 || rows[0].Version != "2.0" || rows[0].OS != "linux" || rows[0].Arch != "x86-64" {
		t.Fatalf("got %+v", rows)
	}
}

func TestGetAndRawGetErrors(t *testing.T) {
	// NewRequest failure (control char in URL).
	c := testClient(nil)
	if _, _, err := c.rawGet("http://\x7f/x", "", ""); err == nil {
		t.Fatal("expected request build error")
	}
	// get surfaces the same build error.
	if _, err := c.get("http://\x7f/x", "", ""); err == nil {
		t.Fatal("expected get build error")
	}
	// doer transport error.
	c = testClient(mockDoer{fn: func(*http.Request) (*http.Response, error) {
		return nil, errors.New("net down")
	}})
	if _, err := c.get("http://x/y", "a", "tok"); err == nil {
		t.Fatal("expected transport error")
	}
	// non-2xx status.
	c = testClient(route(when("/y", resp(404, "missing", nil))))
	if _, err := c.get("http://x/y", "", ""); err == nil {
		t.Fatal("expected status error")
	}
}

func TestSemverTag(t *testing.T) {
	for tag, want := range map[string]bool{
		"1.9.4":       true,
		"14.1.0":      true,
		"0.128.0":     true,
		"v1.2.3":      true,
		"sha256-abcd": false,
		"latest":      false,
		"":            false,
		"v":           false,
		"vabc":        false,
		"edge":        false,
	} {
		if got := semverTag(tag); got != want {
			t.Fatalf("semverTag(%q) = %v, want %v", tag, got, want)
		}
	}
}

func TestMapArchAndEnvOr(t *testing.T) {
	for in, want := range map[string]string{"amd64": "x86-64", "arm64": "aarch64", "riscv64": "riscv64"} {
		if got := mapArch(in); got != want {
			t.Fatalf("mapArch(%q)=%q want %q", in, got, want)
		}
	}
	if got := envOr(func(string) string { return "" }, "K", "def"); got != "def" {
		t.Fatalf("envOr default=%q", got)
	}
	if got := envOr(func(string) string { return "v" }, "K", "def"); got != "v" {
		t.Fatalf("envOr set=%q", got)
	}
}

func TestMainSuccessAndFailure(t *testing.T) {
	// Success: main() runs to completion against a live test server that denies
	// every token (so the catalog is empty) — with recipes/windows files present
	// in the working directory it reads via os.ReadFile.
	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"code":"DENIED"}]}`, http.StatusForbidden)
	}))
	defer deny.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "recipes.txt"), []byte("x.org\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "windows"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"go-projects.txt", "rust-projects.txt"} {
		if err := os.WriteFile(filepath.Join(dir, "windows", f), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	restoreOut := swapStdout(t)
	t.Setenv("GHCR_URL", deny.URL)
	t.Setenv("GITHUB_REPOSITORY_OWNER", "acme")
	t.Setenv("CATALOG_CONCURRENCY", "")
	exited := -1
	osExit = func(code int) { exited = code }
	defer func() { osExit = os.Exit }()
	main()
	restoreOut()
	if exited != -1 {
		t.Fatalf("unexpected exit %d", exited)
	}

	// Failure: an empty working directory has no recipes.txt, so candidateNames
	// errors, run() errors, and main() calls osExit(1).
	t.Chdir(t.TempDir())
	main()
	if exited != 1 {
		t.Fatalf("want exit 1, got %d", exited)
	}
}

// TestRunAuditMode: AUDIT=1 reports platform gaps instead of the catalogue, and
// AUDIT_ALL adds the full disagreement listing. Same enumeration, two questions
// — a second crawler would be a second thing to keep honest.
func TestRunAuditMode(t *testing.T) {
	files := map[string]string{
		"recipes.txt":               "foo\n",
		"windows/go-projects.txt":   "",
		"windows/rust-projects.txt": "",
	}
	// foo 1.0 and 1.2 carry both arches; 1.1 lost one — a hole, not growth.
	both := `{"manifests":[
	 {"platform":{"os":"linux","architecture":"amd64"},"digest":"sha256:a"},
	 {"platform":{"os":"linux","architecture":"arm64"},"digest":"sha256:b"}]}`
	one := `{"manifests":[
	 {"platform":{"os":"linux","architecture":"amd64"},"digest":"sha256:c"}]}`

	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		case strings.Contains(u, "/token?"):
			return resp(200, `{"token":"t"}`, nil), nil
		case strings.Contains(u, "/packages/foo/tags/list"):
			return resp(200, `{"name":"acme/packages/foo","tags":["1.0","1.1","1.2"]}`, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/1.1"):
			return resp(200, one, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/"):
			return resp(200, both, nil), nil
		// The upstream dist carries every foo version for both arches, so a gap
		// in OUR index is a LOST entry rather than a version that never existed.
		case strings.Contains(u, "dist.pkgx.dev/foo/linux/"):
			return resp(200, "1.0\n1.1\n1.2\n", nil), nil
		}
		return resp(404, "nope", nil), nil
	}}
	env := map[string]string{
		"GITHUB_REPOSITORY_OWNER": "acme",
		"GHCR_URL":                "https://ghcr.test",
		"AUDIT":                   "1",
	}
	getenv := func(k string) string { return env[k] }

	var out, errb strings.Builder
	if err := run(&out, &errb, getenv, d, fakeFiles(files)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "foo 1.1") || !strings.Contains(out.String(), "linux/aarch64") {
		t.Errorf("the gap is not reported:\n%s", out.String())
	}
	if strings.Contains(out.String(), "different platform sets") {
		t.Errorf("AUDIT alone must not print the full listing:\n%s", out.String())
	}
	// Upstream carries foo 1.1 for linux/aarch64, so this gap is a LOST index
	// entry — the half a re-dispatch can put back.
	if !strings.Contains(errb.String(), "1 lost index entr") {
		t.Errorf("not classified as lost: %q", errb.String())
	}
	if !strings.Contains(out.String(), "--- LOST") || !strings.Contains(out.String(), "--- ABSENT") {
		t.Errorf("the two buckets are not both shown:\n%s", out.String())
	}

	env["AUDIT_ALL"] = "1"
	var out2, errb2 strings.Builder
	if err := run(&out2, &errb2, getenv, d, fakeFiles(files)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out2.String(), "different platform sets") {
		t.Errorf("AUDIT_ALL must add the full listing:\n%s", out2.String())
	}
	if !strings.Contains(errb2.String(), "disagree on platforms") {
		t.Errorf("no disagreement summary: %q", errb2.String())
	}
}

// TestRunAuditReportsAnAbsence: the other bucket. A version upstream never
// published for that platform cannot be healed, and saying so is the point —
// two mirrors and a glibc rebuild were spent on gnu.org/glibc 2.28.0
// linux/x86-64 before its version lists were consulted.
func TestRunAuditReportsAnAbsence(t *testing.T) {
	files := map[string]string{
		"recipes.txt":               "foo\n",
		"windows/go-projects.txt":   "",
		"windows/rust-projects.txt": "",
	}
	both := `{"manifests":[
	 {"platform":{"os":"linux","architecture":"amd64"},"digest":"sha256:a"},
	 {"platform":{"os":"linux","architecture":"arm64"},"digest":"sha256:b"}]}`
	one := `{"manifests":[
	 {"platform":{"os":"linux","architecture":"amd64"},"digest":"sha256:c"}]}`

	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		case strings.Contains(u, "/token?"):
			return resp(200, `{"token":"t"}`, nil), nil
		case strings.Contains(u, "/packages/foo/tags/list"):
			return resp(200, `{"name":"acme/packages/foo","tags":["1.0","1.1","1.2"]}`, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/1.1"):
			return resp(200, one, nil), nil
		case strings.Contains(u, "/packages/foo/manifests/"):
			return resp(200, both, nil), nil
		// Upstream never had foo 1.1 for aarch64 either.
		case strings.Contains(u, "dist.pkgx.dev/foo/linux/aarch64"):
			return resp(200, "1.0\n1.2\n", nil), nil
		}
		return resp(404, "nope", nil), nil
	}}
	env := map[string]string{
		"GITHUB_REPOSITORY_OWNER": "acme",
		"GHCR_URL":                "https://ghcr.test",
		"AUDIT":                   "1",
	}
	var out, errb strings.Builder

	if err := run(&out, &errb, func(k string) string { return env[k] }, d, fakeFiles(files)); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.Contains(out.String(), "no linux/aarch64 anywhere") {
		t.Errorf("the absence is not stated:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "0 lost index entr(ies), 1 genuine absence(s)") {
		t.Errorf("wrong classification: %q", errb.String())
	}
}
