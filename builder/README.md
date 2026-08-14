# The sovereign builder

A `FROM scratch` image that compiles pantry recipes with **only pkgx packages** —
no debian, no apt, no system libc. `Containerfile` builds it; the header there
shows how to run it.

Measured on a six-recipe panel (nine with their closure), built inside this image
in sovereign mode. `lz4.org` (linux/aarch64) is the reference case: the build
completes inside the image and the bottle it produces carries

```
PT_INTERP  /pkgx/gnu.org/glibc/v2.44.0/lib/glibc-2.44/ld-linux-aarch64.so.1
NEEDED     libc.so.6            RPATH  $ORIGIN/../lib
references to /usr/lib, /lib/aarch64-linux-gnu, debian:  0
```

Pair it with `bk factory --libc pkgx`, which points the compiler at the pkgx
glibc: the environment and the output are then both free of the distribution.

## What a scratch image forced us to fix

Each of these was found by running the thing, not by reading code:

| symptom | fix |
|---|---|
| `context deadline exceeded` on a 969 MiB bottle | a 5-minute *whole-request* timeout could never fetch one — bounded a stall instead (go-pkgx/bottle#12) |
| `PROTOCOL_ERROR` mid-blob from ghcr | HTTP/2 forced by mistake; bottle pulls are one long body (bottle#13) |
| `expected 1762228910, got 1247805440: unexpected EOF` | a cut transfer lost 1.19 GiB — resume with `Range`, verify size+digest (bottle#14) |
| `x509: certificate signed by unknown authority` | no system trust store on scratch — bk installs bottle's embedded bundle, including on go-git's own client (bottle#15, bk#26) |
| `eval "$(pkgx +deps)"` produced nothing | our pkgx had no env-printing mode; it also now exports `lib64/pkgconfig`, which upstream omits (pkgx#1) |
| `mkdir: libc.so.6: cannot open shared object file` | the sanitised build env dropped an explicitly-set `LD_LIBRARY_PATH` (bk#27) |
| `make: /bin/sh: No such file or directory` | `pkgm install -s` now poses the loader **and** `/bin/sh` (pkgm#9) |
| `linux/limits.h` not found | glibc's headers need the kernel headers; mirrored `kernel.org/linux-headers` into our registry |
| `"mkdir": executable file not found in $PATH` | bk builds under a sanitised `PATH=/usr/bin:/bin:…`, so the stubs must go to `/usr`, not `/usr/local` — hence `install -s --prefix /usr` |
| `make: cmp: No such file or directory` | recipes' own test steps call `cmp`: added `gnu.org/diffutils` |
| `ls-remote …/openssl: context deadline exceeded` | github's ref advertisement for a big repository stalls under HTTP/2, like the big ghcr blobs did — bk now uses HTTP/1.1 (bk#28) |

Several toolchain packages (gawk, m4, bison, texinfo, autoconf, libtool) had no
**linux** bottle in our registry at all — only darwin ones. `bk factory
--mirror-from https://dist.pkgx.dev` filled them.

## A local pull-through cache

Rebuilding the image re-pulls llvm (~1.7 GiB) whenever the toolchain list
changes. Run a [zot](https://zotregistry.dev) in front of ghcr and point the
build at it:

```jsonc
// ~/.cache/pkgx-registry/config.json
{
  "distSpecVersion": "1.1.0",
  "storage": { "rootDirectory": "…/data", "dedupe": true },
  "http": { "address": "0.0.0.0", "port": "5111" },
  "extensions": { "sync": { "enable": true, "registries": [
    { "urls": ["https://ghcr.io"], "onDemand": true, "tlsVerify": true,
      "content": [ { "prefix": "/go-pkgx/packages/**" } ] }
  ] } }
}
```

```
pkgx zot serve ~/.cache/pkgx-registry/config.json &
docker build -f builder/Containerfile \
  --build-arg DIST=oci://http://host.docker.internal:5111/go-pkgx/packages \
  -t bk-builder builder
```

Measured: the same install takes **45.7 s** the first time and **2.2 s** after,
and **signature verification stays on** — the cache proxies the cosign referrer
alongside the bottle (checked: the referrer tag in the cache carries
`application/vnd.dev.cosign.artifact.sig.v1+json`).

llvm.org also gets its own `RUN` layer, so editing the rest of the toolchain
re-uses it rather than pulling it again.
