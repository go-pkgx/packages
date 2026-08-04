# packages

[![build](https://github.com/go-pkgx/packages/actions/workflows/build.yml/badge.svg)](https://github.com/go-pkgx/packages/actions/workflows/build.yml)

A pure-Go package factory. Builds [pkgx pantry](https://github.com/pkgxdev/pantry)
recipes with [`bk`](https://github.com/go-pkgx/bk) (the CGO-free re-implementation
of brewkit) and publishes **signed, attested packages** to
`ghcr.io/go-pkgx/packages`.

## Supply chain

Every package is pushed as an OCI artifact carrying, as **OCI referrers**:

- a **CycloneDX SBOM** ([`go-pkgx/sbom`](https://github.com/go-pkgx/sbom)),
- an in-toto **SLSA provenance** statement, and
- a **cosign-style + minisign signature** ([`go-pkgx/sign`](https://github.com/go-pkgx/sign)).

Signatures verify against the pinned public key
`RWQ+rmH+fXy2iYr+gReQAOQtYWtH0A7UlxcAa2hpr+txNBwGqtpFsR6L`. On install,
`bottle.VerifySignature` checks it; set **`PKGX_VERIFY=1`** to fail closed —
an unsigned or badly-signed package is refused rather than installed.

## How it runs

`.github/workflows/build.yml` runs a `linux/x86-64` + `linux/aarch64` matrix, and
`.github/workflows/darwin.yml` a `darwin/aarch64` + `darwin/x86-64` matrix on native
macOS runners — both on a **daily** schedule (`workflow_dispatch` + cron) and both
publishing signed packages to the same `ghcr.io/go-pkgx/packages` OCI registry via the
identical, platform-agnostic `factory.sh` (which also writes a pkgx dist tree, uploaded
as a `dist-*` artifact for the Pages mirror). Auth to ghcr uses the workflow's **native
`GITHUB_TOKEN`** (`permissions.packages: write`) — no long-lived PAT to rotate. Each
run installs pkgx (bk sources its own toolchain via `pkgx +deps`) and bk, clones the
pantry, then `factory.sh`:

- expands the requested projects to their **topologically-ordered runtime-dependency
  closure** (deps built before dependents), and
- **skips any `(project, version, platform)` already in ghcr**, so shared deps build
  once and the catalog is populated progressively.

Per-recipe failures are logged (`failures.txt`) but never fail the run. Grow
`recipes.txt` outward from dependency-free leaves toward the full pantry.

## Build isolation

- **Phase A (shipped, in use).** Linux builds run inside a pinned `debian:stable-slim`
  container — a controlled glibc floor instead of the drifting runner host, with no
  host-tool leakage and reproducible output. macOS builds run directly on GitHub's
  ephemeral macOS runners (each a clean, throwaway VM), which are the isolation there.
- **Phase B (experimental / proven feasible).** Building against pkgx's *own* glibc
  toolchain for truly self-contained `FROM scratch` packages. It is a `bk` change on
  branch `feat/pkgx-glibc-toolchain`, off by default (`BK_PKGX_LIBC=1`); design note:
  [`bk/docs/from-scratch-toolchain.md`](https://github.com/go-pkgx/bk/blob/feat/pkgx-glibc-toolchain/docs/from-scratch-toolchain.md).

## Consuming

Packages are OCI artifacts, so any OCI client can pull them; the catalog grows daily
via the cron, so treat the published set as a moving target. Currently live:
`zlib.net`, `tukaani.org/xz`, `lz4.org`, `gnu.org/tar`, `sourceware.org/bzip2`.

Point the go-pkgx tools at the registry and verify against the pinned key:

    PKGX_DIST=oci://ghcr.io/go-pkgx/packages PKGX_VERIFY=1 pkgm install lz4.org

or pull a package directly:

    docker pull ghcr.io/go-pkgx/packages/lz4.org:1.10.0

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

Like `build.yml`, every workflow triggers on `workflow_dispatch` + a schedule (no
`on: push`). The data files live under `windows/` (`go-projects.txt`,
`go-projects-all.txt`, `rust-projects.txt`, `rust-projects-all.txt`); add rows to
scale. Consume the Windows packages from either channel:

    PKGX_DIST=https://go-pkgx.github.io/packages pkgx syft.exe --version
    PKGX_DIST=oci://ghcr.io/go-pkgx/packages     pkgx syft.exe --version

## License

BSD-3-Clause © the go-pkgx authors.
