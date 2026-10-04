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
until 2026-10-04, and its first attempt died in 22 seconds on a toolchain pin —
`kernel.org/linux-headers@~7.1` against a seed that holds only 7.2.8. That is
the price of a staging, not of seventy builds.

Before dispatching anything, `bk builder --dry-run --platform linux/s390x
--toolchain builder/toolchain.txt` answers "does this architecture have the
bottles" without taking a runner. It is a necessary condition, not a sufficient
one: a bottle that is listed may still fail to unpack.

[botch]: https://manpages.debian.org/testing/botch/botch.1.en.html
