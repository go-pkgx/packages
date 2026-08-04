package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// mockDoer routes every request through fn, so a test can serve the GitHub
// Packages API, the ghcr pull-token endpoint and the OCI registry from memory.
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

// errReader is a ReadCloser whose Read always fails, to exercise body-read errors.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }
func (errReader) Close() error             { return nil }

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

func TestRunHappyPath(t *testing.T) {
	page1 := `[{"name":"packages/foo"},{"name":"other/thing"}]`
	page2 := `[{"name":"packages/bar"}]`
	nextHdr := http.Header{"Link": {`<https://api.test/orgs/acme/packages?package_type=container&per_page=100&page=2>; rel="next"`}}

	// foo carries two versions (1.0 multi-arch, 1.1 single) so the sort
	// comparator's name / os / arch / version tiers all get exercised.
	fooVersions := `[
	  {"created_at":"2026-08-03T00:00:00Z","metadata":{"container":{"tags":["1.1"]}}},
	  {"created_at":"2026-08-01T10:00:00Z","metadata":{"container":{"tags":["1.0","sha256-abc"]}}},
	  {"created_at":"2026-08-01T09:00:00Z","metadata":{"container":{"tags":["sha256-only"]}}}
	]`
	barVersions := `[{"created_at":"2026-07-15T00:00:00Z","metadata":{"container":{"tags":["2.0"]}}}]`

	fooManifest10 := `{"manifests":[
	  {"platform":{"os":"linux","architecture":"amd64"}},
	  {"platform":{"os":"linux","architecture":"arm64"}},
	  {"platform":{"os":"darwin","architecture":"arm64"}},
	  {"platform":{"os":"unknown","architecture":"unknown"}},
	  {"platform":{"os":"","architecture":"amd64"}}
	]}`
	fooManifest11 := `{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}`
	barManifest := `{"manifests":[{"platform":{"os":"windows","architecture":"amd64"}}]}`

	tokenJSON := `{"token":"ghcr-tok"}`

	d := mockDoer{fn: func(r *http.Request) (*http.Response, error) {
		u := r.URL.String()
		switch {
		case strings.Contains(u, "/packages?package_type=container") && strings.Contains(u, "page=2"):
			return resp(200, page2, nil), nil
		case strings.Contains(u, "/packages?package_type=container"):
			return resp(200, page1, nextHdr), nil
		case strings.Contains(u, "packages%2Ffoo/versions"):
			return resp(200, fooVersions, nil), nil
		case strings.Contains(u, "packages%2Fbar/versions"):
			return resp(200, barVersions, nil), nil
		case strings.Contains(u, "/token?"):
			return resp(200, tokenJSON, nil), nil
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
		"GITHUB_API_URL":          "https://api.test",
		"GHCR_URL":                "https://ghcr.test",
		"GITHUB_TOKEN":            "gh-tok",
		"CATALOG_CONCURRENCY":     "2",
	}
	var out, errb strings.Builder
	if err := run(&out, &errb, func(k string) string { return env[k] }, d); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	want := `[
 {
  "name": "bar",
  "os": "windows",
  "arch": "x86-64",
  "version": "2.0",
  "published": "2026-07-15"
 },
 {
  "name": "foo",
  "os": "darwin",
  "arch": "aarch64",
  "version": "1.0",
  "published": "2026-08-01"
 },
 {
  "name": "foo",
  "os": "linux",
  "arch": "aarch64",
  "version": "1.0",
  "published": "2026-08-01"
 },
 {
  "name": "foo",
  "os": "linux",
  "arch": "x86-64",
  "version": "1.0",
  "published": "2026-08-01"
 },
 {
  "name": "foo",
  "os": "linux",
  "arch": "x86-64",
  "version": "1.1",
  "published": "2026-08-03"
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

func TestRunListPackagesError(t *testing.T) {
	d := route(when("/packages?package_type=container", resp(500, "boom", nil)))
	if err := run(io.Discard, io.Discard, func(string) string { return "" }, d); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunConcurrencyZeroDefaults(t *testing.T) {
	// CATALOG_CONCURRENCY=0 must fall back to the default worker count.
	env := map[string]string{"CATALOG_CONCURRENCY": "0"}
	d := route(when("/packages?package_type=container", resp(200, `[]`, nil)))
	var out strings.Builder
	if err := run(&out, io.Discard, func(k string) string { return env[k] }, d); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out.String()) != "null" {
		t.Fatalf("want null, got %q", out.String())
	}
}

func testClient(d doer) *client {
	return &client{
		doer:     d,
		apiBase:  "https://api.test",
		ghcrBase: "https://ghcr.test",
		owner:    "acme",
		stderr:   io.Discard,
	}
}

func TestListPackageNamesFilterAndBadJSON(t *testing.T) {
	c := testClient(route(when("/packages?", resp(200, `[{"name":"packages/a"},{"name":"nope/b"}]`, nil))))
	names, err := c.listPackageNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "a" {
		t.Fatalf("got %v", names)
	}

	c = testClient(route(when("/packages?", resp(200, `not json`, nil))))
	if _, err := c.listPackageNames(); err == nil {
		t.Fatal("expected json error")
	}
}

func TestListVersions(t *testing.T) {
	body := `[
	  {"created_at":"2026-01-02T03:04:05Z","metadata":{"container":{"tags":["1.0","sha256-x"]}}},
	  {"created_at":"2026-01-03T00:00:00Z","metadata":{"container":{"tags":["sha256-y"]}}}
	]`
	c := testClient(route(when("packages%2Fx.org%2Ftool/versions", resp(200, body, nil))))
	vs, err := c.listVersions("x.org/tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].published != "2026-01-02" || len(vs[0].tags) != 1 || vs[0].tags[0] != "1.0" {
		t.Fatalf("got %+v", vs)
	}

	c = testClient(route(when("/versions", resp(200, `bad`, nil))))
	if _, err := c.listVersions("x"); err == nil {
		t.Fatal("expected json error")
	}
}

func TestGhcrToken(t *testing.T) {
	c := testClient(route(when("/token?", resp(200, `{"token":"T"}`, nil))))
	tok, err := c.ghcrToken("foo")
	if err != nil || tok != "T" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}

	c = testClient(route(when("/token?", resp(200, `oops`, nil))))
	if _, err := c.ghcrToken("foo"); err == nil {
		t.Fatal("expected decode error")
	}

	c = testClient(route(when("/token?", resp(403, "denied", nil))))
	if _, err := c.ghcrToken("foo"); err == nil {
		t.Fatal("expected status error")
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

	c = testClient(route(when("/manifests/1.0", resp(200, `nope`, nil))))
	if _, err := c.platforms("foo", "1.0", "tok"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestPackageRowsSkips(t *testing.T) {
	// versions error -> skipped, no rows, no panic.
	c := testClient(route(when("/versions", resp(500, "x", nil))))
	if rows := c.packageRows("foo"); rows != nil {
		t.Fatalf("want nil, got %v", rows)
	}

	// token error -> skipped.
	c = testClient(route(
		when("/versions", resp(200, `[{"created_at":"2026-01-01T00:00:00Z","metadata":{"container":{"tags":["1.0"]}}}]`, nil)),
		when("/token?", resp(500, "x", nil)),
	))
	if rows := c.packageRows("foo"); rows != nil {
		t.Fatalf("want nil, got %v", rows)
	}

	// one tag's manifest errors -> that tag skipped, the other yields a row.
	versions := `[
	  {"created_at":"2026-01-01T00:00:00Z","metadata":{"container":{"tags":["1.0"]}}},
	  {"created_at":"2026-02-02T00:00:00Z","metadata":{"container":{"tags":["2.0"]}}}
	]`
	c = testClient(route(
		when("/versions", resp(200, versions, nil)),
		when("/token?", resp(200, `{"token":"t"}`, nil)),
		when("/manifests/2.0", resp(200, `{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}`, nil)),
		when("/manifests/1.0", resp(500, "bad", nil)),
	))
	rows := c.packageRows("foo")
	if len(rows) != 1 || rows[0].Version != "2.0" || rows[0].OS != "linux" || rows[0].Arch != "x86-64" {
		t.Fatalf("got %+v", rows)
	}
}

func TestGetErrors(t *testing.T) {
	// NewRequest failure (control char in URL).
	c := testClient(nil)
	if _, err := c.get("http://\x7f/x", "", ""); err == nil {
		t.Fatal("expected request build error")
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

func TestPaginateBodyReadError(t *testing.T) {
	c := testClient(mockDoer{fn: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "OK", Body: errReader{}, Header: http.Header{}}, nil
	}})
	if err := c.paginate("http://x/p", func([]byte) error { return nil }); err == nil {
		t.Fatal("expected read error")
	}
}

func TestNextLink(t *testing.T) {
	h := `<https://a/p?page=2>; rel="next", <https://a/p?page=9>; rel="last"`
	if got := nextLink(h); got != "https://a/p?page=2" {
		t.Fatalf("got %q", got)
	}
	if got := nextLink(`<https://a/p?page=9>; rel="last"`); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := nextLink(""); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := nextLink("garbage"); got != "" { // no semicolon segment
		t.Fatalf("got %q", got)
	}
	if got := nextLink(`bare; rel="next"`); got != "" { // url not <...>
		t.Fatalf("got %q", got)
	}
}

func TestMapArchAndHelpers(t *testing.T) {
	for in, want := range map[string]string{"amd64": "x86-64", "arm64": "aarch64", "riscv64": "riscv64"} {
		if got := mapArch(in); got != want {
			t.Fatalf("mapArch(%q)=%q want %q", in, got, want)
		}
	}
	if got := urlEncode("packages/x.org/a-b_c~1"); got != "packages%2Fx.org%2Fa-b_c~1" {
		t.Fatalf("urlEncode=%q", got)
	}
	if got := dateOnly("2026-08-04T12:00:00Z"); got != "2026-08-04" {
		t.Fatalf("dateOnly=%q", got)
	}
	if got := dateOnly("2026-08-04"); got != "2026-08-04" {
		t.Fatalf("dateOnly no-T=%q", got)
	}
	if got := envOr(func(string) string { return "" }, "K", "def"); got != "def" {
		t.Fatalf("envOr default=%q", got)
	}
	if got := envOr(func(string) string { return "v" }, "K", "def"); got != "v" {
		t.Fatalf("envOr set=%q", got)
	}
}

func TestMainSuccessAndFailure(t *testing.T) {
	// Success: main() runs to completion (no exit) against a live test server.
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer ok.Close()

	restoreOut := swapStdout(t)
	t.Setenv("GITHUB_API_URL", ok.URL)
	t.Setenv("GITHUB_REPOSITORY_OWNER", "acme")
	t.Setenv("GITHUB_TOKEN", "t")
	t.Setenv("CATALOG_CONCURRENCY", "")
	exited := -1
	osExit = func(code int) { exited = code }
	defer func() { osExit = os.Exit }()
	main()
	restoreOut()
	if exited != -1 {
		t.Fatalf("unexpected exit %d", exited)
	}

	// Failure: a 500 makes run() error, so main() calls osExit(1).
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer bad.Close()
	t.Setenv("GITHUB_API_URL", bad.URL)
	main()
	if exited != 1 {
		t.Fatalf("want exit 1, got %d", exited)
	}
}
