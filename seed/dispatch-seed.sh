#!/bin/sh
# The s390x seed dispatch, written down because retyping it is how it goes
# wrong: one pass forgot `sovereign=0` and died 90s in naming glibc, the next
# forgot `max_versions` and started down zlib.net's entire release history.
# Both are now refused by the workflow, in `setup`, before a runner is taken —
# this file is the positive form of the same knowledge.
#
#   ./dispatch-seed.sh [ref]            build what is MISSING from order.txt
#   FORCE=1 ./dispatch-seed.sh [ref]    rebuild it all, the old behaviour
set -eu
here=$(dirname "$0")
ref=${1:-main}
# tcl-lang.org is PINNED. Its recipe enumerates SourceForge DIRECTORIES, and
# Tcl/9.1.0/ exists holding nothing but a release candidate — the 9.1.0 tarball
# is a 404 (go-pkgx/packages#262). A per-project `project@constraint` word,
# because --versions would pin all 76. The `=` is load-bearing: a bare version
# is a RANGE.
recipes=$(sed 's|^tcl-lang\.org$|tcl-lang.org@=9.0.4|' "$here/order.txt" \
  | grep -vE '^[[:space:]]*(#|$)' | tr '\n' ' ' | sed 's/ *$//')
exec gh workflow run build.yml --repo go-pkgx/packages --ref "$ref" \
  -f recipes="$recipes" \
  -f arch=s390x \
  -f seed_dist=oci://127.0.0.1:5000/seed \
  -f bootstrap=1 \
  -f sovereign=0 \
  -f max_versions=1 \
  -f source_mirror= \
  -f force="${FORCE:-0}"
