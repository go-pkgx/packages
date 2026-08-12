# `FROM scratch` image builder

Build a genuine `docker FROM scratch` Linux image whose **only** contents are
pkgx bottles pulled from `ghcr.io/go-pkgx/packages` — no debian, no distro, no
system libc. This is the consumer proof for Isolation Phase B: the bottles this
factory publishes are self-sufficient enough to run with nothing underneath them
but the kernel.

The whole flow is a **pure-Go** `bk` subcommand — [`bk fromscratch`](https://github.com/go-pkgx/bk)
(package `cmd/bk`). No interpreter and no shell glue: `bk` already has the OCI
bottle pull, pantry-recipe parsing and version resolution in Go, and reads ELF
`DT_NEEDED` natively via `debug/elf`. The only external command is `docker`
(the container builder itself). A static Go binary is itself deployable in a
`FROM scratch` image; an interpreted helper is not — so Go is the only fit for
from-scratch tooling.

## Usage

```sh
go install github.com/go-pkgx/bk/cmd/bk@main
git clone --depth 1 https://github.com/pkgxdev/pantry

# Resolve + pull the closure, build a scratch image, and smoke-run it. `{V}` in
# the entrypoint is replaced by the root project's resolved version dir. Args
# after `--` are passed to the container (its real exit code is bk's).
bk fromscratch -arch amd64 -tag myimg:lz4 \
    -entrypoint '/pkgx/lz4.org/{V}/bin/lz4' -run lz4.org -- --version

bk fromscratch -arch arm64 -tag myimg:perl \
    -entrypoint '/pkgx/perl.org/{V}/bin/perl' -run perl.org -- --version

# Just print the resolved proj:ver closure (diagnostic, no image):
bk fromscratch -arch arm64 -resolve-only sqlite.org
```

`-arch` is `amd64` or `arm64` (aliases `x86-64` / `aarch64`). `-pantry` defaults
to `./pantry`. External dependency: `docker` (plus a Go toolchain to install
`bk`). CGO is not required — `bk` is a static pure-Go binary.

## How it works

A compiled-C bottle binary (e.g. `lz4.org/.../bin/lz4`) as published carries two
things that a scratch image can't satisfy on its own:

1. **`PT_INTERP`** = an absolute path to the *build container's* ELF loader
   (`/lib64/ld-linux-x86-64.so.2` on amd64, `/lib/ld-linux-aarch64.so.1` on
   arm64) — absent from a scratch image, so the kernel can't even start it.
2. **`NEEDED libc.so.6`** (and `libssl`/… for multi-lib tools) — resolved by the
   loader's default search path, which finds nothing in a scratch image.

`bk fromscratch` reproduces pkgx's own runtime model inside the image:

1. **Closure-resolve**: parse each pantry recipe's top-level `dependencies:` (a
   key is a project iff it looks like a host `something.tld[/path]`; non-dotted
   keys like `linux` / `aarch64` are platform maps, descended into only for the
   target os/arch), transitively with a visited-set, `gnu.org/glibc` first.
2. **Version-select + pull**: for each project pick the newest version
   **published on ghcr with a `linux/<arch>` manifest** (the OCI index filter
   matches **both** `os == linux` **and** the arch — an arch-only filter also
   matches `darwin/<arch>` Mach-O manifests), pull the layer and lay it out at
   `/pkgx/<proj>/v<ver>`. If any closure member is not published for the arch it
   aborts (**exit 3**) instead of building a broken image.
3. **readelf-driven completion**: scan every laid-out ELF's `DT_NEEDED`
   sonames (pure Go, `debug/elf`) and pull the bottle that provides each
   unsatisfied one — the undeclared runtime libraries that recipe graphs omit.
   The canonical case is `libcrypt.so.1`: glibc 2.38+ removed libcrypt, so
   `perl` NEEDs it without declaring `github.com/besser82/libxcrypt`; the
   completion pulls it automatically. Iterates to a fixpoint.
4. **Loader symlink**: link glibc's loader to the arch-standard `PT_INTERP` path
   so unmodified bottles start.
5. **`LD_LIBRARY_PATH`**: auto-discover every dir in the closure that holds a
   shared object (glibc's `libc.so.6` lives in a nested `lib/glibc-<v>/`, so a
   plain `<prefix>/lib` assumption is wrong).
6. Write a `FROM scratch` Dockerfile and `docker build` it; with `-run`, also
   `docker run` the image as a smoke test (its real exit code is propagated).

## CI

[`.github/workflows/fromscratch.yml`](../.github/workflows/fromscratch.yml)
(`workflow_dispatch`) installs `bk`, clones the pantry, and runs
`bk fromscratch … -run` for a panel of witness tools (`lz4`, `openssl`, `xz`,
`zstd`, `sqlite`, `rsync`, `perl`, …). A closure not yet fully published is
reported as a coverage SKIP (exit 3); a tool that builds but fails its smoke
test FAILS the job (exit 1).
