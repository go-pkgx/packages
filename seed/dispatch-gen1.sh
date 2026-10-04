#!/bin/sh
# The SECOND generation: rebuild the seed inside a rootfs made of the seed.
#
# The rule this file exists to hold (user, 2026-10-04): *`sovereign=0` ne doit
# servir qu'à construire la graine, après on veut être autonome.* Generation 0
# takes its compiler, make and shell from the runner's distribution because on
# an architecture with no bottles at all there is nothing else to take them
# from. That licence ends the moment the seed exists.
#
# So this dispatch differs from dispatch-seed.sh in exactly two inputs, and
# both are the whole point:
#
#   bootstrap=      the toolchain is bottles now, not host tools
#   sovereign=1     `bk builder` stages a rootfs FROM the seed registry and
#                   the build happens inside it, under chroot
#
# seed_dist stays. `--to` follows PKGX_DIST on purpose, so generation 1
# replaces generation 0 in the same throwaway registry — build.yml calls the
# seed "thrown away once the sovereign rebuild replaces it". Nothing built
# against a host distribution reaches ghcr, in either generation.
#
#   ./dispatch-gen1.sh [ref]              rebuild all of order.txt, sovereign
#   RECIPES="lz4.org" ./dispatch-gen1.sh  one package — use this FIRST
#
# Start with one. The sovereign path on s390x had never run at all until
# 2026-10-04, and its first attempt died in 22 seconds on a toolchain pin
# (`kernel.org/linux-headers@~7.1` against a seed that holds only 7.2.8). A
# one-package pilot asks "does the rootfs stage and does a compile work" for
# the price of a staging, instead of finding out seventy builds in.
set -eu
here=$(dirname "$0")
ref=${1:-main}
# Same tcl-lang.org pin as the seed: that recipe enumerates SourceForge
# DIRECTORIES and Tcl/9.1.0/ holds nothing but a release candidate (#262).
recipes=${RECIPES:-$(sed 's|^tcl-lang\.org$|tcl-lang.org@=9.0.4|' "$here/order.txt" \
  | grep -vE '^[[:space:]]*(#|$)' | tr '\n' ' ' | sed 's/ *$//')}
exec gh workflow run build.yml --repo go-pkgx/packages --ref "$ref" \
  -f recipes="$recipes" \
  -f arch=s390x \
  -f seed_dist=oci://127.0.0.1:5000/seed \
  -f sovereign=1 \
  -f max_versions=1 \
  -f source_mirror= \
  -f force="${FORCE:-1}"
