package main

import (
	"fmt"
	"io"
	"sort"
)

// summarize reports what the crawl found, in the shape a README or a status
// note needs: how many projects are published, how many platform builds that
// is, the split per platform, and how much of recipes.txt is still ahead.
//
// It exists so no one has to hand-count from registry.json — a number typed
// into prose rots silently, and this repo's own README carried "currently
// live: five packages" while the registry held 1459 projects.
//
// recipes is the candidate list from recipes.txt alone (not the windows-only
// slugs), so "published / remaining" answers the question the factory asks.
func summarize(w io.Writer, rows []row, recipes []string) {
	projects := map[string]bool{}
	platforms := map[string]int{}
	for _, r := range rows {
		projects[r.Name] = true
		platforms[r.OS+"/"+r.Arch]++
	}

	fmt.Fprintf(w, "%d projects, %d platform builds\n", len(projects), len(rows))

	keys := make([]string, 0, len(platforms))
	for k := range platforms {
		keys = append(keys, k)
	}
	// Descending by count, then by name, so the biggest platform reads first
	// and the order is stable when two platforms tie.
	sort.Slice(keys, func(i, j int) bool {
		if platforms[keys[i]] != platforms[keys[j]] {
			return platforms[keys[i]] > platforms[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		fmt.Fprintf(w, "  %-18s %d\n", k, platforms[k])
	}

	var done int
	for _, name := range recipes {
		if projects[name] {
			done++
		}
	}
	fmt.Fprintf(w, "recipes.txt: %d of %d published, %d remaining\n",
		done, len(recipes), len(recipes)-done)
}
