# Recipe overrides

Candidate fixes for genuine **upstream-recipe** bugs (not `bk` gaps), applied to
the cloned `pkgxdev/pantry` before building so we can validate them in our
factory *before* proposing them upstream.

## How it works

`bk factory` applies every `overrides/*.patch` to the pantry before computing
the closure and building — in pure Go, via bk's `overrides` package, with no
`git apply` shell-out. Each patch is a normal `git diff` against the
**pantry root**, so its paths look like
`projects/<project>/package.yml`. That is exactly the diff we later submit as a
pull request to `pkgxdev/pantry` — the override file *is* the upstream PR.

Application is idempotent (tracked pantry files are reset first), so it is safe
against both a fresh CI clone and the persistent clone the local docker repro
harness mounts. A patch that no longer applies (upstream moved) is skipped
loudly and the recipe falls back to upstream as-is — never fatal.

## Workflow

1. Reproduce the failure locally (`scratchpad/repro-localbk.sh <project>`), read
   the real error.
2. Confirm it is a recipe bug (builds in real pkgx, differs only in the recipe),
   not a `bk` behaviour gap — fix `bk` instead if it is.
3. Write `overrides/<slug>.patch` (`git -C pantry diff projects/<p>/package.yml`).
4. Validate it builds via the harness (which now applies overrides).
5. Once green, open the same patch as a PR upstream; when merged, delete the
   local override.

## Naming

`<slug>.patch` where `<slug>` is the project with `/` → `-`
(e.g. `invisible-island.net-ncurses.patch`). One patch may touch one project.

## Status

- `info-zip.org-zip.patch` — **partial / work-in-progress.** Proved the override
  pipeline end-to-end (applies, deps resolve, Debian patch series applies,
  `-std=gnu17` takes effect). Fixes three genuine recipe bugs: the dead Debian
  patch tarball URL (`zip_3.0-11` → `3.0-13`), the missing `.patch` extension on
  each patch path, and K&R function definitions rejected under gcc-14/gnu23
  (`CC="gcc -std=gnu17"`). One layer of zip-3.0 rot remains: `zip.h` redeclares
  `memcmp`/`memcpy` with pre-standard signatures that conflict with modern
  glibc's `<string.h>`. Tracked; the recipe does not build to a bottle yet.

