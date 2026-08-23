// Command browsergw fronts an OCI registry with the CORS headers a browser
// needs, so a page can install a package from it.
//
// The registry we run for this — zot — answers /v2/, /tags/list and
// /manifests/… with Access-Control-Allow-Origin, and answers /blobs/… WITHOUT
// it (its preflight there 405s). Measured against zot v2.1.14:
//
//	/v2/               ACAO present
//	/tags/list         ACAO present
//	/manifests/<ref>   ACAO present
//	/blobs/<digest>    ACAO ABSENT, OPTIONS → 405
//
// So a page can list versions and resolve a manifest, and cannot download the
// layer — which is every install. This gateway supplies the headers uniformly.
//
// It is a gateway, not a cache and not a rewriter: bodies pass through
// untouched, so the digest a client verifies is the registry's own bytes and
// the signature check downstream is unaffected. The durable fix is upstream in
// zot; until then this is what makes a browser install possible, and it is
// small enough to read in one sitting.
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

func main() {
	if err := run(os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "browsergw:", err)
		osExit(1)
	}
}

// osExit is a seam so the failure path is testable.
var osExit = os.Exit

// listenAndServe is a seam so tests never bind a port.
var listenAndServe = http.ListenAndServe

func run(getenv func(string) string, logw io.Writer) error {
	upstream := envOr(getenv, "UPSTREAM", "http://127.0.0.1:5111")
	addr := envOr(getenv, "ADDR", "127.0.0.1:5112")

	h, err := handler(upstream)
	if err != nil {
		return err
	}
	fmt.Fprintf(logw, "browsergw: %s → %s\n", addr, upstream)
	return listenAndServe(addr, h)
}

func envOr(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}

// allowedHeaders are what a registry client sends that CORS does not safelist.
// Accept is here because oras asks for five media types (243 bytes) and the
// safelist stops at 128 — past that the browser preflights, and a preflight
// that does not allow Accept is a request that never leaves the page.
const allowedHeaders = "Authorization,Accept,Content-Type,Range"

// exposedHeaders are what a client must be able to READ back. Cross-origin, a
// page sees only the safelisted response headers unless they are named here,
// and an OCI client that cannot read Docker-Content-Digest cannot check what it
// just downloaded.
const exposedHeaders = "Docker-Content-Digest,Content-Length,Content-Range,Content-Type"

const allowedMethods = "GET,HEAD,OPTIONS"

// handler builds the reverse proxy for upstream.
func handler(upstream string) (http.Handler, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, fmt.Errorf("upstream %q: %w", upstream, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("upstream %q: want scheme://host", upstream)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)

	// DELETE then set. The registry already sends these on some routes, and two
	// Access-Control-Allow-Origin headers make a browser reject the response
	// outright — so "add ours" would break exactly the paths that worked.
	proxy.ModifyResponse = func(r *http.Response) error {
		setCORS(r.Header)
		return nil
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			// Answer the preflight here: upstream 405s it on the blob route,
			// and a 405 preflight means the real request is never sent.
			setCORS(w.Header())
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

func setCORS(h http.Header) {
	for _, k := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
		"Access-Control-Expose-Headers",
	} {
		h.Del(k)
	}
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", allowedMethods)
	h.Set("Access-Control-Allow-Headers", allowedHeaders)
	h.Set("Access-Control-Expose-Headers", exposedHeaders)
}
