// Command catalog enumerates the true package catalog from the ghcr OCI
// registry ghcr.io/go-pkgx/packages and writes it as registry.json.
//
// The Pages viewer counts and renders whatever registry.json contains. Building
// that file from a single build run's dist artifacts undercounts the catalog:
// the linux/darwin factories skip already-published packages, so their per-run
// dist tree is nearly empty, while windows rebuilds everything. The authoritative
// catalog is what actually lives in ghcr, so this tool reads it directly:
//
//  1. list the org's container packages (GitHub Packages API),
//  2. list each package's versions (created_at + semantic tags),
//  3. read each version tag's OCI image index for its {os, architecture} set,
//
// and emits one JSON row {name, os, arch, version, published} per
// (name, os, arch, version) to stdout — the same shape the viewer and the no-JS
// list.html already consume.
//
//	GITHUB_TOKEN=… GITHUB_REPOSITORY_OWNER=go-pkgx catalog > registry.json
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// osExit is a seam so a test can exercise the failure path without exiting.
var osExit = os.Exit

func main() {
	if err := run(os.Stdout, os.Stderr, os.Getenv, http.DefaultClient); err != nil {
		fmt.Fprintln(os.Stderr, "catalog:", err)
		osExit(1)
	}
}

// row is one emitted registry.json record. Field order fixes the JSON key order.
type row struct {
	Name      string `json:"name"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Version   string `json:"version"`
	Published string `json:"published"`
}

// doer is the injected HTTP seam (satisfied by *http.Client) so tests can mock
// the GitHub Packages API, the ghcr pull-token endpoint and the OCI registry
// without touching the network.
type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// client bundles the enumeration configuration and endpoints. apiBase and
// ghcrBase are fields so tests can point them at a mock.
type client struct {
	doer     doer
	apiBase  string // https://api.github.com
	ghcrBase string // https://ghcr.io
	token    string // GitHub token with packages:read (may be empty)
	owner    string // org that owns the packages
	stderr   io.Writer
}

func run(stdout, stderr io.Writer, getenv func(string) string, d doer) error {
	c := &client{
		doer:     d,
		apiBase:  envOr(getenv, "GITHUB_API_URL", "https://api.github.com"),
		ghcrBase: envOr(getenv, "GHCR_URL", "https://ghcr.io"),
		token:    getenv("GITHUB_TOKEN"),
		owner:    envOr(getenv, "GITHUB_REPOSITORY_OWNER", "go-pkgx"),
		stderr:   stderr,
	}

	names, err := c.listPackageNames()
	if err != nil {
		return err
	}

	workers := 8
	if n, err := strconv.Atoi(getenv("CATALOG_CONCURRENCY")); err == nil && n > 0 {
		workers = n
	}
	rows := c.collect(names, workers)

	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.OS != b.OS {
			return a.OS < b.OS
		}
		if a.Arch != b.Arch {
			return a.Arch < b.Arch
		}
		return a.Version < b.Version
	})

	enc := json.NewEncoder(stdout)
	enc.SetIndent("", " ")
	return enc.Encode(rows)
}

// collect fans the package names out across a bounded worker pool, tolerating a
// per-package failure (logged to stderr) so one bad package never aborts the run.
func (c *client) collect(names []string, workers int) []row {
	in := make(chan string)
	var (
		mu  sync.Mutex
		out []row
		wg  sync.WaitGroup
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range in {
				rs := c.packageRows(name)
				mu.Lock()
				out = append(out, rs...)
				mu.Unlock()
			}
		}()
	}
	for _, n := range names {
		in <- n
	}
	close(in)
	wg.Wait()
	return out
}

// packageRows resolves every (os, arch, version) row for one package, skipping
// and logging any error rather than propagating it.
func (c *client) packageRows(name string) []row {
	versions, err := c.listVersions(name)
	if err != nil {
		fmt.Fprintf(c.stderr, "catalog: skip %s versions: %v\n", name, err)
		return nil
	}
	bearer, err := c.ghcrToken(name)
	if err != nil {
		fmt.Fprintf(c.stderr, "catalog: skip %s token: %v\n", name, err)
		return nil
	}
	var rows []row
	for _, v := range versions {
		for _, tag := range v.tags {
			plats, err := c.platforms(name, tag, bearer)
			if err != nil {
				fmt.Fprintf(c.stderr, "catalog: skip %s:%s: %v\n", name, tag, err)
				continue
			}
			for _, p := range plats {
				rows = append(rows, row{
					Name:      name,
					OS:        p.os,
					Arch:      mapArch(p.arch),
					Version:   tag,
					Published: v.published,
				})
			}
		}
	}
	return rows
}

// listPackageNames returns the org's container package names with the required
// "packages/" prefix stripped.
func (c *client) listPackageNames() ([]string, error) {
	url := c.apiBase + "/orgs/" + c.owner + "/packages?package_type=container&per_page=100"
	var names []string
	err := c.paginate(url, func(b []byte) error {
		var page []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		for _, p := range page {
			if n, ok := strings.CutPrefix(p.Name, "packages/"); ok {
				names = append(names, n)
			}
		}
		return nil
	})
	return names, err
}

type version struct {
	published string // created_at date (YYYY-MM-DD)
	tags      []string
}

// listVersions returns each container version's created_at date and its
// semantic tags (sha256-* digests dropped).
func (c *client) listVersions(name string) ([]version, error) {
	enc := urlEncode("packages/" + name)
	url := c.apiBase + "/orgs/" + c.owner + "/packages/container/" + enc + "/versions?per_page=100"
	var versions []version
	err := c.paginate(url, func(b []byte) error {
		var page []struct {
			CreatedAt string `json:"created_at"`
			Metadata  struct {
				Container struct {
					Tags []string `json:"tags"`
				} `json:"container"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		for _, v := range page {
			var tags []string
			for _, t := range v.Metadata.Container.Tags {
				if !strings.HasPrefix(t, "sha256-") {
					tags = append(tags, t)
				}
			}
			if len(tags) == 0 {
				continue
			}
			versions = append(versions, version{
				published: dateOnly(v.CreatedAt),
				tags:      tags,
			})
		}
		return nil
	})
	return versions, err
}

