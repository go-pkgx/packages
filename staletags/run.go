package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ⛔⛔ go-pkgx IS THE REGISTRY CLIENTS STILL READ. bottle.DistBase points
// there, so every `zpkgs install` in existence resolves against it. Nothing
// in it carries a build identity — it predates them — so the criterion this
// tool applies would condemn the entire live registry. The guard is here and
// not in a comment because the two org names differ by four characters and
// the flag is typed by hand.
const liveRegistry = "go-pkgx"

type options struct {
	org      string
	prefix   string
	workflow string
	token    string
	only     string
	expect   int
	yes      bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "staletags:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	var o options
	fs := flag.NewFlagSet("staletags", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.org, "org", "go-zpkg", "the organisation that owns the packages")
	fs.StringVar(&o.prefix, "prefix", "packages/", "the registry prefix each project sits under")
	fs.StringVar(&o.workflow, "workflow", ".github/workflows/wasip1.yml", "the lane whose matrix says which projects to look at")
	fs.StringVar(&o.token, "token", "", "the credential to use (default ~/.github-token)")
	// ⛔ -only NARROWS THE WORK, NOT THE INVENTORY. The matrix is still read
	// and -expect still checked, so running one project cannot quietly become
	// a run whose denominator nobody looked at.
	fs.StringVar(&o.only, "only", "", "act on this slug alone; the inventory is still read in full")
	fs.IntVar(&o.expect, "expect", 0, "fail unless the matrix holds exactly this many projects")
	fs.BoolVar(&o.yes, "yes", false, "actually delete; without it, nothing is removed")
	fs.Usage = func() {
		fmt.Fprint(out, `usage: staletags [flags]

Delete the published container versions whose tags carry no build identity.
Without -yes it prints the plan and removes nothing. A deleted version cannot
be restored.

flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if o.org == liveRegistry {
		return fmt.Errorf("refusing to touch %s: it is the registry clients read, "+
			"and nothing in it carries a build identity — this would condemn all of it", liveRegistry)
	}

	projs, err := projects(o.workflow)
	if err != nil {
		return err
	}
	if o.expect > 0 && len(projs) != o.expect {
		return fmt.Errorf("%s holds %d project(s), expected %d — if the lane changed, "+
			"move -expect deliberately", o.workflow, len(projs), o.expect)
	}

	tok, err := tokenFrom(o.token)
	if err != nil {
		return err
	}
	c := client{token: tok, http: &http.Client{Timeout: 30 * time.Second}}

	if o.only != "" && !known(projs, o.only) {
		return fmt.Errorf("-only %q is not in %s — a filter that matches nothing "+
			"reports a clean registry", o.only, o.workflow)
	}

	var seen, keptN, doomedN, untagged, missing, deleted, skipped int
	for _, p := range projs {
		if o.only != "" && p.Slug != o.only {
			skipped++
			continue
		}
		pkg := o.prefix + p.Slug
		vs, err := c.versions(o.org, pkg)
		if err == errNotFound {
			missing++
			fmt.Fprintf(out, "%-56s not published in %s\n", p.Slug, o.org)
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", pkg, err)
		}
		seen += len(vs)

		vds := classify(vs)
		var doomed []verdict
		for _, vd := range vds {
			switch {
			case vd.delete:
				doomed = append(doomed, vd)
				doomedN++
			case len(vd.v.tags()) == 0:
				untagged++
			default:
				keptN++
			}
		}
		if len(doomed) == 0 {
			fmt.Fprintf(out, "%-56s clean (%d version(s))\n", p.Slug, len(vs))
			continue
		}
		fmt.Fprintf(out, "%-56s %d of %d version(s) to delete\n", p.Slug, len(doomed), len(vs))
		for _, vd := range doomed {
			fmt.Fprintf(out, "    %-11d %s  %-40s %s\n",
				vd.v.ID, vd.v.Created.Format("2006-01-02 15:04:05"),
				strings.Join(vd.v.tags(), ","), vd.why)
			if !o.yes {
				continue
			}
			if err := c.deleteVersion(o.org, pkg, vd.v.ID); err != nil {
				return fmt.Errorf("%s version %d: %w", pkg, vd.v.ID, err)
			}
			deleted++
		}
	}

	// ⛔ THE DENOMINATOR IS PRINTED. A sweep that could not read its input
	// reports nothing to do, which reads exactly like a clean registry.
	fmt.Fprintf(out, "\n%d of %d project(s), %d version(s) read: %d identified, %d untagged (left alone), "+
		"%d unidentified, %d not published here\n",
		len(projs)-skipped, len(projs), seen, keptN, untagged, doomedN, missing)
	if !o.yes {
		fmt.Fprintln(out, "nothing done. Pass -yes to delete; a deleted version cannot be restored.")
		return nil
	}
	fmt.Fprintf(out, "deleted %d version(s)\n", deleted)
	if deleted != doomedN {
		return fmt.Errorf("planned %d deletion(s) and made %d", doomedN, deleted)
	}
	return nil
}
