// staletags deletes the published container versions that carry no build
// identity, and nothing else.
//
// ⛔⛔ WHY A TOOL AND NOT A LIST OF COMMANDS. On 2026-11-05 the wasip1 lane
// published thirty bottles before the factory could compute a Concrete, so
// every one of them landed on the bare address `<version>`. The same projects
// were republished on 2026-11-06 at `<version>-h<hash>`. Both now resolve, so
// `filippo.io/age@1.3.2` names TWO different artefacts and a reader cannot
// tell which bytes they get. An address that two builds can both claim is the
// exact failure the hashed layout exists to prevent — leaving the old one in
// place keeps the defect alive at a well-known name.
//
// ⛔ THE CRITERION IS A PROPERTY, NOT A DATE. "Published before 2026-11-06"
// would be true of these thirty and of nothing else TODAY, and silently wrong
// the next time a lane regresses. What makes a version defective is that its
// tags carry no identity, so that is what this asks.
//
// ⛔ A VERSION IS KEPT IF ANY OF ITS TAGS IS IDENTIFIED. The bottles are built
// reproducibly (SOURCE_DATE_EPOCH is pinned), so an old publish and a new one
// can produce byte-identical bytes — and then the registry holds ONE version
// wearing both the bare tag and the hashed one. Deleting it because of the
// bare tag would destroy the artefact the hashed tag addresses. The question
// is therefore asked of the version, never of the tag.
//
// ⛔ REFERRERS ARE FOLLOWED BY DIGEST, NOT GUESSED. A signature or provenance
// attachment is published as a version tagged `sha256-<hex>`, where <hex> is
// the digest of the manifest it describes. The REST API gives each version's
// digest in `name`, so a referrer is matched to its subject exactly: this
// deletes `sha256-<hex>` only when it is about to delete the version whose
// digest is <hex>. Nothing is removed on the strength of its date or its
// shape.
//
// ⛔ UNTAGGED VERSIONS ARE COUNTED AND LEFT ALONE. GHCR lists the children of
// a multi-arch index as versions of their own, and an untagged one may well be
// referenced by an index we are keeping. They are reported so that "clean" is
// a measured claim and not an impression.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// identityRE matches a tag that carries a build identity: the version,
// optionally a libc discriminator, then `-h` and the Merkle hash of the
// Concrete, optionally followed by `--<os>-<arch>` for the platform image of a
// multi-arch index.
//
// The hash is lowercase base32 without padding, as bottle writes it.
var identityRE = regexp.MustCompile(`-h[0-9a-z]{16}(--[a-z0-9-]+)?$`)

// referrerRE matches the tag under which an attachment (signature,
// provenance) is published: the digest of its subject, with `:` spelt `-`.
var referrerRE = regexp.MustCompile(`^sha256-([0-9a-f]{64})$`)

// identified says whether a tag addresses a build whose inputs are recorded.
func identified(tag string) bool { return identityRE.MatchString(tag) }

// pkgVersion is one version of a container package, as the REST API returns
// it. `Name` is the manifest digest.
type pkgVersion struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created_at"`
	Metadata struct {
		Container struct {
			Tags []string `json:"tags"`
		} `json:"container"`
	} `json:"metadata"`
}

func (v pkgVersion) tags() []string { return v.Metadata.Container.Tags }

// digestHex is the hex of the manifest digest, or "" if it is not a sha256.
func (v pkgVersion) digestHex() string {
	return strings.TrimPrefix(v.Name, "sha256:")
}

// verdict is what we decided about one version, and why.
type verdict struct {
	v      pkgVersion
	delete bool
	why    string
}

