// Command goproxycheck asserts that every workflow bootstrapping a go-pkgx
// tool states one module-proxy policy, in one place per file, and that no
// command line contradicts it.
//
// # The incident
//
// On 2026-10-12 the darwin lane failed twice in a row, ninety seconds in,
// having built nothing:
//
//	../../../go/pkg/mod/github.com/go-pkgx/bottle@v0.44.0/hcl.go:13:2:
//	  reading gopkg.in/yaml.v3/go.mod at revision v3.0.1: git ls-remote ...
//	  exit status 128:
//	fatal: unable to access 'https://gopkg.in/yaml.v3/':
//	  The requested URL returned error: 502
//
// The step was `GOPROXY=direct go install github.com/go-pkgx/pkgx@main`, and
// fourteen sites across eight workflows said exactly that. `direct` was
// deliberate and the reason was written beside it — "pull current main from
// GitHub, not a stale proxy cache" — because a lane that builds the fleet's
// own tools has to get the commit it asked for.
//
// But `GOPROXY=direct` applies to the WHOLE graph, not to the module named on
// the line. It makes the bootstrap depend on every vanity-import host in the
// closure being up, and gopkg.in was down intermittently for hours. Measured
// the same morning, from this machine, with a clean module cache:
//
//	GOPROXY=direct                                      -> exit 1, the 502 above
//	GOPROXY=https://proxy.golang.org,direct             -> exit 0
//	  GONOPROXY=github.com/go-pkgx/*
//
// Both arms in the same minute, so the difference is the configuration and
// not the weather.
//
// # Why the retry loop that was almost written here is not the fix
//
// The first attempt at this was a composite action wrapping the install in
// three tries. It would not have helped: the outage lasted hours, not
// seconds, and `gopkg.in?go-get=1` answered 502 from this machine at the same
// minute the runner saw it. A retry turns an outage into a slower outage.
// Worse, a composite action could not have covered the three `go run` sites in
// ci.yml, and `fromscratch.yml` has no `actions/checkout`, so the action would
// not even have loaded there.
//
// # Why GONOPROXY keeps what `direct` was for
//
// GONOPROXY is a pattern: the matching modules are fetched from their origin,
// everything else from the proxy. Proven as a pair, pointing GOPROXY at a dead
// address so only the direct path can answer:
//
//	GOPROXY=http://127.0.0.1:1 GONOPROXY=github.com/go-pkgx/*
//	  go list -m github.com/go-pkgx/pkgx@main  -> exit 0, v0.16.1
//	  go list -m gopkg.in/yaml.v3@v3.0.1       -> exit 1, connection refused
//
// The first says our modules really do bypass the proxy; the second says the
// pattern is narrow and has not quietly disabled it for everything. Checking
// only the first would have passed for a configuration that bypassed the proxy
// altogether, which is the status quo under a different name.
//
// Note what could NOT be measured: every go-pkgx repository had its main tip
// equal to its newest tag that morning, so `@main` resolved to a tag and the
// proxy and the origin agreed by construction. The dead-proxy pair is here
// precisely because the natural experiment could not tell the two apart.
//
// # The rule
//
// Workflow-level `env:` is the narrowest scope GitHub offers that covers every
// `go` invocation in a file, including one buried in a longer script, and it
// needs no checkout. So the policy is stated once per file and this guard
// keeps the eight copies level — eight declarations of configuration plus one
// executable rule, rather than fourteen command lines that can drift apart
// silently.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The policy, written once. A workflow that bootstraps a tool must declare
// exactly this.
const (
	WantGOPROXY   = "https://proxy.golang.org,direct"
	WantGONOPROXY = "github.com/go-pkgx/*"
)

// remoteModuleRun matches `go install`/`go run` of a module fetched from
// somewhere else — the `@` is what makes it remote. `go build ./...` and
// `go test ./...` on the checked-out tree are not matched: they need the proxy
// too, but they fail loudly in a way nobody mistakes for a broken recipe, and
// requiring the policy of every workflow that merely runs the tests would make
// the guard about Go rather than about bootstrapping.
//
// `[^@\s]+@` and not `.*@`: the module path cannot contain a space, and
// anchoring on the run of non-space characters keeps `go run ./x && echo a@b`
// from matching.
var remoteModuleRun = regexp.MustCompile(`\bgo\s+(?:install|run)\s+[^@\s]+@`)

// inlineGOPROXY matches an assignment on a command line. Both the `env FOO=`
// and the plain `FOO=cmd` prefix forms, and an `export`, because all three
// reach the same place.
var inlineGOPROXY = regexp.MustCompile(`\bGO(?:NO)?(?:PROXY|PRIVATE)\s*=`)

