# The seed order

`order.txt` is the order in which an architecture that has **no bottles at all**
gets its first generation. It is not a list of packages we happen to want; it is
a build plan, and most of it is forced.

## Why a file, and not a command

`bk closure --build` computes an order, and for most of this file it computes
*this* one. It cannot compute all of it, because the build-dependency graph has
cycles and an order over a cyclic graph does not exist.

What exists is a topological sort of everything else, **plus a choice of which
edges to give up on** — Debian's bootstrap tooling calls that a *feedback arc
set*, and [botch][botch] computes one because it is the part a person has to
decide. (Debian's build graph under native compilation has a single strongly
connected component of about a thousand vertices. Ours has 27 of 77.)

So the order is a human artefact with a machine-checkable property, and both
halves matter:

```
bk closure --cycles --build --pantry P --overlay O --overrides overrides $(…)
```

names the components, and

```
bk closure --build --check-order seed/order.txt --pantry P --overlay O --overrides overrides $(…)
```

separates what the file decided from what it got wrong:

| | |
|---|---|
| an edge out of order **inside** a component | the **choice**. Unavoidable. 19 of them today. |
| an edge out of order **between** components | a **mistake**. Nothing forces it. Must be 0. |

CI runs the second one. It is not a formality: the first time it was run, on
2026-09-28, it found two —

```
rust-lang.org is built at 62 and needs llvm.org at 65
rust-lang.org/cargo is built at 63 and needs llvm.org at 65
```

— which was the live failure of that day's seed run,
`cargo: resolve deps: GET .../llvm.org/linux/s390x/versions.txt: Not Found`,
with two more failures cascading from it. One line moved fixed all three.

## Why it is HERE and not on somebody's laptop

It was on one, for weeks. The 19 cycle choices in it were paid for in failed
three-hour builds, and nothing but that laptop remembered them.

## Running it

```
./seed/dispatch-seed.sh [ref]            build what is MISSING
FORCE=1 ./seed/dispatch-seed.sh [ref]    rebuild it all
```

The script is the positive form of knowledge the workflow now also enforces:
`bootstrap=1` needs `sovereign=0` and `max_versions`, and a dispatch without
them is refused in `setup`, before a runner is taken. It also pins
`tcl-lang.org@=9.0.4`, because that recipe enumerates SourceForge *directories*
and `Tcl/9.1.0/` exists holding nothing but a release candidate (#262).

## The second generation

`sovereign=0` buys exactly one thing: a first generation on an architecture
that has no bottles at all. It takes the compiler, `make` and the shell from
the runner's distribution because there is nothing else to take them from.
**That licence ends the moment the seed exists.**

```
RECIPES="lz4.org" ./seed/dispatch-gen1.sh   one package, to prove the rootfs
./seed/dispatch-gen1.sh                     all of order.txt, sovereign
```

It differs from `dispatch-seed.sh` in two inputs: `bootstrap` is empty (the
toolchain is bottles now) and `sovereign=1` (`bk builder` stages a rootfs
**from the seed registry** and the build happens inside it, under `chroot`).
`seed_dist` stays, and `--to` follows `PKGX_DIST` on purpose: generation 1
replaces generation 0 in the same throwaway registry, which is what build.yml
means by "thrown away once the sovereign rebuild replaces it". Nothing built
against a host distribution reaches ghcr in either generation.

**Pilot with one package.** The sovereign path on s390x had never run at all
until 2026-10-04. It took **ten** dispatches to get one package through, and
the sequence is worth keeping because it is what bringing up a generation
actually looks like:

| it died on | because |
|---|---|
| resolving the toolchain | a pin written for a stale mirror (`~7.1`) against a seed that holds only 7.2.8 |
| staging the rootfs | the seed's bottles are unsigned and verification was on |
| posing the loader | `bottle.LoaderNameFor` had no s390x entry — its loader is `ld64.so.1`, not `ld-linux-s390x.so.1` |
| installing, three times over | `$RUNNER_TEMP` is reused on a self-hosted runner and `InstallFor` reads an existing directory as "already installed", so a half-written tree was inherited in silence |
| the first `mkdir` | `coreutils` linked the builder's `libselinux`, which no bottle provides |
| `make install` | `sed` did too |
| the dependency eval | `libcxx.llvm.org` was never in the seed, and bk hands it to **every** `--libc pkgx` build |

Each was invisible until the previous was lifted. A one-package pilot costs a
staging; the same sequence found seventy builds in would have cost a day.

**Two tools came out of it, and they are the reason the list above is short.**
`bk builder --dry-run` answers "does this architecture have the bottles"
without taking a runner, naming **every** blocked root rather than the first —
and telling a conflict between roots apart from a missing bottle. After
staging, `bk builder` reports every `NEEDED` soname the tree does not contain,
**with who asks**: that turned "one contaminated package per failed build"
into five names at once.

Neither is sufficient. A bottle that resolves may still fail to unpack, and a
census is not a worklist — of the five sonames, `lldb` and texinfo's `info`
are on no build path at all, and the second generation rebuilds them clean
inside a rootfs where the host libraries do not exist.

**What generation 1 is for.** Once it runs, every failure is a report about
generation 0: what the Ubuntu builder lent it. That is the point of the lane,
not a defect in it.

[botch]: https://manpages.debian.org/testing/botch/botch.1.en.html