// classify decides, for every version of one package, which to delete.
//
// It is a pure function of the listing so that the decision can be tested
// without a registry.
func classify(versions []pkgVersion) []verdict {
	out := make([]verdict, 0, len(versions))
	// doomed holds the digests of the versions we are deleting, so that their
	// attachments can be matched to them exactly.
	doomed := map[string]bool{}

	type pending struct {
		v        pkgVersion
		referrer string // the subject digest, for a `sha256-…` tag
	}
	var later []pending

	for _, v := range versions {
		ts := v.tags()
		if len(ts) == 0 {
			out = append(out, verdict{v: v, why: "untagged — may be a child of an index we keep"})
			continue
		}
		if len(ts) == 1 {
			if m := referrerRE.FindStringSubmatch(ts[0]); m != nil {
				later = append(later, pending{v: v, referrer: m[1]})
				continue
			}
		}
		if anyIdentified(ts) {
			out = append(out, verdict{v: v, why: "carries a build identity"})
			continue
		}
		doomed[v.digestHex()] = true
		out = append(out, verdict{v: v, delete: true, why: "no tag carries a build identity"})
	}

	for _, p := range later {
		if doomed[p.referrer] {
			out = append(out, verdict{v: p.v, delete: true,
				why: "attached to " + short(p.referrer) + ", which is going"})
			continue
		}
		out = append(out, verdict{v: p.v,
			why: "attached to " + short(p.referrer) + ", which stays"})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].v.ID < out[j].v.ID })
	return out
}

func anyIdentified(tags []string) bool {
	for _, t := range tags {
		if identified(t) {
			return true
		}
	}
	return false
}

func short(hex string) string {
	if len(hex) > 12 {
		return hex[:12]
	}
	return hex
}

// --- the registry ---------------------------------------------------------

type client struct {
	token string
	http  *http.Client
}

func (c client) do(method, u string, out any) error {
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		return err
	}
	// ⛔ The credential travels in a header, never in the URL or an argument.
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

var errNotFound = errors.New("not found")

const apiBase = "https://api.github.com"

func (c client) versions(org, pkg string) ([]pkgVersion, error) {
	var all []pkgVersion
	for page := 1; page <= 20; page++ {
		u := fmt.Sprintf("%s/orgs/%s/packages/container/%s/versions?per_page=100&page=%d",
			apiBase, url.PathEscape(org), url.PathEscape(pkg), page)
		var got []pkgVersion
		if err := c.do(http.MethodGet, u, &got); err != nil {
			return nil, err
		}
		all = append(all, got...)
		if len(got) < 100 {
			return all, nil
		}
	}
	return nil, fmt.Errorf("%s/%s: more than 2000 versions; refusing to guess the rest", org, pkg)
}

func (c client) deleteVersion(org, pkg string, id int64) error {
	u := fmt.Sprintf("%s/orgs/%s/packages/container/%s/versions/%d",
		apiBase, url.PathEscape(org), url.PathEscape(pkg), id)
	return c.do(http.MethodDelete, u, nil)
}

// --- the inventory --------------------------------------------------------

type entry struct {
	Slug    string `yaml:"slug"`
	Version string `yaml:"version"`
}

type workflow struct {
	Jobs map[string]struct {
		Strategy struct {
			Matrix struct {
				Include []entry `yaml:"include"`
			} `yaml:"matrix"`
		} `yaml:"strategy"`
	} `yaml:"jobs"`
}

// projects reads the lane's matrix. The workflow is the only statement of
// what this lane publishes; a hand-kept copy here would be a second one.
func projects(path string) ([]entry, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w workflow
	if err := yaml.Unmarshal(body, &w); err != nil {
		return nil, err
	}
	var out []entry
	for _, j := range w.Jobs {
		for _, e := range j.Strategy.Matrix.Include {
			if e.Slug != "" {
				out = append(out, e)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no matrix entry has a slug — a scan that cannot read reports nothing to do", path)
	}
	return out, nil
}

// known says whether slug is one the lane publishes.
func known(projs []entry, slug string) bool {
	for _, p := range projs {
		if p.Slug == slug {
			return true
		}
	}
	return false
}

func tokenFrom(path string) (string, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".github-token")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(body))
	if tok == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return tok, nil
}
