package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// auditSplitIndexes reports projects whose versions do not agree on the set of
// platforms they carry.
//
// Publishing a version's index is a read-modify-write on one mutable tag, and
// the arch jobs — plus the separate darwin workflow — race on it. A writer whose
// read predated another's write re-tags without that platform, verifies its own,
// and exits happy. The bottle is pushed, valid, and simply not in the index, so
// every install for that platform fails with
//
//	pkgx: no bottle for gnu.org/gperf v3.3 (linux/aarch64): platform not in index
//
// naming a package that looks published and is not. Two were found by tripping
// over them in a build (python.org 3.14.7 lost linux/arm64; gnu.org/gperf 3.3
// lost BOTH linux platforms to the darwin run). This finds the rest.
//
// It reports DISAGREEMENT, not a verdict. Nothing in a registry records which
// platforms a version was supposed to have, and when a race has hit the same
// project more than once the healthy set is genuinely ambiguous from the data
// alone — a rule that guessed would hide exactly the repeated case. So each
// distinct platform set is printed with the versions holding it, and the reader
// (or the recipe's own `platforms:`) decides which is the anomaly.
func auditSplitIndexes(rows []row, w io.Writer) int {
	// project -> version -> platforms
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

	var names []string
	for n := range byProject {
		names = append(names, n)
	}
	sort.Strings(names)

	found := 0
	for _, name := range names {
		// signature -> versions
		groups := map[string][]string{}
		for v, plats := range byProject[name] {
			groups[signature(plats)] = append(groups[signature(plats)], v)
		}
		if len(groups) < 2 {
			continue // every version agrees
		}
		found++
		var sigs []string
		for s := range groups {
			sigs = append(sigs, s)
		}
		// Widest platform set first: it is the one a split index departs from.
		sort.Slice(sigs, func(i, j int) bool {
			ci, cj := strings.Count(sigs[i], ","), strings.Count(sigs[j], ",")
			if ci != cj {
				return ci > cj
			}
			return sigs[i] < sigs[j]
		})
		fmt.Fprintf(w, "%s: %d different platform sets\n", name, len(groups))
		for _, s := range sigs {
			vs := groups[s]
			sort.Strings(vs)
			fmt.Fprintf(w, "    [%s] %d version(s): %s\n", s, len(vs), strings.Join(vs, " "))
		}
	}
	return found
}

// signature renders a platform set as a stable, comparable string.
func signature(plats map[string]bool) string {
	var out []string
	for p := range plats {
		out = append(out, p)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// auditPlatformGaps is the high-confidence half of the audit: a version that
// lacks a platform which BOTH an older and a newer version of the same project
// carry.
//
// Plain disagreement between versions is mostly history — a project gains
// darwin/aarch64, or windows, and every earlier version legitimately lacks it.
// That reads as a step, and there are hundreds of them. A race reads as a GAP:
// the platform is there before and after, and missing in between. Nothing about
// building a package produces that shape; losing an index write does.
// collectPlatformGaps returns every gap, for a caller that wants to classify
// them; auditPlatformGaps prints them unclassified.
func collectPlatformGaps(rows []row) []gap {
	var out []gap
	forEachGap(rows, func(g gap) { out = append(out, g) })
	sortGaps(out)
	return out
}

func auditPlatformGaps(rows []row, w io.Writer) int {
	byProject := map[string]map[string]map[string]bool{} // project -> version -> platforms
	for _, r := range rows {
		if byProject[r.Name] == nil {
			byProject[r.Name] = map[string]map[string]bool{}
		}
		if byProject[r.Name][r.Version] == nil {
			byProject[r.Name][r.Version] = map[string]bool{}
		}
		byProject[r.Name][r.Version][r.OS+"/"+r.Arch] = true
	}

	var names []string
	for n := range byProject {
		names = append(names, n)
	}
	sort.Strings(names)

	found := 0
	forEachGap(rows, func(g gap) {
		found++
		fmt.Fprintf(w, "%s %s: missing %s (present in both older and newer versions)\n", g.project, g.version, g.platform)
	})
	return found
}

// forEachGap walks the gaps in a stable order and hands each to fn.
func forEachGap(rows []row, fn func(gap)) {
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
	var names []string
	for n := range byProject {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		versions := byProject[name]
		var ordered []string
		plats := map[string]bool{}
		for v, ps := range versions {
			ordered = append(ordered, v)
			for p := range ps {
				plats[p] = true
			}
		}
		sort.Slice(ordered, func(i, j int) bool { return lessVersion(ordered[i], ordered[j]) })

		var platList []string
		for p := range plats {
			platList = append(platList, p)
		}
		sort.Strings(platList)

		gaps := map[string][]string{} // version -> missing platforms
		for _, p := range platList {
			// First and last index carrying p; anything between them that lacks
			// it is a hole in an otherwise continuous run.
			first, last := -1, -1
			for i, v := range ordered {
				if versions[v][p] {
					if first < 0 {
						first = i
					}
					last = i
				}
			}
			// first is always >= 0: platList comes from these very versions.
			for i := first + 1; i < last; i++ {
				if !versions[ordered[i]][p] {
					gaps[ordered[i]] = append(gaps[ordered[i]], p)
				}
			}
		}
		if len(gaps) == 0 {
			continue
		}
		var gv []string
		for v := range gaps {
			gv = append(gv, v)
		}
		sort.Slice(gv, func(i, j int) bool { return lessVersion(gv[i], gv[j]) })
		for _, v := range gv {
			sort.Strings(gaps[v])
			for _, p := range gaps[v] {
				fn(gap{project: name, version: v, platform: p})
			}
		}
	}
}

// lessVersion orders version strings numerically component by component, so
// 1.10.0 sorts after 1.9.0 rather than before it. A non-numeric component
// compares as text, which is enough to keep a project's own versions in a
// stable, sensible order.
func lessVersion(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, errA := strconv.Atoi(as[i])
		y, errB := strconv.Atoi(bs[i])
		if errA == nil && errB == nil {
			if x != y {
				return x < y
			}
			continue
		}
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}
