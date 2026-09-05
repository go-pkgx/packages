# packages

[![build](https://github.com/go-pkgx/packages/actions/workflows/build.yml/badge.svg)](https://github.com/go-pkgx/packages/actions/workflows/build.yml)

A pure-Go package factory. Builds [pkgx pantry](https://github.com/pkgxdev/pantry)
recipes with [`bk`](https://github.com/go-pkgx/bk) (the CGO-free re-implementation
of brewkit) and publishes **signed, attested packages** to
`ghcr.io/go-pkgx/packages`.

## Supply chain

Every package is pushed as an OCI artifact carrying, as **OCI referrers**:

- a **CycloneDX SBOM** ([`go-attest/sbom`](https://github.com/go-attest/sbom)),
- an in-toto **SLSA provenance** statement, and
- a **cosign-style + minisign signature** ([`go-attest/sign`](https://github.com/go-attest/sign)).

Signatures verify against the pinned public key
`RWQ+rmH+fXy2iYr+gReQAOQtYWtH0A7UlxcAa2hpr+txNBwGqtpFsR6L`. On install,
`bottle.VerifySignature` checks it; set **`PKGX_VERIFY=1`** to fail closed —
an unsigned or badly-signed package is refused rather than installed.

## How it runs

`.github/workflows/build.yml` runs a `linux/x86-64` + `linux/aarch64` matrix on our
own GARM/Incus pool (32 cores, an ephemeral container per job, tagged
`[self-hosted, incus, amd64]`; the `runner: github` dispatch input falls back to
`ubuntu-latest` when the pool is down, and otherwise jobs simply queue), and
`.github/workflows/darwin.yml` a `darwin/aarch64` + `darwin/x86-64` matrix on
GitHub's native macOS runners — `macos-14` for aarch64 and `macos-15-intel` for
x86-64, `max-parallel: 6` because macOS runner concurrency is scarce, with
`recipes.txt` chunked 50 projects at a time across both — both dispatched on demand (`workflow_dispatch`; the daily crons are
commented out since 2026-08-12 to leave the runners free for targeted jobs, and only
`index-audit.yml` still runs on a schedule) and both publishing signed packages to the same `ghcr.io/go-pkgx/packages` OCI registry via the
identical, platform-agnostic `bk factory` (which also writes a pkgx dist tree, uploaded
as a `dist-*` artifact for the Pages mirror). Auth to ghcr uses the workflow's **native
`GITHUB_TOKEN`** (`permissions.packages: write`) — no long-lived PAT to rotate. Each
run installs pkgx (bk sources its own toolchain via `pkgx +deps`) and bk, clones the
pantry, then runs `bk factory` — one pure-Go command (no bash, no curl/jq, no `git apply`), which:

- expands the requested projects to their **topologically-ordered runtime-dependency
  closure** (deps built before dependents), and
- **skips any `(project, version, platform)` already in ghcr**, so shared deps build
  once and the catalog is populated progressively.

Per-recipe failures are logged (`failures.txt`) but never fail the run. `recipes.txt`
holds 1900 candidate projects, **1576 of them published**; the front is the other 324.

## Working the front

What is left is not one problem. `bk depgaps` separates the two, ranks each by how
many recipes it blocks, and reads satisfiability through bottle's own semver so it
agrees with what pkgx will decide at install:

    bk depgaps --pantry pantry --overrides overrides --registry registry.json \
               --platform linux/x86-64

- A **version line** the registry does not carry. `python.org ~3.12` blocked 23
  recipes; upstream had it, so a mirror closed it.
- A **project** of which nothing at all is published. This half is usually the
  bigger one and was invisible until 2026-08-25: on linux/x86-64 it held 797
  blocked dependencies against the version lines' 333, and `rust-lang.org` alone
  accounted for 292 of them.

Both are usually a mirror away rather than a build. Check upstream carries the line
first — two mirrors and a glibc rebuild were once spent healing a bottle that never
existed — then:

    recipes="python.org@~3.12 rust-lang.org libgit2.org@~1.7"
    mirror_from=https://dist.pkgx.dev   max_versions=1   no_closure=1

One version per line is enough: a recipe wants *a* version in the line, and an
unbounded mirror has filled a runner's disk. A project may appear only **once** per
dispatch — the pin map keeps the last. Never run two dispatches of the same workflow
at once: four publishers doing a read-modify-write on one mutable index tag is what
silently lost `cmake.org 4.4.2 linux/amd64`.

Measured over 39 such waves on 2026-08-25: **4–8 minutes each**, against 45 minutes
to 4 hours for a batch of builds.

The blocked-dependency count now stands at **88 on linux/x86-64, 97 on
linux/aarch64, 112 on darwin/aarch64, 122 on darwin/x86-64** (2026-08-27). No
before-figure is quoted, and the ones that were here are gone: they came from a
crawl that read only the first page of ghcr's tag listing and so missed a third
of the registry. The ratio was not measurable; only these absolutes are.

## Build isolation

- **Phase A (shipped, in use).** Linux builds run inside a pinned `debian:stable-slim`
  container — a controlled glibc floor instead of the drifting runner host, with no
  host-tool leakage and reproducible output. macOS builds run directly on GitHub's
  ephemeral macOS runners (each a clean, throwaway VM), which are the isolation there.
- **Phase B (shipped).** Building against pkgx's *own* glibc toolchain, for packages
  self-contained enough to run `FROM scratch`: `bk factory --libc=pkgx` retargets the
  compiler at the `gnu.org/glibc` bottle — its crt objects, its libc, its dynamic
  linker — so the output owes nothing to the debian container. `bk builder` stages the
  sovereign rootfs itself (static Go binaries plus toolchain bottles from this signed
  registry, nothing else), and `builder/` here is what builds that image. Design note:
  [`bk/docs/from-scratch-toolchain.md`](https://github.com/go-pkgx/bk/blob/main/docs/from-scratch-toolchain.md).

`.github/workflows/sovereign.yml` is that claim as a job: stage the rootfs from
`builder/toolchain.txt`, enter it with `chroot`, and run `bk` through pkgx exactly
as `builder/Containerfile`'s `ENTRYPOINT` does — no distribution involved.

It exists because on the ordinary path what can be built is decided by the
runner's distribution, which was measured twice on our own pool:

- on **debian 12** every compile died at `env: … version GLIBC_2.38 not found`,
  because bk installs a *mirrored* coreutils linked against a newer glibc than
  the host's;
- on **debian 13** the first recipe through, `jonas.github.io/tig`, failed on a
  C23 diagnostic from the runner's GCC — not from the `llvm.org` bottle we ship
  and intend to compile with.

Both are the same defect in different clothes. The job's `force` input defaults to
1 on purpose: the point is to *compile*, and a build skipped because the bottle is
already published proves nothing.

## Install pkgm

To install from this registry you need `pkgm` (the pure-Go installer). One line:

```sh
# Linux / macOS
curl -fsSL https://go-pkgx.github.io/install.sh | sh -s -- pkgm v0.1.3
```

```powershell
# Windows (PowerShell)
$env:PKGM_VERSION='v0.1.3'; irm https://go-pkgx.github.io/install.ps1 | iex
```

or, for Go users, `go install github.com/go-pkgx/pkgm@latest`. The installer
grabs the static binary for your os/arch from that release and verifies it
against the release `SHA256SUMS`; then `pkgm install lz4.org` verifies each
bottle against this signed registry by default.

The version is named rather than resolved from `/releases/latest` when the line
runs: a registry whose install instructions change under the reader is a
registry whose bug reports cannot be reproduced. `sh -s -- pkgm latest` (or
`PKGM_VERSION=latest`) asks for the newest.

## Consuming

Packages are OCI artifacts, so any OCI client can pull them. On 2026-08-27:

    $ SUMMARY=1 go run ./catalog
    1579 projects, 41236 platform builds
      linux/aarch64      14883
      linux/x86-64       12966
      darwin/x86-64      6657
      darwin/aarch64     6176
      windows/x86-64     554
    recipes.txt: 1578 of 1900 published, 322 remaining

That is a moving target, and the command above is the point: it enumerates ghcr
anonymously, so re-run it rather than trusting the paste.
<https://go-pkgx.github.io/packages> browses the same data (its deploy is
dispatched, so the site can lag the registry).

Point the go-pkgx tools at the registry and verify against the pinned key:

    PKGX_DIST=oci://ghcr.io/go-pkgx/packages PKGX_VERIFY=1 pkgm install lz4.org

or pull a package directly:

    docker pull ghcr.io/go-pkgx/packages/lz4.org:1.10.0

## What CI watches between builds

Two workflows exist to catch the failures that do not announce themselves.

**`fromscratch.yml` — continuous proof that a published package runs with no
system at all.** For each of twelve witness tools (`lz4.org`, `openssl.org`,
`tukaani.org/xz`, `facebook.com/zstd`, `sqlite.org`, `rsync.samba.org`,
`perl.org`, `stedolan.github.io/jq`, `gnu.org/wget`, `curl.se`,
`git-scm.org`, …) `pkgm image` emits a `FROM scratch` Containerfile whose `RUN`
step uses pkgm *itself*, inside the image, to install the tool's whole runtime
closure over HTTPS from this registry and wire the pkgx loader. The job then
executes exactly that — the ENV and argv are read out of the Containerfile pkgm
emits, so the two cannot drift — in an **empty chroot** rather than a docker
image, because the Incus runner image carries neither docker nor podman and a
chroot proves the same thing with no image-layer machinery in between. A closure
that is not fully published yet makes the install fail and is reported as a
coverage gap (SKIP); a tool that installs but whose smoke run fails **fails the
job**.

**`index-audit.yml` — a gate on the one defect that hides.** Publishing a
version's OCI index is a read-modify-write on one mutable tag, and four
publishers race on it (two arches × `build.yml`, plus `darwin.yml`). When one
loses, the bottle is in the registry, valid and signed, and simply absent from
the index: `pkgx` says "platform not in index" and the package looks published
when it is not. 137 such entries had accumulated unnoticed, found by tripping
over one of them in a build. They are at 0, and this is the daily crawl
(04:00 UTC) that keeps them there. It is also why two dispatches of the same
publishing workflow must never run at once.

## Two channels — every platform, both

Every platform — linux, darwin **and** windows — publishes to **both** channels:

- the **signed OCI registry** `ghcr.io/go-pkgx/packages` (SBOM + provenance +
  signature, via `bk publish`), and
- the **GitHub Pages pkgx dist mirror** `https://go-pkgx.github.io/packages`.

The four build workflows (`build.yml`, `darwin.yml`, `windows.yml`,
`windows-rust.yml`) each publish their packages to the OCI registry *and* upload
their pkgx dist tree as a `dist-*` artifact. A single deployer,
`.github/workflows/pages.yml`, then unions the latest successful run of each build
workflow into one combined site and deploys it to Pages — so the dist mirror
carries all platforms side by side (`<project>/<os>/<arch>/…`).

## Windows

pkgx ships no Windows packages, so this repo cross-builds pantry projects to
Windows PE packages and publishes them, like every other platform, to **both** the
signed `ghcr.io/go-pkgx/packages` OCI registry and the
`https://go-pkgx.github.io/packages` Pages mirror.

- **`.github/workflows/windows.yml` (Go).** Go tools cross-build to Windows *for
  free* — `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build` yields a
  self-contained `.exe` with no libc and no DLLs — so a large slice of the pantry
  (**351 Go projects** in `windows/go-projects.txt`) becomes Windows packages with
  no per-recipe work. For each `slug|github-repo|bin` row it finds the newest tag,
  clones it, builds, lays the `.exe` out as a pkgx dist tree
  `<slug>/windows/x86-64/{versions.txt, v<ver>.tar.gz}` + `package.yml`, and
  `bk publish`es it (signed) to the OCI registry as `windows/x86-64`.
- **`.github/workflows/windows-rust.yml` (Rust).** Cross-builds the Rust projects
  in `windows/rust-projects.txt` with mingw-w64 (colocating the mingw runtime DLLs
  for self-containment), signs and publishes each to the OCI registry, and uploads
  its dist tree for the Pages mirror.
- **`.github/workflows/windows-proof.yml` (e2e proof).** Cross-builds a *real* tool
  (`sqlite3`, x86-64 + aarch64) with the same llvm-mingw toolchain the pantry ships,
  then on a `windows-latest` runner builds `pkgx.exe`, serves the dist locally, and
  **fetches + runs the package on real Windows** — the guarantee that a cross-built
  Windows package actually runs via pkgx.

Like `build.yml`, every one of these triggers on `workflow_dispatch` alone (no
`on: push`; their crons have been commented out since 2026-08-12, and
`index-audit.yml` is the only scheduled workflow left). The data files live under `windows/` (`go-projects.txt`,
`go-projects-all.txt`, `rust-projects.txt`, `rust-projects-all.txt`); add rows to
scale. Consume the Windows packages from either channel:

    PKGX_DIST=https://go-pkgx.github.io/packages pkgx syft.exe --version
    PKGX_DIST=oci://ghcr.io/go-pkgx/packages     pkgx syft.exe --version

## License

BSD-3-Clause © the go-pkgx authors.
