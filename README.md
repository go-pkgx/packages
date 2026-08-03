# factory

[![build](https://github.com/go-pkgx/factory/actions/workflows/build.yml/badge.svg)](https://github.com/go-pkgx/factory/actions/workflows/build.yml)

Builds [pkgx pantry](https://github.com/pkgxdev/pantry) recipes with
[`bk`](https://github.com/go-pkgx/bk) (the pure-Go brewkit) and publishes
**signed, attested bottles** to `ghcr.io/go-pkgx/bottles`.

Each bottle is pushed as an OCI artifact with a **CycloneDX SBOM**, an in-toto
**SLSA provenance** statement, and a **cosign-style signature** (verified against
the pinned key by `bottle.VerifySignature` / `PKGX_VERIFY`) as OCI referrers.

## How it runs

`.github/workflows/build.yml` runs a `linux/x86-64` + `linux/aarch64` matrix:
installs pkgx (bk sources its toolchain via `pkgx +deps`) and bk, clones the
pantry, then `factory.sh` builds each project in `recipes.txt` and
`bk publish --sign`es it to ghcr. The pantry is built **progressively** — start
with dependency-free leaves, grow `recipes.txt` outward. Per-recipe failures are
logged (not fatal); trigger via *workflow_dispatch*, optionally overriding the
recipe list.

## Consuming

    PKGX_DIST=oci://ghcr.io/go-pkgx/bottles PKGX_VERIFY=1 pkgm install lz4.org

## License

BSD-3-Clause © the factory authors.
