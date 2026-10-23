# packages

[![build](https://github.com/go-pkgx/packages/actions/workflows/build.yml/badge.svg)](https://github.com/go-pkgx/packages/actions/workflows/build.yml)

A pure-Go package factory. Builds [pkgx pantry](https://github.com/pkgxdev/pantry)
recipes with [`bk`](https://github.com/go-pkgx/bk) (the CGO-free re-implementation
of brewkit) and publishes **signed, attested packages** to
`ghcr.io/go-pkgx/packages`.

## Supply chain

Every package is pushed as an OCI artifact carrying, as **OCI referrers**:

- a **CycloneDX SBOM** ([`go-attest/sbom`](https://github.com/go-attest/sbom)),
- an in-toto **SLSA provenance** statement,
- an **OpenVEX document**, where this repository holds advisory records for the
  project, and
- a **cosign-style + minisign signature** ([`go-attest/sign`](https://github.com/go-attest/sign)).

Signatures verify against the pinned public key
`RWQ+rmH+fXy2iYr+gReQAOQtYWtH0A7UlxcAa2hpr+txNBwGqtpFsR6L`. On install,
`bottle.VerifySignature` checks it; set **`PKGX_VERIFY=1`** to fail closed —
an unsigned or badly-signed package is refused rather than installed.

That covers what leaves. What went *in* was recorded by nothing until
[`bk`#112](https://github.com/go-pkgx/bk/issues/112): of 1858 recipes with a
distributable URL, exactly one declared a checksum and `bk` verified none, so a
source tarball that changed upstream produced a different bottle, correctly
signed, attesting a URL rather than the bytes that came back from it. The
provenance now carries the source's SHA-256 (or a git checkout's commit) as a
SLSA `resolvedDependency`, and where a recipe declares a `sha:` URL — the form
the pantry format already had and nothing read — a mismatch fails the build
instead of falling through to the next mirror.

### `advisories/` — why a package was rebuilt

A rebuild could not say why it existed. `bottle` names one (`3.5.8_1`, the same
upstream source built again) and nothing in the artefact said what it fixed.

`advisories/<project>/advisories.yaml` holds that, one file per project, as an
ordered list of **timestamped events** — the shape Wolfi uses, because a single
`status:` field cannot be superseded. **Every publishing lane passes it**, so a
bottle built on any platform carries the same answer:

| lane | how |
|---|---|
| `build.yml` (linux) | `bk factory --advisories advisories` |
| `build.yml` sovereign chunk | `SOVEREIGN_ADVISORIES`, bound in at `/advisories` |
| `sovereign.yml` | the same variable |
| `darwin.yml` | `bk factory --advisories advisories` |
| `windows.yml`, `windows-rust.yml` | `bk publish --advisories "$GITHUB_WORKSPACE/advisories"` |

The sovereign lane needs a **mount**, not a path: a workspace path means
nothing inside the rootfs, and `--advisories advisories` without the bind would
make `bk` refuse with "no advisories.yaml under advisories" — which reads as
"the tree is empty" and means "the tree is on the other side of a namespace".

`bk factory` validates the whole tree **once, before the first build**, because
a record that does not parse stops a *publish* — and the fortieth bottle of a
fifty-project chunk is six hours into the run. The Windows lanes' publish is
best-effort by design, so there a refusal lands in the non-fatal branch; the net
under that is this repository's own `bk advisories` check on every pull request.

`bk publish --advisories advisories`
turns it into the OpenVEX referrer above, and `bk advisories` validates the
tree on every pull request.

The first record is a worked example of why the file exists rather than a
re-derivation each time. `bk vulnfetch` reported `MAL-2026-3020`, *"Malicious
code in @bitwarden/cli (npm)"*, against the npm identity `bitwarden.com`'s
recipe states. Three measurements settle it:

| | |
|---|---|
| which version is malicious | **exactly `2026.4.0`**, from the advisory's own `affected.versions` |
| what we have published | `ghcr.io/go-pkgx/packages/bitwarden.com` lists one version tag: `1.22.1` |
| what a rebuild would pull | npm's `dist-tags.latest` is `2026.9.1`, and the `2026.4.0` tarball answers **404** — withdrawn |

So no published bottle contains it and no rebuild can reach it:
`false-positive-determination`, with the OpenVEX justification
`component_not_present`. The sweep will return that finding again on every run;
the record is what stops a decided question from being re-asked.

### Every platform we publish, which was two of six

⛔ The daily lane had its platform list **written into the workflow**, and the
list said `linux/x86-64` and `darwin/aarch64`. The registry carries six.
Measured 2026-10-20 with `bk vulnfetch` against the published catalogues, the
four nobody had ever asked about:

| platform | asked | unanswerable | not asked | Σ | findings |
|---|---|---|---|---|---|
| `linux/aarch64` | 132 | 1104 | 437 | 1673 | **0** |
| `darwin/x86-64` | 137 | 1074 | 462 | 1673 | **0** |
| `windows/x86-64` | 94 | 461 | 1118 | 1673 | **0** |
| `linux/s390x` | 1 | 28 | 1644 | 1673 | **0** |

All four clean, so this was a gap in **coverage and not an exposure** — which
is the only reason it could be closed by adding steps rather than by
rebuilding anything. The three buckets add to **1673** on every one of them,
the same arithmetic the two original platforms carry: none of them is a silent
drop. `linux/aarch64` is the largest platform here by bottle count, and
`linux/s390x` has 39 bottles — a scan that skips the small ones is a scan
whose coverage depends on how interesting they looked.

⛔ **And the list is no longer written down**, which was the actual defect:
adding the four by hand closed that day's gap and left the mechanism that
caused it in place. The index has always known — `pkgs catalog update
--platform linux/nonsense` is refused with *"platform not in index"* — and
since go-pkgx/pkgx#119 shipped, `pkgs catalog platforms` asks it:

```console
$ pkgs catalog platforms
darwin/aarch64
darwin/x86-64
linux/aarch64
linux/s390x
linux/x86-64
windows/x86-64
```

`./vulnsweep` fetches and scans every platform that names, so a **seventh is
covered the day it is published**, by nobody's decision. It is one step where
there were six, so the per-platform naming six steps gave for free has to be
earned: it prints a line per platform, **keeps going after a red one** (a
sweep that stops at the first reports one problem and hides the rest), and
names in its exit which platforms need attention. ⛔ And it refuses to sweep
nothing — an empty list, a list that is only noise, and a list it could not
obtain are each exit 2, never the clean zero this lane would otherwise print
on the day the registry stops answering.

## Trees upstream does not have

Most of what is here is a pkgx pantry recipe built differently. These are
projects the pantry does not carry at all, written for this factory — the HPC
and GPU floor an MPI actually needs, which upstream stops just above:

| project | | |
|---|---|---|
| `openucx.org` | 1.22.0 | the transport OpenMPI, MPICH and OpenSHMEM all reach shared memory and RDMA through. Built `--with-verbs`, so `libuct_ib.so`, `libuct_ib_mlx5.so` and `libuct_ib_efa.so` are there rather than TCP alone |
| `github.com/linux-rdma/rdma-core` | 65.0 | `libibverbs` — what makes the previous line more than a configure flag |
| `github.com/ROCm/ROCR-Runtime` | 7.2.4 | the HSA runtime an AMD GPU is driven through, **built from NCSA source**. A machine that can reach a git tag rebuilds it with no vendor in the loop |
| `nvidia.com/cuda-cudart` | 13.3.1 | the CUDA runtime, from NVIDIA's own redistributable set, verified against the sha256 in their manifest. `linux-x86_64` and `linux-sbsa` — server ARM, not Tegra |
| `kernel.org/linux` | 6.19.14 | the microVM kernel, now with `INFINIBAND_USER_ACCESS`, `RDMA_RXE`, `RDMA_SIW`, `MLX5_*`, `VFIO_*`, `TRANSPARENT_HUGEPAGE` and `PCI_P2PDMA` — the devices `libibverbs` opens, and GPUDirect |
| `github.com/containers/crun` | 1.29.1 | the runtime that closes the microVM boot loop |

The difference between the two GPU rows is worth stating: ROCm is source we
compile, CUDA is an archive we fetch under a licence that names which files may
travel. Sovereignty is not the same thing on the two sides, and the recipes say
so where it matters.

Nothing here needs a GPU or a fabric to *build*. What a build machine can
honestly check is that a runtime links, loads and answers — `hsa_init()`
returning "out of resources" with no `/dev/kfd` is a pass — and that a transport
module was compiled at all, since a silently declined `--with-verbs` looks
exactly like a missing `libuct_ib.so`.

These are packages, not a scheduler integration — but the piece that usually
follows is already in `pkgx`: **`pkge`**, a module front-end shaped like Lmod's,
over pkgx's own resolution, with no Lua interpreter and no modulefile tree.

    eval "$(pkgx env init)"
    pkge load openucx.org
    pkge save mpi-stack          # and `pkge restore mpi-stack` later

On a cluster that already runs Lmod nothing has to change — `pkgx --modulefile
+openucx.org` publishes a modulefile the site's own Lmod loads, so conflicts,
hierarchies and `spider` keep working. Where there is no Lmod,
`pkgx env init --module` defines `module` and `ml`; where there is one it
**refuses** to, because two implementations answering one command is how a
support ticket becomes unanswerable. See
[Environments and HPC](https://go-pkgx.github.io/docs/environments/).

### Both halves, or neither

A project upstream does not carry lives in two places that must agree:
`overrides/<name>-new.patch`, applied to a fresh pantry clone, is what **`bk`
builds** from; [`pantry-overlay`](https://github.com/go-pkgx/pantry-overlay) is
what a consumer **resolves** over HTTP — `bottle`'s `fetchRecipe` tries the
overlay, then upstream, and nothing else.

Four of these projects were published, signed and unreachable to every consumer
because only the build half existed. Nothing said so: the bottles were in the
registry and only a *recipe* fetch would have failed. `go run ./overlaycheck`
now fails a pull request when the two disagree, and found a fifth on its first
run.

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

Per-recipe failures are logged (`failures.txt`) but never fail the run.

Measured 2026-10-04 with `go run ./catalog`, and with the two sets named
because the previous figure conflated them:

| | |
|---|---|
| `recipes.txt` roots | 1902 |
| …of those, published | **1508** — the front is the other 394 |
| projects in the registry | 1585 |
| …published but NOT a root | 77 — dependencies the closure pulled in, which is by design |

A registry count is dated to the HOUR, not the day: the same probe reported 25
of 25 s390x toolchain roots unresolvable at 11:45 and 21 of 25 at 17:03 the
same afternoon, because another session was publishing while it ran.

## IBM Z, and why its runner is ours to build

`build.yml` carries a third linux lane, `linux/s390x`, tagged
`[self-hosted, linux, s390x]` and pointed at a LinuxONE VM. It is **opt-in**:
absent from the blank matrix, built only when a dispatch names
`arch: s390x`. Adding an architecture with no bottles to every nightly buys a
matrix of failures nobody reads.

### The second generation runs (2026-10-04)

`sovereign=0` buys exactly one thing — a first generation on an architecture
that has no bottles — and that licence ends the moment the seed exists.
`seed/dispatch-gen1.sh` spends it: `bk builder` stages a rootfs **from the
seed registry**, and everything is rebuilt inside it under `chroot`.

The first package through end to end was `pass lz4.org 1.10.0 linux/s390x`,
and it took ten dispatches to get there. `seed/README.md` has the sequence;
the short version is that the first four obstacles were broken tooling and the
rest were generation 0 showing what the Ubuntu builder had lent it —
`libselinux` in `coreutils`, then in `sed`, then a `libcxx` that was never
built. **Once the lane runs, every failure is a report about generation 0.
That is the point of the lane, not a defect in it.**

Two tools came out of it and both are in `bk`: `builder --dry-run`, which says
whether an architecture has the bottles without taking a runner, and the
staged-tree soname census, which names every library the tree lacks **and who
asks for it** — five at once, where before each cost a failed build.

The runner is ours to build because `actions/runner` does not build for s390x
out of the box — it is .NET, and `linux-s390x` is a community-supported target
with no Microsoft binaries. [`IBM/action-runner-image-pz`][gaplib] carries the
dotnet environment and the IBM Z patches; its `run.sh` is interactive and
covers Ubuntu 22.04/24.04 and CentOS 9. The lane assumes an Ubuntu image,
because the job's `base tools` step uses `apt-get`.

    git clone https://github.com/IBM/action-runner-image-pz
    cd action-runner-image-pz && bash run.sh     # VM, Ubuntu, then register

IBM also offers **hosted** s390x runners free to open-source projects through
its Open Source Program Office. That is the better end state — native, and no
agent to rebuild whenever `actions/runner` moves — and it is an email rather
than a command.

### Bootstrapping an architecture that has no bottles

The sovereign rootfs needs `gnu.org/glibc`, `llvm.org` and
`kernel.org/linux-headers` as s390x bottles, and there are none. The way out is
already an input: `sovereign: "0"` falls back to the runner's distribution, so
the first pass builds the toolchain on the VM's own Ubuntu and the second
switches to the rootfs those bottles now make possible.

### What this lane is actually for

s390x is our **first big-endian target** — x86-64, aarch64 and both darwin
arches are little-endian, so every recipe carrying a byte-order bug has been
invisible. Expect the failures to be upstream's rather than ours, and expect
them to be where the time goes; the matrix entry is the cheap part.

[gaplib]: https://github.com/IBM/action-runner-image-pz

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
curl -fsSL https://go-pkgx.github.io/install.sh | sh -s -- pkgm v0.1.4
```

```powershell
# Windows (PowerShell)
$env:PKGM_VERSION='v0.1.4'; irm https://go-pkgx.github.io/install.ps1 | iex
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

Three workflows exist to catch the failures that do not announce themselves.

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

**`sonames.yml` — can what we publish actually load?** `bk unresolved` reads
every ELF and Mach-O in an installed closure and reports each reference the
store cannot answer. It was written and tested, and until 2026-10-23
**nothing in this repository ran it** — a control that exists and is wired to
nothing is not a control. What that cost: the published `python.org 3.14.7`
for linux/x86-64 needs `libssl.so.1.1` and `libcrypto.so.1.1` in `_ssl` and
`_hashlib`, and this registry publishes no openssl 1.x at all, so python's
`ssl` could not load and every build that fetches over HTTPS died there. The
same bottle for linux/aarch64 needs `libssl.so.3` — one project, one version,
two platforms, one of them wrong. `overrides/python.org.hcl` had already
moved the recipe's pin to `^3`; an override moves a *recipe*, and a
`DT_NEEDED` compiled into a bottle published earlier does not move with it.

`./sonamesweep` takes a **chunk** of the platform's bottled projects and says
which — "clean for chunk 3 of 14 on linux/x86-64" is a sentence that cannot
be read as a clean bill of health, and ~1400 closure installs per platform is
not a thing to pretend to do weekly. It refuses a catalogue for a platform
this host is not: `bk unresolved` installs for the **host**, so that answer
would be this machine's bottles under another platform's name.

### How the lanes bootstrap their tools, and why it is one line per workflow

Every publishing lane starts by installing `bk`, `pkgx` or `pkgm` from source,
and all of them need the *current* `main` rather than whatever a module proxy
has cached — a catalogue or a bottle built by an old `bk` is the one thing these
jobs must not publish.

That used to be written `GOPROXY=direct go install …@main`, at fourteen sites
across eight workflows. `direct` applies to the **whole** module graph, not to
the module named on the line, so the bootstrap depended on every vanity-import
host in the closure being reachable. On 2026-10-12 `gopkg.in` answered 502 for
hours and the darwin lane failed twice in ninety seconds, having built nothing:

```text
reading gopkg.in/yaml.v3/go.mod at revision v3.0.1: … exit status 128:
fatal: unable to access 'https://gopkg.in/yaml.v3/': The requested URL returned error: 502
```

The policy is now stated once per workflow, in its top-level `env:`, which is
the narrowest scope that covers every `go` invocation in a file — including one
buried inside a longer script, and with no dependency on `actions/checkout`
having run:

```yaml
env:
  GOPROXY: https://proxy.golang.org,direct
  GONOPROXY: github.com/go-pkgx/*
```

Dependencies come from the proxy, which is a cache that survives an upstream
host going down; `go-pkgx` modules still go straight to GitHub, so `@main` is
the commit it says. Measured both ways on one machine in one minute with a
clean module cache — `GOPROXY=direct` exit 1, the configuration above exit 0 —
and the narrowness proved by pointing `GOPROXY` at a dead address, where our
modules still resolve and `gopkg.in/yaml.v3` does not.

`go run ./goproxycheck` holds the eight copies level: it refuses any command
line that sets the policy inline, and any workflow that installs a module by
version without declaring it. It runs in CI as part of `go test ./...`, reads
composite actions as well as workflows, and **exits 2 if it read no files at
all** — a scan that could not reach its input must not report a clean tree.

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