// ghcrToken fetches an anonymous pull token for a public package's OCI repo.
func (c *client) ghcrToken(name string) (string, error) {
	url := c.ghcrBase + "/token?service=ghcr.io&scope=repository:" +
		c.owner + "/packages/" + name + ":pull"
	resp, err := c.get(url, "", "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tok struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	return tok.Token, nil
}

type platform struct {
	os   string
	arch string
}

// platforms reads a version tag's OCI image index and returns its per-manifest
// {os, architecture}, dropping attestation/unknown entries.
func (c *client) platforms(name, tag, bearer string) ([]platform, error) {
	url := c.ghcrBase + "/v2/" + c.owner + "/packages/" + name + "/manifests/" + tag
	resp, err := c.get(url, "application/vnd.oci.image.index.v1+json", bearer)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var idx struct {
		Manifests []struct {
			Platform struct {
				OS   string `json:"os"`
				Arch string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, err
	}
	var plats []platform
	for _, m := range idx.Manifests {
		if m.Platform.OS == "" || m.Platform.OS == "unknown" ||
			m.Platform.Arch == "" || m.Platform.Arch == "unknown" {
			continue
		}
		plats = append(plats, platform{os: m.Platform.OS, arch: m.Platform.Arch})
	}
	return plats, nil
}

// paginate GETs a GitHub API URL and follows rel="next" Link headers, passing
// each page's body to fn.
func (c *client) paginate(url string, fn func([]byte) error) error {
	for url != "" {
		resp, err := c.get(url, "application/vnd.github+json", c.token)
		if err != nil {
			return err
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if err := fn(b); err != nil {
			return err
		}
		url = nextLink(resp.Header.Get("Link"))
	}
	return nil
}

// get issues an authenticated GET and returns the response for a 2xx status, or
// an error (with the body) otherwise. token, when non-empty, is sent as a
// Bearer credential.
func (c *client) get(url, accept, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

// nextLink extracts the rel="next" URL from an RFC 8288 Link header, or "".
func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		seg := strings.Split(strings.TrimSpace(part), ";")
		if len(seg) < 2 {
			continue
		}
		u := strings.TrimSpace(seg[0])
		if !strings.HasPrefix(u, "<") || !strings.HasSuffix(u, ">") {
			continue
		}
		for _, p := range seg[1:] {
			if strings.TrimSpace(p) == `rel="next"` {
				return u[1 : len(u)-1]
			}
		}
	}
	return ""
}

// mapArch normalizes OCI arch names to the dist-tree convention the viewer
// groups by (linux/x86-64, …).
func mapArch(a string) string {
	switch a {
	case "amd64":
		return "x86-64"
	case "arm64":
		return "aarch64"
	default:
		return a
	}
}

// urlEncode percent-encodes every reserved byte of a path segment (so a '/' in a
// package name becomes %2F), matching the encoding the GitHub Packages API path
// requires.
func urlEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if 'A' <= ch && ch <= 'Z' || 'a' <= ch && ch <= 'z' ||
			'0' <= ch && ch <= '9' || ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			b.WriteByte(ch)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[ch>>4])
		b.WriteByte(hex[ch&0x0f])
	}
	return b.String()
}

// dateOnly keeps the YYYY-MM-DD prefix of an RFC 3339 timestamp.
func dateOnly(ts string) string {
	if t, _, ok := strings.Cut(ts, "T"); ok {
		return t
	}
	return ts
}

func envOr(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}
