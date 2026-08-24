// Command catalog enumerates the true package catalog from the ghcr OCI
// registry ghcr.io/go-pkgx/packages and writes it as registry.json — using NO
// GitHub token. The org Packages API 400s under a plain GITHUB_TOKEN, so instead
// of listing the org's packages server-side this tool probes ghcr anonymously:
//
//   - Candidate package names come from the repository, not the org API: every
//     non-comment line of recipes.txt (the linux/darwin candidates) unioned with
//     the first '|'-field of windows/go-projects.txt and windows/rust-projects.txt
//     (the windows slugs).
//   - For each candidate it fetches an anonymous ghcr pull token
//     (https://ghcr.io/token?service=ghcr.io&scope=repository:OWNER/packages/NAME:pull).
//     Public packages yield a 200 + token; unpublished/inaccessible names yield a
//     403, which is the "not published" signal — the candidate is skipped.
//   - With the bearer it reads the version tags
//     (GET /v2/OWNER/packages/NAME/tags/list), dropping sha256-* digests and any
//     non-semver junk, and then each tag's OCI image index
//     (GET /v2/OWNER/packages/NAME/manifests/TAG) for its {os, architecture} set.
//
// It emits one JSON row {name, os, arch, version} per (name, os, arch, version)
// to stdout — the same shape the viewer and the no-JS list.html consume, minus
// the publish date (anonymous ghcr exposes no created_at without the org API).
//
//	GITHUB_REPOSITORY_OWNER=go-pkgx catalog > registry.json
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
	if err := run(os.Stdout, os.Stderr, os.Getenv, http.DefaultClient, os.ReadFile); err != nil {
		fmt.Fprintln(os.Stderr, "catalog:", err)
		osExit(1)
	}
}

