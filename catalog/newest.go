package main

import "sort"

// newestGaps reports, per project, the platforms its NEWEST version lacks while
// some other version of the same project has them.
//
// The hole rule in forEachGap can never see these. It looks for a platform
// present both before and after the version that lacks it, so a platform
// missing from the newest version moves the run's end backwards and the newest
// version sits outside the range — silently, on the one version consumers
// resolve by default.
//
// cmake.org 4.4.2 is the case that surfaced it. Its index carries linux/arm64,
// darwin/arm64 and darwin/amd64; every cmake 3.x carries linux/amd64; 4.4.2
// does not. The audit reported 0 lost entries that morning, and the first
// anyone knew was freetype.org failing hours later with
//
//	pkgx: no bottle for cmake.org v4.4.2 (linux/x86-64): platform not in index
//
// This is deliberately NOT wired into the gate. For an interior hole the shape
// is the evidence — a platform that was there, went missing, and came back was
// lost. At the newest version there is no such evidence: "the upstream dist has
// it and we do not" is equally the signature of a version we simply have not
// built yet for that platform, and our per-platform build fronts do lag each
// other. Failing on that would put a permanently red lane in front of ordinary
// lag, which is the one thing the audit's own design says not to do.
//
// So it is a worklist, printed and counted, and a build is what tells the two
// apart.
func newestGaps(rows []row) []gap {
	byProject := map[string]map[string]map[string]bool{}
	for _, r := range rows {
		if byProject[r.Name] == nil {
			byProject[r.Name] = map[string]map[string]bool{}
		}
		if byProject[r.Name][r.Version] == nil {
			byProject[r.Name][r.Version] = map[string]bool{}
		}
		byProject[r.Name][r.Version][r.OS+"/"+r.Arch] = true
	}

	var out []gap
	for name, versions := range byProject {
		var ordered []string
		plats := map[string]bool{}
		for v, ps := range versions {
			ordered = append(ordered, v)
			for p := range ps {
				plats[p] = true
			}
		}
		sort.Slice(ordered, func(i, j int) bool { return lessVersion(ordered[i], ordered[j]) })
		newest := ordered[len(ordered)-1]

		var missing []string
		for p := range plats {
			if !versions[newest][p] {
				missing = append(missing, p)
			}
		}
		sort.Strings(missing)
		for _, p := range missing {
			out = append(out, gap{project: name, version: newest, platform: p})
		}
	}
	sortGaps(out)
	return out
}