// A workflow and a composite action keep their steps in different places, so
// the decoder accepts both shapes and the walk does not have to know which
// kind of file it opened.
type doc struct {
	Env  map[string]string `yaml:"env"`
	Jobs map[string]struct {
		Steps []step `yaml:"steps"`
	} `yaml:"jobs"`
	Runs struct {
		Steps []step `yaml:"steps"`
	} `yaml:"runs"`
}

type step struct {
	Run string `yaml:"run"`
}

// scripts returns every shell body in the file. Jobs are visited in name order
// so two runs over the same tree report in the same order; a map range would
// make the diagnostics shuffle between runs and look like movement.
func (d *doc) scripts() []string {
	var out []string
	names := make([]string, 0, len(d.Jobs))
	for n := range d.Jobs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for _, s := range d.Jobs[n].Steps {
			if s.Run != "" {
				out = append(out, s.Run)
			}
		}
	}
	for _, s := range d.Runs.Steps {
		if s.Run != "" {
			out = append(out, s.Run)
		}
	}
	return out
}

// Check reports what is wrong with one file, as a list of complaints. An empty
// list means it conforms.
//
// It returns complaints rather than printing them so the test can read them,
// and so a file with two faults reports both: a checker that stops at the
// first makes a second pass necessary to find out whether anything else is
// wrong.
func Check(name string, src []byte) ([]string, error) {
	var d doc
	if err := yaml.Unmarshal(src, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	var bad []string
	bootstraps := false
	for _, s := range d.scripts() {
		for _, line := range strings.Split(s, "\n") {
			// A comment sets nothing. The first pass over the real tree
			// reported three faults that were the sentence EXPLAINING the
			// policy — "# GOPROXY=direct: pull current main from GitHub, not a
			// stale proxy cache" — so a guard without this would push whoever
			// obeys it into deleting the reasoning to quiet the tool.
			//
			// Only a line that STARTS with `#`, not everything after the first
			// one: `#` is an ordinary character inside a shell word, and
			// trimming from it would hide a real assignment written after a
			// quoted `#`.
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if inlineGOPROXY.MatchString(line) {
				bad = append(bad, fmt.Sprintf("%s: a command line sets the module-proxy policy: %s\n"+
					"\tthe policy belongs in the workflow-level env:, where it covers every go invocation in the file;\n"+
					"\tan inline assignment overrides it for one line and drifts from the other files",
					name, strings.TrimSpace(line)))
			}
			if remoteModuleRun.MatchString(line) {
				bootstraps = true
			}
		}
	}
	if !bootstraps {
		// Nothing here fetches a module by version, so the policy is not
		// required. Demanding it everywhere would put it in files that have no
		// use for it, and a declaration nobody needs is one nobody maintains.
		return bad, nil
	}
	for k, want := range map[string]string{"GOPROXY": WantGOPROXY, "GONOPROXY": WantGONOPROXY} {
		got, ok := d.Env[k]
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("%s: installs a module by version and its workflow-level env: does not set %s: %s", name, k, want))
		case got != want:
			bad = append(bad, fmt.Sprintf("%s: env: %s is %q, want %q", name, k, got, want))
		}
	}
	sort.Strings(bad)
	return bad, nil
}

// CheckDir walks a directory for workflow and action files.
func CheckDir(dir string) ([]string, int, error) {
	var bad []string
	seen := 0
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		if x := filepath.Ext(p); x != ".yml" && x != ".yaml" {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		seen++
		// No error arm on Rel: both paths come from this same WalkDir, so the
		// second is under the first by construction and Rel cannot fail. A
		// fallback here would be a branch no test could reach, which is how
		// unreachable code gets written and then trusted.
		rel, _ := filepath.Rel(dir, p)
		c, err := Check(rel, src)
		if err != nil {
			return err
		}
		bad = append(bad, c...)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Strings(bad)
	return bad, seen, nil
}

// osExit is the seam the test replaces, so main() itself is exercised rather
// than taken on trust. Same shape as ./cargolocked and ./overlaycheck.
var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	// ContinueOnError, not ExitOnError: a flag set that exits the process
	// cannot be tested, and a usage error must come back as a status like any
	// other failure.
	fs := flag.NewFlagSet("goproxycheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", ".github", "directory to walk for workflow and action files")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	w := stdout
	bad, seen, err := CheckDir(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "goproxycheck: %v\n", err)
		return 2
	}
	if seen == 0 {
		// ⛔ A walk that read nothing must not pass. A scan that could not
		// read its input and reported zero faults has happened here before,
		// and it is indistinguishable from a clean tree.
		fmt.Fprintf(stderr, "goproxycheck: no workflow or action file under %s — nothing was checked\n", *dir)
		return 2
	}
	for _, b := range bad {
		fmt.Fprintln(w, b)
	}
	fmt.Fprintf(w, "goproxycheck: %d file(s), %d complaint(s)\n", seen, len(bad))
	if len(bad) > 0 {
		return 1
	}
	return 0
}