// row is one emitted registry.json record. Field order fixes the JSON key order.
type row struct {
	Name    string `json:"name"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Version string `json:"version"`
}

// doer is the injected HTTP seam (satisfied by *http.Client) so tests can mock
// the ghcr pull-token endpoint and the OCI registry without touching the network.
type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// client bundles the enumeration configuration and endpoints. ghcrBase is a
// field so tests can point it at a mock.
type client struct {
	doer     doer
	ghcrBase string // https://ghcr.io
	owner    string // org that owns the packages
	stderr   io.Writer
}

func run(stdout, stderr io.Writer, getenv func(string) string, d doer, readFile func(string) ([]byte, error)) error {
	c := &client{
		doer:     d,
		ghcrBase: envOr(getenv, "GHCR_URL", "https://ghcr.io"),
		owner:    envOr(getenv, "GITHUB_REPOSITORY_OWNER", "go-pkgx"),
		stderr:   stderr,
	}

	names, err := candidateNames(readFile)
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

	// AUDIT=1 reports split multi-platform indexes instead of emitting the
	// catalogue: the same enumeration answers both questions, and a second
	// crawler would be a second thing to keep honest.
	if getenv("AUDIT") != "" {
		// A gap is the shape a lost index write leaves — and also the shape of a
		// version upstream only ever published for some platforms. Ask upstream
		// which it is, because the two call for opposite actions: re-dispatch, or
		// nothing at all.
		up := newUpstreamIndex(func(url string) (*http.Response, error) {
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			return c.doer.Do(req)
		})
		lost, absent := classifyGaps(collectPlatformGaps(rows), up)
		fmt.Fprintf(stdout, "--- LOST: upstream has these, our index does not — a re-dispatch puts them back ---\n")
		for _, g := range lost {
			fmt.Fprintf(stdout, "%s %s: missing %s\n", g.project, g.version, g.platform)
		}
		fmt.Fprintf(stdout, "\n--- ABSENT: upstream has no such bottle either — nothing to heal ---\n")
		for _, g := range absent {
			fmt.Fprintf(stdout, "%s %s: no %s anywhere\n", g.project, g.version, g.platform)
		}
		// A third bucket, reported and not gated: see newestGaps for why the
		// evidence that makes an interior hole a defect does not exist here.
		behind, _ := classifyGaps(newestGaps(rows), up)
		fmt.Fprintln(stdout, "\n--- BEHIND: our newest version lacks a platform the upstream dist carries — lost, or never built; only a build tells them apart ---")
		for _, g := range behind {
			fmt.Fprintf(stdout, "%s %s: no %s\n", g.project, g.version, g.platform)
		}
		fmt.Fprintf(stderr, "catalog: %d lost index entr(ies), %d genuine absence(s), %d newest-version gap(s)\n",
			len(lost), len(absent), len(behind))
		if getenv("AUDIT_ALL") != "" {
			fmt.Fprintln(stdout, "\n--- every project whose versions disagree (mostly history: a project gaining a platform) ---")
			n := auditSplitIndexes(rows, stdout)
			fmt.Fprintf(stderr, "catalog: %d project(s) whose versions disagree on platforms\n", n)
		}
		// A LOST entry is a defect: the bottle is in the registry and the index
		// does not list it, so every install for that platform fails on a package
		// that looks published. Fail, so a scheduled run is a GATE rather than a
		// report nobody reads — the count went from 137 to 0 by hand, and the race
		// that produced them is narrowed, not closed.
		//
		// Absences do not fail: upstream never published those bottles either,
		// there is nothing to do about them, and gating on them would mean a
		// permanently red lane that everyone learns to ignore.
		if len(lost) > 0 {
			return fmt.Errorf("%d index entr(ies) lost a platform the upstream dist still carries — re-dispatch those projects (see the LOST list above)", len(lost))
		}
		return nil
	}
	// SUMMARY=1 counts instead of emitting: the same crawl answers "what is
	// published" and "how much of recipes.txt is left", and a count derived here
	// cannot drift from the registry the way one typed into prose does.
	if getenv("SUMMARY") != "" {
		recipes, err := recipeNames(readFile)
		if err != nil {
			return err
		}
		summarize(stdout, rows, recipes)
		return nil
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", " ")
	return enc.Encode(rows)
}

// candidateNames returns the de-duplicated, sorted set of package slugs to probe
// in ghcr: every non-comment, non-blank recipes.txt line (the linux/darwin
// candidates) unioned with the first '|'-field of each windows project list.
// Every slug is lower-cased: ghcr/OCI repository names are lowercase, and the
// build factories publish each package under its lower-cased slug, so a slug
// with any upper-case letter (github.com/AOMediaCodec/…) must be probed — and
// emitted — in lower case to match the actual registry name.
// recipeNames is the recipes.txt half of the candidate list: the projects this
// factory is asked to build, without the windows-only slugs. summarize reports
// progress against it, so it is the list the factory's own front is measured on.
func recipeNames(readFile func(string) ([]byte, error)) ([]string, error) {
	data, err := readFile("recipes.txt")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.ToLower(line))
	}
	sort.Strings(out)
	return out, nil
}

func candidateNames(readFile func(string) ([]byte, error)) ([]string, error) {
	set := map[string]bool{}

	recipes, err := recipeNames(readFile)
	if err != nil {
		return nil, err
	}
	for _, name := range recipes {
		set[name] = true
	}

	for _, path := range []string{"windows/go-projects.txt", "windows/rust-projects.txt"} {
		data, err := readFile(path)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			slug, _, _ := strings.Cut(line, "|")
			if slug = strings.TrimSpace(slug); slug != "" {
				set[strings.ToLower(slug)] = true
			}
		}
	}

	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// collect fans the candidate names out across a bounded worker pool, tolerating a
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

// packageRows resolves every (os, arch, version) row for one candidate. A 403 at
// the token endpoint means "not published" and yields no rows silently; any real
// error is logged to stderr and skipped rather than propagated.
func (c *client) packageRows(name string) []row {
	bearer, err := c.ghcrToken(name)
	if err != nil {
		fmt.Fprintf(c.stderr, "catalog: skip %s token: %v\n", name, err)
		return nil
	}
	if bearer == "" {
		return nil // candidate not published (token denied)
	}
	tags, err := c.listTags(name, bearer)
	if err != nil {
		fmt.Fprintf(c.stderr, "catalog: skip %s tags: %v\n", name, err)
		return nil
	}
	var rows []row
	for _, tag := range tags {
		plats, err := c.platforms(name, tag, bearer)
		if err != nil {
			fmt.Fprintf(c.stderr, "catalog: skip %s:%s: %v\n", name, tag, err)
			continue
		}
		for _, p := range plats {
			rows = append(rows, row{
				Name:    name,
				OS:      p.os,
				Arch:    mapArch(p.arch),
				Version: tag,
			})
		}
	}
	return rows
}

// ghcrToken fetches an anonymous pull token for a public package's OCI repo. A
// 403/404 (the "not published / inaccessible" signal ghcr returns for an unknown
// name) yields ("", nil) so the caller skips the candidate silently.
func (c *client) ghcrToken(name string) (string, error) {
	url := c.ghcrBase + "/token?service=ghcr.io&scope=repository:" +
		c.owner + "/packages/" + name + ":pull"
	resp, code, err := c.rawGet(url, "", "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// 403 (denied) / 404 (unknown) / 400 (NAME_INVALID for a slug ghcr cannot
	// name) all mean "no such public package" — skip the candidate silently.
	if code == http.StatusForbidden || code == http.StatusNotFound || code == http.StatusBadRequest {
		return "", nil
	}
	// 401 is a THIRD state, and lumping it in with the crawl's real errors cost
	// twenty minutes to tell apart: the package repository EXISTS but refuses an
	// anonymous pull. Either it was never made public, or a publish created the
	// repository and pushed nothing into it. Both are invisible to every
	// consumer — `pkgm install` sees exactly what this crawl sees — so it yields
	// no rows like any unpublished candidate, but it says which state it is
	// instead of printing an authentication error that reads like a network
	// fault.
	if code == http.StatusUnauthorized {
		fmt.Fprintf(c.stderr, "catalog: %s exists but is not publicly pullable (401) — never made public, or an empty repository left by a failed publish\n", name)
		return "", nil
	}
	if code < 200 || code >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token %s: %s: %s", name, resp.Status, strings.TrimSpace(string(b)))
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	return tok.Token, nil
}

// listTags returns a package's semantic version tags (sha256-* digests and any
// non-semver junk dropped). A 404 (no such repo / empty) yields no tags.
func (c *client) listTags(name, bearer string) ([]string, error) {
	url := c.ghcrBase + "/v2/" + c.owner + "/packages/" + name + "/tags/list"
	resp, code, err := c.rawGet(url, "", bearer)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if code == http.StatusNotFound {
		return nil, nil
	}
	if code < 200 || code >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(b)))
	}
	var tl struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tl); err != nil {
		return nil, err
	}
	var tags []string
	for _, t := range tl.Tags {
		if semverTag(t) {
			tags = append(tags, t)
		}
	}
	sort.Strings(tags)
	return tags, nil
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

// rawGet issues a GET and returns the response and its status code without
// treating a non-2xx as an error, so callers can special-case 403/404.
// token, when non-empty, is sent as a Bearer credential.
func (c *client) rawGet(url, accept, token string) (*http.Response, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, 0, err
	}
	return resp, resp.StatusCode, nil
}

// get issues a GET and returns the response for a 2xx status, or an error (with
// the body) otherwise.
func (c *client) get(url, accept, token string) (*http.Response, error) {
	resp, code, err := c.rawGet(url, accept, token)
	if err != nil {
		return nil, err
	}
	if code < 200 || code >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

// semverTag reports whether t is a version tag worth keeping: not a sha256-*
// digest and not floating junk like "latest" — a leading optional 'v' followed
// by a digit (1.9.4, v1.2.3, 0.128.0), which is how the factories tag releases.
func semverTag(t string) bool {
	if t == "" || strings.HasPrefix(t, "sha256-") {
		return false
	}
	s := strings.TrimPrefix(t, "v")
	return s != "" && s[0] >= '0' && s[0] <= '9'
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

func envOr(getenv func(string) string, key, def string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return def
}
