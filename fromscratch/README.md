# `FROM scratch` image builders

Build a genuine `docker FROM scratch` Linux image whose **only** contents are
pkgx bottles pulled from `ghcr.io/go-pkgx/packages` — no debian, no distro, no
system libc. This is the consumer proof for Isolation Phase B: the bottles this
factory publishes are self-sufficient enough to run with nothing underneath them
but the kernel.

## Usage

```sh
# Explicit bottle list (first MUST be gnu.org/glibc):
./mkscratch.sh amd64 myimg:lz4 /pkgx/lz4.org/v1.10.0/bin/lz4 \
    gnu.org/glibc:2.44 lz4.org:1.10.0

# Auto-resolve the full runtime closure to newest published bottles.
# {V} in the entrypoint is replaced by the root project's resolved version dir.
# (run from the repo root; pantry defaults to ./pantry, override with PANTRY=)
git clone --depth 1 https://github.com/pkgxdev/pantry
./fromscratch/mkclosure.sh amd64 myimg:lz4 '/pkgx/lz4.org/{V}/bin/lz4' lz4.org
./fromscratch/mkclosure.sh amd64 myimg:openssl '/pkgx/openssl.org/{V}/bin/openssl' openssl.org

docker run --rm --platform linux/amd64 myimg:lz4 --version
```

`<arch>` is `amd64` or `arm64` (aliases `x86-64` / `aarch64`). Dependencies:
`curl`, `python3`, `tar`, `docker`. The scripts are bash-3.2 portable.

## How it works

A compiled-C bottle binary (e.g. `lz4.org/.../bin/lz4`) as published carries two
things that a scratch image can't satisfy on its own:

1. **`PT_INTERP`** = an absolute path to the *build container's* ELF loader
   (`/lib64/ld-linux-x86-64.so.2` on amd64, `/lib/ld-linux-aarch64.so.1` on
   arm64) — absent from a scratch image, so the kernel can't even start it.
2. **`NEEDED libc.so.6`** (and `libssl`/… for multi-lib tools) — resolved by the
   loader's default search path, which finds nothing in a scratch image.

`mkscratch.sh` reproduces pkgx's own runtime model inside the image:

1. **Pull** each bottle layer from ghcr and lay it out at `/pkgx/<proj>/v<ver>`.
   The ghcr filter matches **both** `os == linux` **and** the arch — an arch-only
   filter also matches `darwin/<arch>` Mach-O manifests (a silent wrong-pull).
2. **Loader symlink**: link glibc's loader to the arch-standard `PT_INTERP` path
   so unmodified bottles start.
3. **`LD_LIBRARY_PATH`**: auto-discover every dir in the closure that holds a
   shared object (glibc's `libc.so.6` lives in a nested `lib/glibc-<v>/`, so a
   plain `<prefix>/lib` assumption is wrong). Bottles built after
   [bk#3](https://github.com/go-pkgx/bk) self-locate their own libs via a
   `$ORIGIN/../lib` RUNPATH, so in practice only glibc must be found this way —
   listing every lib dir keeps older, slot-less bottles working too.

`mkclosure.sh` computes the runtime closure automatically: it parses each pantry
recipe's top-level `dependencies:` (a key is a project iff it looks like a host
`something.tld[/path]`; non-dotted keys like `linux` / `aarch64` are platform
maps, descended into only for the target os/arch), transitively with a
visited-set, always adds `gnu.org/glibc`, and picks each project's newest version
**published on ghcr with a `linux/<arch>` manifest**. If any closure member is
not published for the arch it aborts (**exit 3**) instead of building a broken
image.

## CI

[`.github/workflows/fromscratch.yml`](../.github/workflows/fromscratch.yml)
(daily + `workflow_dispatch`) builds scratch images for the witness tools
`lz4.org` and `openssl.org` and smoke-tests them (`--version` plus a real
operation — an lz4 compress/decompress round-trip and an openssl `dgst -sha256`
checked against the runner's own hash). A failed smoke test fails the job.
