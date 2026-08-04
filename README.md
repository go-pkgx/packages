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

`.github/workflows/build.yml` runs a `linux/x86-64` + `linux/aarch64` matrix on a
**daily** schedule (`cron: "0 6 * * *"`, plus `workflow_dispatch`). Auth to ghcr
uses the workflow's **native `GITHUB_TOKEN`** (`permissions.packages: write`) — no
long-lived PAT to rotate. Each run installs pkgx (bk sources its own toolchain via
`pkgx +deps`) and bk, clones the pantry, then `factory.sh`:

- expands the requested projects to their **topologically-ordered runtime-dependency
  closure** (deps built before dependents), and
- **skips any `(project, version, platform)` already in ghcr**, so shared deps build
  once and the catalog is populated progressively.

Per-recipe failures are logged (`failures.txt`) but never fail the run. Grow
`recipes.txt` outward from dependency-free leaves toward the full pantry.

## Build isolation

- **Phase A (shipped, in use).** Builds run inside a pinned `debian:stable-slim`
  container — a controlled glibc floor instead of the drifting runner host, with no
  host-tool leakage and reproducible output.
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

## License

BSD-3-Clause © the go-pkgx authors.
