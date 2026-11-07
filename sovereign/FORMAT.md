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

⛔ The pkgx column describes **upstream's** recipes. Our own schema
(`zbk/schema/package.schema.json`, "derived from libpkgx's parser") is not
identical to it: it already declares `license` and `platforms`, which §2 and
§6 below correct. It still has no integrity field of any kind.

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

⛔ **This is not new ground, and the existing analysis is sharper than the
first draft of this section.** `zbk/fetch/fetch.go:160` already states the
problem and already mitigates it: "Exactly one recipe of 904 in the pantry
carries a `sha:`, and that one points at a `.sha256` served by the same host
as the tarball — it catches corruption, not substitution. For everything else
TLS is the whole of it, across 233 distinct upstream hosts." The mitigation is
`fetch.Pin`, trust on first use: the first fetch of a URL records its digest,
and a later fetch serving different bytes fails with `ErrSourcePinMismatch`.
As that comment puts it, it "does not make an upstream trustworthy. It makes a
CHANGE visible, which is the part nobody had."

What a recipe-pinned digest adds — the only thing it adds — is **the first
fetch**. Trust on first use cannot refuse a first answer, because it has
nothing to compare against: it records it. The zlib attempt above is exactly
that case. A first fetch returning a 404 page is a digest `fetch.Pin` would
have written down as the truth about zlib, and then defended against
correction. A digest put in the recipe by whoever read the release is the one
control that covers it.

Re-measured 2026-11-07 against the current trees: **1 of 1932** upstream
recipes carries a `sha:`, and **0 of our 213** carry any integrity field —
the schema's `distributableObject` offers only `url`, `ref` and
`strip-components`.

**2. The licence field exists and nothing fills it.** Corrected after reading
the authoritative schema: `license` is already there
(`zbk/schema/package.schema.json:98`, a `stringOrArray`). The gap is not a
missing field, it is a field no recipe is required to carry and almost none
does — **0 of our 213** overlay recipes and **1 of upstream's 1932**. A field
that is optional and unused is indistinguishable from an absent one, except
that it reads as though we had the information. We have just spent a day
establishing that an unstated licence is not an absent one; shipping bottles
with no record of their terms repeats upstream's mistake at our own expense.
So the change here is a *requirement*, not a field.

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

**6. The platform field exists and almost nothing states it.** Corrected, as
§2 was: `platforms` is in the schema already
(`zbk/schema/package.schema.json:83` — "darwin, linux, windows, or
`<os>/<arch>`"), and **7 of our 213** overlay recipes set it. Two things are
genuinely missing rather than unused: there is no way to *exclude* a couple
that a broader claim admits — Nixpkgs pairs `platforms` with `badPlatforms`
for exactly this, and recommends claiming everything and subtracting — and
nothing requires the claim, so "which nodes should build this" still has no
answer for 206 of 213 recipes without running the build.

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

⛔ **Corrected, and it inverts the risk.** The first draft said an unknown
field would be a parse error. It is the opposite:
`zbk/schema/package.schema.json` sets `"additionalProperties": true` at the
root and in every nested object, so a recipe may declare `sha256`,
`provenance` or anything else today and `zbk` will **accept it and silently
ignore it**. That is worse than refusing it. A recipe carrying a digest that
nothing checks reads, to anyone who opens it, as a verified source.

So the first step is not "add fields", it is "stop accepting fields nobody
reads". The order that keeps the tree buildable throughout:

1. Close the schema: `additionalProperties: false` at the root, with the
   present keys enumerated, so an unknown or misspelt key fails loudly. Do
   this first — every step below is only as good as this one.
2. `source.sha256` + `provenance`, and make `license` **required** rather
   than merely present. This is a migration, not a flag day: 0 of 213 recipes
   carry a licence and none carries a digest, so the requirement lands with a
   grace list that shrinks, and the gate reports its denominator.
3. `deps` slots, with the present `build.dependencies` reading as
   `deps.build`.
4. `platforms` required, plus an exclusion form, so a build can be dispatched
   without being run.
5. `identifiers` and `vulnerable`, joined to `advisories/`.
6. `patch` blocks and `outputs`.

Steps 1–4 are what the first bootstrap tranche needs; 5–6 can follow the
recipes.

Both front-ends have to move together: `zbk/pantry/hcl` converts HCL to YAML
through `bottle.HCLToYAML` and validates with the same schema, deliberately,
"because the CLIENT has to read these files too — the overlay is fetched at
install time". A field the factory honours and the installer ignores would be
two readings of one recipe.

## The first tranche

`seed/order.txt` holds 78 projects, and nothing else builds until they do.
That is the first tranche, in that order, starting at `lz4.org`,
`tukaani.org/xz` and `zlib.net`, which have no dependencies of their own.
