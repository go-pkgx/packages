#!/usr/bin/env bash
# mkclosure.sh — resolve a pantry project's full RUNTIME closure to published
# ghcr bottles and build a `docker FROM scratch` image for it (via mkscratch.sh).
#
#   mkclosure.sh <arch> <tag> <entrypoint> <root-project>
#
#     <arch>          amd64 | arm64  (aliases: x86-64 | aarch64)
#     <tag>           docker image tag to build
#     <entrypoint>    in-image absolute path to the binary to run. Use the token
#                     {V} for the root project's resolved version directory, e.g.
#                     /pkgx/lz4.org/{V}/bin/lz4  ->  /pkgx/lz4.org/v1.10.0/bin/lz4
#     <root-project>  pantry project, e.g. lz4.org, openssl.org
#
# It parses each recipe's TOP-LEVEL `dependencies:` (a key is a project iff it
# looks like a host `…​.<tld>(/path)?`; non-dotted keys such as linux / aarch64
# are platform maps and are recursed into only for the target os/arch — darwin /
# windows / the other arch are skipped), transitively with a visited-set, always
# adds gnu.org/glibc, and for every project picks the NEWEST version published on
# ghcr with a linux/<arch> manifest. If any closure member is not published for
# the arch it aborts (exit 3) rather than building a broken image.
#
# Env: PANTRY=<dir> (default: pantry). bash 3.2-portable. Deps: python3, docker,
# curl (via mkscratch.sh).
set -uo pipefail

die() { echo "mkclosure: $*" >&2; exit 1; }

[ $# -eq 4 ] || die "usage: mkclosure.sh <arch> <tag> <entrypoint> <root-project>"
ARCH="$1"; TAG="$2"; ENTRYPOINT="$3"; ROOTPROJ="$4"
PANTRY="${PANTRY:-pantry}"
HERE="$(cd "$(dirname "$0")" && pwd)"

case "$ARCH" in
  amd64|x86-64|x86_64) OARCH=amd64; PARCH=x86-64 ;;
  arm64|aarch64)       OARCH=arm64; PARCH=aarch64 ;;
  *) die "unknown arch '$ARCH' (want amd64|arm64)" ;;
esac
[ -d "$PANTRY/projects" ] || die "pantry not found at '$PANTRY' (set PANTRY=<dir>)"

# Resolve the closure to `proj:ver` lines (glibc first). The python helper prints
# either the resolved specs (exit 0) or a diagnostic + exit 3 for an unpublished
# closure member.
SPECS="$(PANTRY="$PANTRY" OARCH="$OARCH" PARCH="$PARCH" ROOTPROJ="$ROOTPROJ" \
         python3 "$HERE/closure.py")" || {
  rc=$?
  printf '%s\n' "$SPECS" >&2
  exit "$rc"
}

# Collect specs into an array (bash 3.2: no mapfile), find the root version to
# expand {V} in the entrypoint.
SPEC_LIST=()
ROOTVER=""
while IFS= read -r line; do
  [ -n "$line" ] || continue
  SPEC_LIST+=("$line")
  p="${line%:*}"; v="${line##*:}"
  [ "$p" = "$ROOTPROJ" ] && ROOTVER="$v"
done < <(printf '%s\n' "$SPECS")

[ ${#SPEC_LIST[@]} -ge 2 ] || die "closure too small — resolution produced no deps"
[ -n "$ROOTVER" ] || die "root project $ROOTPROJ not in resolved closure"

ENTRYPOINT="${ENTRYPOINT//\{V\}/v$ROOTVER}"

echo "mkclosure: $ROOTPROJ closure (linux/$OARCH) = ${#SPEC_LIST[@]} bottle(s):"
for s in "${SPEC_LIST[@]}"; do echo "  $s"; done

exec bash "$HERE/mkscratch.sh" "$OARCH" "$TAG" "$ENTRYPOINT" "${SPEC_LIST[@]}"
