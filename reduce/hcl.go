package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/go-pkgx/bottle"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"gopkg.in/yaml.v3"
)

// reduceFile removes every top-level item the upstream recipe already states,
// and reports how many it KEPT because a comment belongs to them.
//
// Removal is by hclwrite, which edits the token stream and leaves everything
// it does not touch byte for byte — the alternative, re-emitting the document,
// would reformat 183 files and lose every comment in them.
func reduceFile(src []byte, name string, up map[string]any) ([]byte, int, error) {
	doc, err := hclDoc(src, name)
	if err != nil {
		return nil, 0, err
	}
	// Sorted, so the same file always reduces the same way. Two runs that
	// disagreed about the order would make the diff unreadable and the check
	// below meaningless.
	var keys []string
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	kept := 0
	out := src
	for _, k := range keys {
		u, in := up[k]
		if !in || !reflect.DeepEqual(doc[k], u) {
			continue // it differs, or upstream does not say it: this is the delta
		}
		trial, ok := removeTop(out, name, k)
		if !ok {
			// Neither a top-level block nor an attribute under that name — a
			// LABELLED block reads as a key in the document and is not one
			// here. Nothing to remove.
			continue
		}
		// A comment belongs to the item hclwrite removed, and it went with it.
		// Measured, not assumed: hclwrite takes a leading comment along with
		// the block or attribute it precedes.
		if lostAComment(out, trial) {
			kept++
			continue
		}
		out = trial
	}
	return tidy(out), kept, nil
}

// removeTop deletes one top-level attribute or block, reporting whether it
// found one.
//
// No error: reduceFile has already read this same source, so a parse failure
// here would be the same failure twice — and a second error path that no input
// can reach is a branch nobody can test. A file that does not parse and a key
// that is not there both mean "nothing to remove".
func removeTop(src []byte, name, key string) ([]byte, bool) {
	f, diags := hclwrite.ParseConfig(src, name, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, false
	}
	body := f.Body()
	if body.GetAttribute(key) != nil {
		body.RemoveAttribute(key)
		return f.Bytes(), true
	}
	for _, b := range body.Blocks() {
		if b.Type() == key && len(b.Labels()) == 0 {
			body.RemoveBlock(b)
			return f.Bytes(), true
		}
	}
	return nil, false
}

// lostAComment reports whether any comment present before is gone after.
//
// By TOKEN, not by line. Reading "a line starting with #" as a comment is a
// guess about text, and perl.org's script contains
//
//	sed -i.bak 's|^#!{{prefix}}/bin/|#!/usr/bin/env |' $x
//
// inside a string, where a `#` is a shebang and means nothing to HCL. The
// guess would have called that a comment and kept a block to protect it.
//
// The test is still behavioural — which item hclwrite considers a comment to
// belong to is its business, and all that matters is whether the text
// survived — but now it asks the lexer instead of guessing.
func lostAComment(before, after []byte) bool {
	have := map[string]int{}
	for _, c := range comments(after) {
		have[c]++
	}
	for _, c := range comments(before) {
		if have[c] == 0 {
			return true
		}
		have[c]--
	}
	return false
}

// comments lists the comment tokens in a file, in order.
func comments(src []byte) []string {
	f, diags := hclwrite.ParseConfig(src, "x.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil
	}
	var out []string
	for _, t := range f.BuildTokens(nil) {
		if t.Type == hclsyntax.TokenComment {
			out = append(out, strings.TrimSpace(string(t.Bytes)))
		}
	}
	return out
}

// tidy collapses the blank lines a removal leaves behind. hclwrite takes the
// item and its comment and leaves the newlines that surrounded them, so a file
// that loses six blocks ends up mostly empty lines.
//
// Textual, and safe because the property check downstream reads the DOCUMENT:
// whitespace between top-level items cannot change what a recipe says, and if
// it somehow did, sameAfterMerge would refuse the file.
func tidy(src []byte) []byte {
	var out []string
	blanks := 0
	for _, l := range strings.Split(string(src), "\n") {
		if strings.TrimSpace(l) == "" {
			blanks++
			continue
		}
		if blanks > 0 && len(out) > 0 {
			out = append(out, "")
		}
		blanks = 0
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

// sameAfterMerge is the property the whole reduction rests on: merged over
// upstream, the reduced entry must give the document the full entry gave.
func sameAfterMerge(full, reduced []byte, name string, up map[string]any) (bool, error) {
	a, err := hclDoc(full, name)
	if err != nil {
		return false, err
	}
	b, err := hclDoc(reduced, name)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(mergeOver(up, a), mergeOver(up, b)), nil
}

// mergeOver is bottle's rule, restated here because this tool must judge by
// what the CLIENT will do: a key the overlay states replaces that key, a key
// it omits is inherited, and a list replaces rather than merges.
func mergeOver(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if om, ok := v.(map[string]any); ok {
			if bm, ok2 := out[k].(map[string]any); ok2 {
				out[k] = mergeOver(bm, om)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// hclDoc reads an overlay entry as a document, through the same converter the
// client uses.
func hclDoc(src []byte, name string) (map[string]any, error) {
	y := src
	if strings.HasSuffix(name, ".hcl") {
		var err error
		if y, err = bottle.HCLToYAML(src, name); err != nil {
			return nil, err
		}
	}
	var m map[string]any
	if err := yaml.Unmarshal(y, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return m, nil
}
