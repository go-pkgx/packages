# The sovereign recipe format

This is the format the rebuilt pantry is written in. It is not a transcription
of anyone else's schema into HCL: the compatibility constraint is gone
(go-zpkg/packages#431), so each field below is here because a system we read
carries it and ours could not say it, or because something we have already
got wrong would have been caught by saying it.

Nothing is written against this format until it is agreed. 1932 recipes
written against a schema that then moves is 1932 edits.

## What the five systems carry

Read 2026-11-07, from primary sources, at the versions linked.

| | pkgx `package.yml` | Guix `package` | Nixpkgs | Spack | Homebrew |
|---|---|---|---|---|---|
| source location | `distributable.url` | `origin.uri` | `src` | `url`, `urls`, `list_url` | `url` |
| **source digest** | `sha:` — *a URL to a `.sha256` file* | `sha256` / `hash` (`content-hash`) | fixed-output hash on `src` | `version(…, sha256=)` | `sha256` |
| licence | — | `license` | `meta.license` | `license(…, checked_by=)` | `license` (required in core) |
| SBOM ids | — | — | `meta.identifiers` (CPE, purl) | — | — |
| known vulns | — | — | `meta.knownVulnerabilities` | `deprecated(reason="vuln", severity=)` | — |
| dependency kinds | `dependencies`, `build.dependencies`, `test.dependencies` | `native-inputs` / `inputs` / `propagated-inputs` | `nativeBuildInputs` / `buildInputs` / `depsBuildBuild` / `depsTargetTarget` … | `type=build\|link\|run\|test` | `depends_on … :build, :test` |
| platforms | only `env:` maps and `if:` on script lines | `supported-systems` | `meta.platforms`, `meta.badPlatforms` | `conflicts(…, when=)` | `on_macos`, `on_linux`, `on_arm` |
| outputs | one | `outputs` (default `("out")`) | `outputs`, `separateDebugInfo` | — | — |
| patches | shell: `run: patch -p1 <props/…diff` | `origin.patches`, `patch-flags` | `patches` | `patch(…, sha256=, level=, when=, fixes=)` | `patch do … sha256` |
| tests | `test.script` | `#:tests?` | `checkPhase`, `doCheck` | test methods | `test do` |
| version discovery | `versions.github`, `versions.strip` | — | — | `list_url`, `spack checksum` | inferred from `url`, `livecheck` |

Sources: pkgx — `pkgxdev/pantry@4eb778f`, read from the recipes themselves
(`projects/openssl.org/package.yml`, `projects/zlib.net/package.yml`), the
repository documenting no field reference. Guix — `doc/guix.texi` at
`git.savannah.gnu.org/git/guix.git`, `@deftp {Data Type} package` (l. 8613)
and `origin` (l. 9009). Nixpkgs — the stdenv `meta` and `stdenv` chapters.
Spack — the packaging guide. Homebrew — the Formula Cookbook.

## The nine things our format could not say

**1. The source digest is fetched, not pinned.** pkgx writes
`sha: ${{url}}.sha256` — the checksum is a URL *at the same origin as the
tarball*, so whoever can change one can change the other. The other four all
pin the digest in the recipe. Our own HCL recipes are worse still:
`windows/recipes/zlib.net/package.hcl` carries no integrity field at all. A
build whose inputs are not pinned cannot have a Merkle identity that means
anything, which is the whole point of `Concrete.Hash()`.

This is not hypothetical, and writing this document demonstrated it. Fetching
the example's own source, twice, on 2026-11-07:

```console
$ curl -sSLo zlib.tar.gz https://zlib.net/zlib-1.3.1.tar.gz
$ file zlib.tar.gz
zlib.tar.gz: HTML document text, ASCII text          # 355 bytes, "404 Not Found"

$ curl -sSLo zlib2.tar.gz https://github.com/madler/zlib/releases/download/v1.3.1/zlib-1.3.1.tar.gz
$ file zlib2.tar.gz
zlib2.tar.gz: HTML document text, ASCII text         # 1812 bytes, "Forbidden · GitHub"
```

Two locations, two HTML error pages, neither of them zlib. A recipe carrying a
pinned digest refuses both. A recipe that fetches `${url}.sha256` at build
time asks the same servers what the right answer is, and a scheme that simply
hashes what arrives records the digest of an error page as the source of
truth. The digest in the example below is therefore left as a placeholder
rather than written from memory: an unverified digest in the document that
argues for verified digests would be the same mistake in miniature.

**2. No licence.** Nothing in our recipes records what the packaged project is
licensed under. We have just spent a day establishing that an unstated licence
is not an absent one; shipping 1932 bottles with no record of their terms
repeats upstream's mistake at our own expense.

**3. No SBOM identifiers.** Only Nixpkgs carries them —
`meta.identifiers`, "Software identifiers such as CPE and Package URL, used
for SBOMs and vulnerability lookup". Without a CPE or purl, matching an
advisory to a package is matching on a name we invented, and
`filippo.io/age` is not what NVD calls it.

**4. Vulnerability state is out of band.** We keep `advisories/` in a separate
directory. Nixpkgs puts `knownVulnerabilities` on the package and refuses to
install it; Spack's `deprecated(reason="vuln")` does the same. Ours is better
than having nothing, but a reader of a recipe cannot see that the thing is
known-broken.

**5. Dependency kinds are too coarse to cross-compile.** We have `build` and
runtime, flat. Guix's manual is explicit that the `native-inputs` /`inputs`
distinction "is necessary when considering cross-compilation": native inputs
are built for the **build** machine, inputs for the **target**. We have
already paid for not having this. `windows-c.yml` records that bk
deliberately does not inject `llvm.org` for a Windows cross build and the
recipe must name `llvm.org/mingw-w64` itself, and that a recipe which forgets
gets "C compiler cannot create executables" rather than a missing-dependency
error. That is a build-slot confusion with no vocabulary to express it. It is
also the thing standing between us and dispatching builds to the right os/arch
node of a heterogeneous cluster.

**6. No declarative platform support.** Guix has `supported-systems`
("architecture-kernel, for example `x86_64-linux`"); Nixpkgs has `platforms`
and `badPlatforms`. We state platform support only as a side effect of `env:`
maps, so nothing can answer "which nodes should build this" without running
the build.

**7. Patches are shell, not data.** pkgx writes
`run: patch -p1 <props/x509_def.c.diff` with `if: <3.4.0`. Spack's
`patch(…, sha256=, level=, when=, fixes=)` carries the digest *and what the
patch fixes*. A patch that names its CVE is the link between `advisories/` and
the thing that closes it.

**8. One output.** Guix defaults to `("out")` and splits; Nixpkgs has
`separateDebugInfo`. On a shared NFS tree, shipping debug info to every node
that only wants the binary is paid for on every node.

**9. No provenance for the recipe itself.** None of the five records this, and
we need it: our build instructions must be derived from each project's own
build system, under that project's licence, and the recipe should say which
files that was. It is the difference between a recipe we can defend and one we
merely assert.

## The format

```hcl
# zlib.net — the reference deflate implementation.

summary  = "Lossless compression library"
homepage = "https://zlib.net"

# SPDX expression. Required: a recipe with no licence does not build.
license = "Zlib"

# What the outside world calls this, so an advisory can find it.
identifiers {
  cpe  = "cpe:2.3:a:zlib:zlib"
  purl = "pkg:generic/zlib"
}

# Which os/arch couples this recipe claims. A build node is chosen from this
# without running anything; `!` excludes a couple the rest of a glob admits.
platforms = ["linux/*", "darwin/*", "windows/x86-64", "wasip1/wasm"]

# Outputs. "out" always exists; the rest are opt-in and separately addressed.
outputs = ["out", "debug"]

source {
  url = "https://zlib.net/zlib-{{version.raw}}.tar.gz"
  # ⛔ PINNED, not fetched. A digest resolved at build time from the same
  # host as the archive is not an integrity check. (Left as a placeholder
  # here rather than written from memory — see §1.)
  sha256           = "<the archive's digest, recorded when it was first fetched>"
  strip-components = 1

  # Further locations for the SAME bytes; the digest above governs all of them.
  mirrors = ["https://github.com/madler/zlib/releases/download/v{{version.raw}}/zlib-{{version.raw}}.tar.gz"]
}

versions {
  github = "madler/zlib"
  strip  = ["/^v/"]
}

provides = ["bin/minigzip"]

# ⛔ DEPENDENCIES ARE SLOTTED, as Guix and Spack slot them. `build` runs on the
# machine doing the building; `host` is linked into the artefact and must be
# built for the target; `run` is needed when the bottle is used. A cross build
# resolves `build` natively and `host` for the target — the distinction that
# `windows-c.yml` has no way to state today.
deps {
  build = { "gnu.org/make" = "*" }
  host  = {}
  run   = {}
  test  = {}
}

# A patch is data: a digest, where it applies, and what it closes.
patch "undefined-version" {
  file   = "patches/undefined-version.diff"
  sha256 = "…"
  level  = 1
  when   = "<1.3.1"
  fixes  = []            # CVE ids this patch closes; joins advisories/
}

build {
  script = [
    "./configure --prefix=\"{{ prefix }}\"",
    "make --jobs {{ hw.concurrency }} install",
  ]
  env {
    linux = { CFLAGS = "-fPIC" }
  }
}

test {
  script = ["test -x \"{{ prefix }}/bin/minigzip\""]
}

# Known to be broken and not yet fixed. Non-empty refuses the install, as
# Nixpkgs refuses a package with meta.knownVulnerabilities.
vulnerable = []

# ⛔ WHERE THIS BUILD CAME FROM. Our recipes are derived from each project's
# OWN build system, under that project's licence — never transcribed from
# another distribution's recipe. This records what was read, so the claim can
# be checked instead of trusted.
provenance {
  read = ["configure", "README"]
  note = "./configure then make install, as the project's README documents"
}
```

## What it costs

`zbk` parses the current schema; every field above that it does not know is a
parse error, so the format lands in `zbk` before any recipe uses it. The order
that keeps the tree buildable throughout:

1. `source.sha256` + `license` + `provenance` — refuse a recipe without them.
2. `deps` slots, with the present `build.dependencies` reading as `deps.build`.
3. `platforms`, so a build can be dispatched without being run.
4. `identifiers` and `vulnerable`, joined to `advisories/`.
5. `patch` blocks and `outputs`.

Steps 1–3 are what the first bootstrap tranche needs; 4–5 can follow the
recipes.

## The first tranche

`seed/order.txt` holds 78 projects, and nothing else builds until they do.
That is the first tranche, in that order, starting at `lz4.org`,
`tukaani.org/xz` and `zlib.net`, which have no dependencies of their own.
