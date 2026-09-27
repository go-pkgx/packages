package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// upstreamDist is the canonical pkgx distribution, used to tell a LOST index
// entry from a version that never existed for that platform anywhere.
const upstreamDist = "https://dist.pkgx.dev"

// upstreamIndex answers "does the upstream dist carry <project> <version> for
// <os>/<arch>?" from its per-platform versions.txt, fetched once per platform.
type upstreamIndex struct {
	get func(url string) (*http.Response, error)

	mu   sync.Mutex
	seen map[string]map[string]bool // "project|os/arch" -> version set
}

func newUpstreamIndex(get func(string) (*http.Response, error)) *upstreamIndex {
	return &upstreamIndex{get: get, seen: map[string]map[string]bool{}}
}

// has reports whether upstream carries that exact version for that platform.
// A fetch failure answers false — the caller treats "we could not confirm" the
// same as "not there", which keeps a network hiccup from silently reclassifying
// a real defect as an absence.
func (u *upstreamIndex) has(project, osn, arch, version string) bool {
	key := project + "|" + osn + "/" + arch
	u.mu.Lock()
	set, ok := u.seen[key]
	u.mu.Unlock()
	if !ok {
		set = u.fetch(project, osn, arch)
		u.mu.Lock()
		u.seen[key] = set
		u.mu.Unlock()
	}
	return set[version]
}

func (u *upstreamIndex) fetch(project, osn, arch string) map[string]bool {
	out := map[string]bool{}
	url := fmt.Sprintf("%s/%s/%s/%s/versions.txt", upstreamDist, project, osn, arch)
	resp, err := u.get(url)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return out
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if v := strings.TrimSpace(sc.Text()); v != "" {
			out[v] = true
		}
	}
	return out
}

// classifyGaps splits the gap report in two, because the two halves call for
// opposite actions.
//
// A gap is the shape a lost index write leaves: a platform present in an older
// AND a newer version, missing in between. But it is also the shape of a
// version that upstream only ever published for some platforms — gnu.org/glibc
// 2.28.0 exists for linux/aarch64 and has never existed for linux/x86-64,
// anywhere, so its "gap" can no more be healed than invented.
//
// So each gap is checked against the upstream dist. Missing there too: an
// ABSENCE, nothing to do but know it. Present there: a LOST entry, and a
// re-dispatch (or a mirror) puts it back.
func classifyGaps(gaps []gap, u *upstreamIndex) (lost, absent []gap) {
	for _, g := range gaps {
		osn, arch, _ := strings.Cut(g.platform, "/")
		if u.has(g.project, osn, arch, g.version) {
			lost = append(lost, g)
			continue
		}
		absent = append(absent, g)
	}
	sortGaps(lost)
	sortGaps(absent)
	return lost, absent
}

// gap is one (project, version, platform) the index does not list.
type gap struct {
	project  string
	version  string
	platform string
}

func sortGaps(gs []gap) {
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].project != gs[j].project {
			return gs[i].project < gs[j].project
		}
		if gs[i].version != gs[j].version {
			return lessVersion(gs[i].version, gs[j].version)
		}
		return gs[i].platform < gs[j].platform
	})
}

// manifestTag is the name a version's platform manifest is pushed under:
// `<ver>--<os>-<arch>`, which go-pkgx/bottle#37 introduced so a version's index
// could be composed from tags no two publishers contend for.
func manifestTag(g gap) string {
	return g.version + "--" + strings.ReplaceAll(g.platform, "/", "-")
}

// splitByPlatformTag divides lost entries into the ones an index recompose can
// fix and the ones only a build can.
//
// The distinction is the difference between a minute and an hour, and between
// 221 rebuilds and however few actually lost their bytes. A LOST entry means
// upstream carries that version for that platform and our index does not; it
// does NOT mean we ever pushed the bottle. The interior hole that made the
// entry a defect proves we built the PLATFORM for the project at other
// versions — nothing about this one.
//
// A package whose tags could not be listed at all yields no entry in have, and
// every one of its gaps lands in `rebuild`. That is the safe direction: a build
// republishes the index too, so treating a reindexable entry as a rebuild costs
// time, while the reverse would recompose an index around a manifest that is
// not there.
func splitByPlatformTag(lost []gap, have map[string]map[string]bool) (reindexable, rebuild []gap) {
	for _, g := range lost {
		if have[g.project][manifestKey(g.version, g.platform)] {
			reindexable = append(reindexable, g)
			continue
		}
		rebuild = append(rebuild, g)
	}
	return reindexable, rebuild
}

// indexOmitsAManifest names every platform manifest that is IN the registry
// under its own tag and missing from its version's index.
//
// This is the defect the audit exists for, stated by its mechanism instead of
// inferred from a project's shape. Publishing an index is a read-modify-write
// on one mutable tag and four publishers race on it; a writer whose read
// predated another's write re-tags without that platform, verifies its own,
// and exits happy. The bottle is pushed, valid, signed — and not listed. Since
// go-pkgx/bottle#37 each platform's manifest also gets an uncontended tag of
// its own, so the evidence of that race is sitting in the tag listing.
//
// It cannot have a false positive: the manifest is there or it is not.
//
// It CAN be blind, and the reason is HISTORY rather than a gap anyone has to
// close. Measured 2026-09-27, openssl.org carries 62 linux platform tags while
// zlib.net and curl.se carry only darwin ones and gnu.org/gperf none at all —
// which first read like publishers that disagree. They do not: bottle's push
// is the single publish path for a build and a mirror alike, and it has tagged
// every platform manifest since go-pkgx/bottle#37. What has no tag is what was
// published BEFORE that, and the set shrinks on its own as the catalogue is
// rebuilt.
//
// So nothing is owed here. The denominator beside the count is what says how
// far the evidence reaches on any given day.
//
// `have` is keyed by the PARSED manifest — `<version>\x00<os>/<arch>` — rather
// than by the tag text. A tag is parsed once, where it is read, so there is no
// second parse here to disagree with that one and no unreachable arm guarding
// against a key the map cannot hold.
func indexOmitsAManifest(rows []row, have map[string]map[string]bool) []gap {
	indexed := map[string]bool{} // project \x00 version \x00 platform
	for _, r := range rows {
		indexed[r.Name+"\x00"+r.Version+"\x00"+r.OS+"/"+r.Arch] = true
	}
	var out []gap
	for project, manifests := range have {
		for key := range manifests {
			ver, platform, _ := strings.Cut(key, "\x00")
			if !indexed[project+"\x00"+key] {
				out = append(out, gap{project: project, version: ver, platform: platform})
			}
		}
	}
	sortGaps(out)
	return out
}

// manifestKey is how a platform manifest is named inside `have`: parsed, not
// as the tag it was read from.
func manifestKey(version, platform string) string { return version + "\x00" + platform }

// splitPlatformTag reads `<ver>--<os>-<arch>` into the version and the
// `<os>/<arch>` platform it names. Only the FIRST hyphen of the right half
// separates them, because an arch is spelled x86-64 here.
//
// Deliberately narrow about the left half. `sha256-…--x` is not a version tag
// with a platform suffix, and neither is a bare `--linux-x86-64`; both would
// make the audit claim a manifest exists for a version nothing published.
func splitPlatformTag(t string) (version, platform string, ok bool) {
	ver, plat, ok := strings.Cut(t, "--")
	if !ok || !semverTag(ver) {
		return "", "", false
	}
	os, arch, ok := strings.Cut(plat, "-")
	if !ok || os == "" || arch == "" {
		return "", "", false
	}
	return ver, os + "/" + arch, true
}
