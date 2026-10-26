#!/usr/bin/env bash
# Run ONE program inside a staged sovereign rootfs, with the recipes, the
# advisories and an output directory bound in. Sibling of sovereign-build.sh,
# which can only run `bk factory` over a PANTRY: a wasip1 recipe is not in the
# pantry (its slug is zlib.net, and the pantry's zlib.net is the host recipe), so
# it is built with `bk build --recipe` instead — in the same namespace, the same
# chroot, the same pkgx-provided environment.
#
# Usage:  builder/sovereign-bk.sh <rootfs> <recipes-dir> <advisories-dir> <program> [args...]
#
#   <program> is an ABSOLUTE path inside the rootfs: /usr/local/bin/bk for the
#   build and the publish, /bin/sh for the evidence step. It is exec'd through
#   `pkgx +gnu.org/glibc +gnu.org/coreutils +gnu.org/make +llvm.org --`, which is
#   builder/Containerfile's ENTRYPOINT: make, clang, wasm-ld and llvm-ar come
#   from bottles in the rootfs, never from the runner.
#
# Bound in: <recipes-dir> at /recipes, <advisories-dir> at /advisories, and
# $root/dist is /dist (it is a plain directory of the rootfs). Every piece below
# is in sovereign-build.sh for the reason its header gives; none is new.
set -euo pipefail

root="${1:?rootfs}"; recipes="${2:?recipes}"; advisories="${3:?advisories}"; shift 3
: "${1:?program}"

mkdir -p "$root/recipes" "$root/advisories" "$root/dist" "$root/dev" "$root/proc" "$root/tmp" "$root/etc"
chmod 1777 "$root/tmp"
cp /etc/resolv.conf "$root/etc/resolv.conf"

SUDO=""
if [ "$(id -u)" -ne 0 ]; then SUDO="sudo"; fi
# --preserve-env is sudo's flag, so it goes when sudo does (see sovereign-build.sh).
PRESERVE=""
[ -n "$SUDO" ] && PRESERVE="--preserve-env=OCI_USERNAME,OCI_PASSWORD,SIGNING_KEY,PKGX_DIR,HOME,PKGX_DIST,PKGX_CACHE,PKGX_VERIFY,PKGX_PANTRY_OVERLAY,SOURCE_DATE_EPOCH"

# shellcheck disable=SC2016  # the inner script is literal: it runs in the new namespace.
# shellcheck disable=SC2086  # $PRESERVE is one flag or nothing.
$SUDO $PRESERVE \
  unshare --mount --pid --fork "$(command -v bash)" -euxc '
    r="$1"; recipes="$2"; advisories="$3"; shift 3
    : > "$r/dev/null";    mount --bind /dev/null    "$r/dev/null"
    : > "$r/dev/zero";    mount --bind /dev/zero    "$r/dev/zero"
    : > "$r/dev/full";    mount --bind /dev/full    "$r/dev/full"
    : > "$r/dev/random";  mount --bind /dev/random  "$r/dev/random"
    : > "$r/dev/urandom"; mount --bind /dev/urandom "$r/dev/urandom"
    : > "$r/dev/tty";     mount --bind /dev/tty     "$r/dev/tty"
    mount -t proc proc "$r/proc"
    mount --bind "$recipes" "$r/recipes"
    mount --bind "$advisories" "$r/advisories"
    prog="$1"; shift
    exec chroot "$r" /usr/local/bin/pkgx \
      +gnu.org/glibc +gnu.org/coreutils +gnu.org/make +llvm.org \
      -- "$prog" "$@"
  ' bash "$root" "$recipes" "$advisories" "$@"
