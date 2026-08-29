#!/usr/bin/env bash
# Run `bk factory` inside a staged sovereign rootfs, so neither the host's libc
# nor its compiler takes part in the build.
#
# Usage:  builder/sovereign-build.sh <rootfs> <pantry> <overrides> [bk args...]
#
# The rootfs is what `bk builder --container` staged. Everything the build needs
# beyond it is arranged here, and each piece is here because its absence was
# measured:
#
#   /proc      bk dies at `readlink /proc/self/exe: no such file or directory`.
#              A container runtime always mounts one.
#   /dev/*     bound, not mknod'd: an unprivileged Incus container has no
#              CAP_MKNOD and mknod answers "Operation not permitted".
#   /tmp       pkgm stages blobs there; without it, "temp file: no such file or
#              directory" AFTER the registry has already answered.
#   resolv.conf  the resolver a runtime injects too.
#
# The environment is builder/Containerfile's ENV, and bk is invoked through pkgx
# with its ENTRYPOINT's argv, so a caller cannot drift from what the image
# promises. PKGX_DIR matters more than it looks: without it pkgx falls back to
# $HOME/.pkgx, re-downloads a toolchain the staged tree already holds, and bakes
# PT_INTERP=/root/.pkgx/... into every bottle — an absolute path no consumer has.
set -euo pipefail

root="${1:?rootfs}"; pantry="${2:?pantry}"; overrides="${3:?overrides}"; shift 3

mkdir -p "$root/pantry" "$root/overrides" "$root/dist" "$root/dev" "$root/proc" "$root/tmp" "$root/etc"
chmod 1777 "$root/tmp"
cp /etc/resolv.conf "$root/etc/resolv.conf"

log="${SOVEREIGN_LOG:-${RUNNER_TEMP:-/tmp}/bk.log}"

SUDO=""
if [ "$(id -u)" -ne 0 ]; then SUDO="sudo"; fi

# --preserve-env, never a command line: the token and the signing key must not
# become argv anybody can read out of /proc.
# shellcheck disable=SC2016  # the inner script is deliberately literal: it runs
# in the new namespace, where these variables are the ones that matter.
$SUDO --preserve-env=OCI_USERNAME,OCI_PASSWORD,SIGNING_KEY,RECIPES,FORCE,MAX_VERSIONS,NO_CLOSURE,PLATFORM,PKGX_DIR,HOME,PKGX_DIST,PKGX_PANTRY_OVERLAY \
  unshare --mount --pid --fork "$(command -v bash)" -euxc '
    r="$1"; pantry="$2"; overrides="$3"; shift 3
    for d in null zero full random urandom tty; do
      [ -e "$r/dev/$d" ] || : > "$r/dev/$d"
      mount --bind "/dev/$d" "$r/dev/$d"
    done
    mount -t proc proc "$r/proc"
    mount --bind "$pantry" "$r/pantry"
    mount --bind "$overrides" "$r/overrides"
    exec chroot "$r" /usr/local/bin/pkgx \
      +gnu.org/glibc +gnu.org/coreutils +gnu.org/make +llvm.org \
      -- /usr/local/bin/bk factory \
           --pantry /pantry --overrides /overrides --libc pkgx \
           --to "${OCI_TO:-oci://ghcr.io/go-pkgx/packages}" --bottles /dist "$@"
  ' bash "$root" "$pantry" "$overrides" "$@" 2>&1 | tee "$log"

# `bk factory` is best-effort: it exits 0 with a per-recipe tally. That is right
# for a 50-project chunk and useless as a signal on its own, so read the tally.
# Both directions matter — the first green run of this path reported
# "0 built, 15 skipped, 0 failed": every step passed and nothing was compiled.
if grep -qE '=== summary .*: [0-9]+ built, [0-9]+ skipped, [1-9][0-9]* failed ===' "$log"; then
  echo "::error::a build failed inside the sovereign rootfs"
  exit 1
fi
if [ "${SOVEREIGN_REQUIRE_BUILD:-1}" = 1 ] &&
   ! grep -qE '=== summary .*: [1-9][0-9]* built,' "$log"; then
  echo "::error::nothing was built — a run that compiles nothing proves nothing"
  exit 1
fi
