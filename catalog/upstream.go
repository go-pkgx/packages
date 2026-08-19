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
