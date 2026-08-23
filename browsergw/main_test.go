package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// upstreamLike stands in for zot: CORS on the manifest route, none on blobs,
// and a 405 for the blob preflight — the exact shape measured on v2.1.14.
func upstreamLike(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/blobs/"):
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Docker-Content-Digest", "sha256:deadbeef")
			io.WriteString(w, "layer-bytes")
		default:
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,content-type")
			io.WriteString(w, "{}")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBlobGetsTheHeadersItLacked is the whole reason this exists: without them
// a page can list versions and resolve a manifest, and cannot download the
// layer — which is every install.
func TestBlobGetsTheHeadersItLacked(t *testing.T) {
	up := upstreamLike(t)
	h, err := handler(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v2/p/blobs/sha256:deadbeef", nil)
	req.Header.Set("Origin", "http://page.test")

	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("ACAO = %q", got)
	}
	// A client that cannot READ the digest cannot check what it downloaded.
	if got := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, "Docker-Content-Digest") {
		t.Errorf("expose-headers = %q", got)
	}
	if rec.Body.String() != "layer-bytes" {
		t.Errorf("body was altered: %q", rec.Body.String())
	}
	if rec.Header().Get("Docker-Content-Digest") != "sha256:deadbeef" {
		t.Error("the registry's own digest header was lost")
	}
}

// TestHeadersAreReplacedNotDuplicated: the routes that already worked must keep
// working. Two Access-Control-Allow-Origin headers make a browser reject the
// response outright, so "add ours" would break exactly those paths.
func TestHeadersAreReplacedNotDuplicated(t *testing.T) {
	up := upstreamLike(t)
	h, _ := handler(up.URL)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v2/p/manifests/1.0.0", nil))

	if n := len(rec.Header().Values("Access-Control-Allow-Origin")); n != 1 {
		t.Fatalf("%d Access-Control-Allow-Origin headers, want exactly 1", n)
	}
	// And ours wins, because upstream's list omits Accept.
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Accept") {
		t.Errorf("allow-headers = %q, want Accept — oras sends 243 bytes of it", got)
	}
}

// TestPreflightIsAnsweredHere: upstream 405s the blob preflight, and a 405
// preflight means the real request is never sent.
func TestPreflightIsAnsweredHere(t *testing.T) {
	up := upstreamLike(t)
	h, _ := handler(up.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/v2/p/blobs/sha256:deadbeef", nil)
	req.Header.Set("Origin", "http://page.test")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "accept")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status %d, want 204", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Accept") {
		t.Errorf("preflight does not allow Accept: %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}
	if rec.Header().Get("Access-Control-Max-Age") == "" {
		t.Error("no Max-Age: every request would preflight again")
	}
}

func TestHandlerRejectsABadUpstream(t *testing.T) {
	for _, bad := range []string{"", "127.0.0.1:5111", "://nope", "not a url at all"} {
		if _, err := handler(bad); err == nil {
			t.Errorf("upstream %q was accepted", bad)
		}
	}
}

func TestRunUsesTheDefaultsAndReportsThem(t *testing.T) {
	old := listenAndServe
	var gotAddr string
	listenAndServe = func(addr string, _ http.Handler) error {
		gotAddr = addr
		return nil
	}
	defer func() { listenAndServe = old }()
	var log strings.Builder

	if err := run(func(string) string { return "" }, &log); err != nil {
		t.Fatal(err)
	}

	if gotAddr != "127.0.0.1:5112" {
		t.Errorf("addr = %q", gotAddr)
	}
	if !strings.Contains(log.String(), "127.0.0.1:5111") {
		t.Errorf("the log does not say what it fronts: %q", log.String())
	}
}

func TestRunHonoursTheEnvironment(t *testing.T) {
	old := listenAndServe
	var gotAddr string
	listenAndServe = func(addr string, _ http.Handler) error { gotAddr = addr; return nil }
	defer func() { listenAndServe = old }()
	env := map[string]string{"UPSTREAM": "https://registry.test", "ADDR": "0.0.0.0:9999"}

	if err := run(func(k string) string { return env[k] }, io.Discard); err != nil {
		t.Fatal(err)
	}
	if gotAddr != "0.0.0.0:9999" {
		t.Errorf("addr = %q", gotAddr)
	}
}

func TestRunSurfacesFailures(t *testing.T) {
	if err := run(func(k string) string {
		if k == "UPSTREAM" {
			return "://bad"
		}
		return ""
	}, io.Discard); err == nil {
		t.Error("a bad upstream must fail the run")
	}

	old := listenAndServe
	listenAndServe = func(string, http.Handler) error { return errors.New("port taken") }
	defer func() { listenAndServe = old }()
	if err := run(func(string) string { return "" }, io.Discard); err == nil {
		t.Error("a failed listen must be reported")
	}
}

func TestMainExitsOnFailure(t *testing.T) {
	oldExit, oldListen := osExit, listenAndServe
	code := -1
	osExit = func(c int) { code = c }
	listenAndServe = func(string, http.Handler) error { return errors.New("boom") }
	defer func() { osExit, listenAndServe = oldExit, oldListen }()

	main()

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
