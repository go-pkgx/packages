#!/usr/bin/env python3
"""Resolve a pantry project's runtime closure to published ghcr bottle specs.

Driven by env: PANTRY, ROOTPROJ, OARCH (amd64|arm64), PARCH (x86-64|aarch64).
Prints `proj:ver` lines (gnu.org/glibc first), newest published linux/<arch>
version per project. Exit 3 if any closure member has no linux/<arch> bottle.

Recipe dependency parsing is a minimal indentation walk of the top-level
`dependencies:` block (no PyYAML dependency): a key is a PROJECT iff its first
path segment looks like a host (`something.tld`); a non-dotted key (linux,
darwin, aarch64, x86-64, …) is a platform map, recursed into only when it
applies to the target os/arch. Only the KEYS matter here (versions are resolved
from what is actually published on ghcr, not from the recipe constraint).
"""
import json
import os
import re
import sys
import urllib.request

PANTRY = os.environ.get("PANTRY", "pantry")
ROOTPROJ = os.environ["ROOTPROJ"]
OARCH = os.environ["OARCH"]
PARCH = os.environ["PARCH"]

REG = "https://ghcr.io/v2/go-pkgx/packages"
GLIBC = "gnu.org/glibc"

# A dependency key is a project iff its first path segment (up to the first '/')
# contains a dot — i.e. a host like gnu.org, openssl.org, github.com/o/r. Bare
# words (linux, darwin, aarch64, x86-64) are platform maps.
HOST_RE = re.compile(r"^[^/\s]+\.[^/\s]+")
# Platform-map keys we descend into for a linux/<arch> closure. Anything else
# non-dotted (darwin, windows, the other arch) is skipped.
PLATFORM_OK = {"linux", PARCH}


def http_json(url):
    req = urllib.request.Request(url, headers={"Accept":
        "application/vnd.oci.image.index.v1+json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


def pull_token(proj):
    url = ("https://ghcr.io/token?service=ghcr.io&scope=repository:"
           "go-pkgx/packages/%s:pull" % proj)
    try:
        with urllib.request.urlopen(url, timeout=30) as r:
            return json.load(r).get("token", "")
    except Exception:
        return ""


def parse_deps(proj):
    """Return the list of runtime-dependency project names declared by proj that
    apply to the target linux/<arch>. Missing recipe -> [] (can't recurse)."""
    path = os.path.join(PANTRY, "projects", proj, "package.yml")
    try:
        with open(path, "r", encoding="utf-8") as f:
            lines = f.readlines()
    except OSError:
        return None  # signal: no recipe

    # Isolate the top-level `dependencies:` block (indent-0 key, until next
    # indent-0 line). `build.dependencies` etc. live under indented keys, so an
    # indent-0 scan naturally excludes them.
    block = []
    in_block = False
    for raw in lines:
        line = raw.rstrip("\n")
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        indent = len(line) - len(line.lstrip(" "))
        if indent == 0:
            in_block = (line.rstrip() == "dependencies:")
            continue
        if in_block:
            block.append(line)

    projects = []
    # stack of (indent, allowed) for enclosing platform maps.
    stack = []
    for line in block:
        indent = len(line) - len(line.lstrip(" "))
        while stack and stack[-1][0] >= indent:
            stack.pop()
        parent_allowed = stack[-1][1] if stack else True
        key = line.strip().split(":", 1)[0].strip().strip('"').strip("'")
        if not key:
            continue
        if HOST_RE.match(key):
            if parent_allowed:
                projects.append(key)
        else:
            allowed = parent_allowed and (key in PLATFORM_OK)
            stack.append((indent, allowed))
    return projects


def closure(root):
    seen = set()
    order = []

    def visit(proj):
        if proj in seen:
            return
        seen.add(proj)
        deps = parse_deps(proj)
        if deps is None:
            # No recipe: cannot recurse, but still require the bottle itself.
            sys.stderr.write("closure: no recipe for %s (leaf)\n" % proj)
            deps = []
        for d in deps:
            visit(d)
        order.append(proj)

    visit(root)
    return order


def version_key(tag):
    return tuple(int(x) for x in tag.split("."))


VER_RE = re.compile(r"^[0-9]+(\.[0-9]+)*$")


def newest_published(proj):
    """Newest published version of proj carrying a linux/<OARCH> manifest, or
    None."""
    tok = pull_token(proj)
    if not tok:
        return None
    req = urllib.request.Request("%s/%s/tags/list" % (REG, proj),
                                 headers={"Authorization": "Bearer %s" % tok})
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            tags = json.load(r).get("tags", []) or []
    except Exception:
        return None
    # Drop cosign signature/attestation tags (sha256-…) and non-version tags.
    cands = [t for t in tags if VER_RE.match(t)]
    cands.sort(key=version_key, reverse=True)
    for tag in cands:
        idx = http_index(proj, tag, tok)
        if idx is None:
            continue
        for m in idx.get("manifests", []):
            p = m.get("platform", {})
            if p.get("os") == "linux" and p.get("architecture") == OARCH:
                return tag
    return None


def http_index(proj, ref, tok):
    req = urllib.request.Request(
        "%s/%s/manifests/%s" % (REG, proj, ref),
        headers={"Authorization": "Bearer %s" % tok,
                 "Accept": "application/vnd.oci.image.index.v1+json"})
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.load(r)
    except Exception:
        return None


def main():
    order = closure(ROOTPROJ)
    # glibc always first; drop any recipe-declared glibc to avoid a duplicate.
    projects = [GLIBC] + [p for p in order if p != GLIBC]

    specs = []
    missing = []
    for proj in projects:
        ver = newest_published(proj)
        if ver is None:
            missing.append(proj)
        else:
            specs.append("%s:%s" % (proj, ver))

    if missing:
        sys.stderr.write(
            "closure: NOT published for linux/%s: %s\n" % (OARCH, ", ".join(missing)))
        sys.exit(3)

    sys.stdout.write("\n".join(specs) + "\n")


if __name__ == "__main__":
    main()
