#!/usr/bin/env bash
# mkscratch.sh — build a genuine `docker FROM scratch` image containing ONLY
# pkgx bottles pulled from ghcr.io/go-pkgx/packages (no debian, no system libc).
#
#   mkscratch.sh <arch> <tag> <entrypoint> <proj:ver> [<proj:ver> ...]
#
#     <arch>        amd64 | arm64  (aliases: x86-64 | aarch64)
#     <tag>         docker image tag to build (e.g. gopkgx-scratch:lz4)
#     <entrypoint>  in-image absolute path to the binary to run as ENTRYPOINT,
#                   e.g. /pkgx/lz4.org/v1.10.0/bin/lz4
#     <proj:ver>…   EXPLICIT bottle list. The FIRST one MUST be gnu.org/glibc
#                   (it supplies the ELF loader + libc.so.6 for the whole image).
#
# Mechanism (pkgx's own runtime model, baked into the image):
#   1. Each bottle layer is pulled from ghcr and extracted at /pkgx/<proj>/v<ver>.
#      The ghcr pull filters BOTH os==linux AND the requested arch — an arch-only
#      filter also matches darwin/<arch> Mach-O manifests (known silent-wrong-pull).
#   2. LD_LIBRARY_PATH is auto-discovered = every dir in the closure that actually
#      holds a shared object (covers glibc's nested lib/glibc-<v>/ dir). Bottles
#      built post-bk#3 self-locate their own libs via a $ORIGIN RUNPATH, so in
#      practice only glibc must be found here — but listing every lib dir keeps
#      pre-bk#3 (slot-less) bottles working too.
#   3. The glibc loader is symlinked to the arch-standard PT_INTERP path so the
#      unmodified bottle binaries (whose PT_INTERP points at the build container's
#      loader) start: amd64 => /lib64/ld-linux-x86-64.so.2,
#      arm64 => /lib/ld-linux-aarch64.so.1.
#
# bash 3.2-portable (no mapfile / declare -A / ${v,,}). Deps: curl, python3, tar,
# docker.
set -uo pipefail

die() { echo "mkscratch: $*" >&2; exit 1; }

[ $# -ge 4 ] || die "usage: mkscratch.sh <arch> <tag> <entrypoint> <proj:ver>..."

ARCH="$1"; TAG="$2"; ENTRYPOINT="$3"; shift 3

# Normalise arch to the OCI form (OARCH) and pick the PT_INTERP loader path.
case "$ARCH" in
  amd64|x86-64|x86_64) OARCH=amd64; LOADER=ld-linux-x86-64.so.2; INTERP=/lib64/ld-linux-x86-64.so.2 ;;
  arm64|aarch64)       OARCH=arm64; LOADER=ld-linux-aarch64.so.1; INTERP=/lib/ld-linux-aarch64.so.1 ;;
  *) die "unknown arch '$ARCH' (want amd64|arm64)" ;;
esac

# The first bottle MUST be glibc — it is the loader + libc provider.
case "$1" in
  gnu.org/glibc:*) : ;;
  *) die "first bottle must be gnu.org/glibc:<ver> (got '$1')" ;;
esac

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
ROOT="$WORK/root"
mkdir -p "$ROOT/pkgx"

REG=https://ghcr.io/v2/go-pkgx/packages

# pull_bottle <proj> <ver> — pull the linux/<OARCH> bottle layer from ghcr and
# extract it under $ROOT/pkgx (layer paths already carry <proj>/v<ver>/…).
pull_bottle() {
  proj="$1"; ver="$2"
  echo "  pull $proj $ver (linux/$OARCH)"
  tok=$(curl -fsSL "https://ghcr.io/token?service=ghcr.io&scope=repository:go-pkgx/packages/${proj}:pull" \
        | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))') || return 1
  [ -n "$tok" ] || { echo "    no pull token for $proj" >&2; return 1; }
  idx=$(curl -fsSL -H "Authorization: Bearer $tok" \
        -H 'Accept: application/vnd.oci.image.index.v1+json' \
        "$REG/${proj}/manifests/${ver}") || return 1
  dig=$(printf '%s' "$idx" | python3 -c '
import sys,json
try: m=json.load(sys.stdin).get("manifests",[])
except Exception: m=[]
oa=sys.argv[1]
r=[x["digest"] for x in m
   if x.get("platform",{}).get("os")=="linux"
   and x.get("platform",{}).get("architecture")==oa]
print(r[0] if r else "")' "$OARCH")
  [ -n "$dig" ] || { echo "    no linux/$OARCH manifest for $proj $ver" >&2; return 1; }
  man=$(curl -fsSL -H "Authorization: Bearer $tok" \
        -H 'Accept: application/vnd.oci.image.manifest.v1+json' \
        "$REG/${proj}/manifests/${dig}") || return 1
  ldig=$(printf '%s' "$man" | python3 -c '
import sys,json
l=json.load(sys.stdin).get("layers",[])
print(l[0]["digest"] if l else "")')
  [ -n "$ldig" ] || { echo "    no layer for $proj $ver" >&2; return 1; }
  curl -fsSL -H "Authorization: Bearer $tok" "$REG/${proj}/blobs/${ldig}" -o "$WORK/layer.tgz" || return 1
  tar xzf "$WORK/layer.tgz" -C "$ROOT/pkgx" || return 1
}

for spec in "$@"; do
  proj="${spec%:*}"; ver="${spec##*:}"
  [ -n "$proj" ] && [ -n "$ver" ] && [ "$proj" != "$ver" ] || die "bad spec '$spec' (want proj:ver)"
  pull_bottle "$proj" "$ver" || die "failed to pull $spec"
done

# Symlink the glibc loader to the standard PT_INTERP path so unmodified bottles
# (PT_INTERP = the build container's loader path) can start.
LDSRC=$(find "$ROOT/pkgx/gnu.org/glibc" -name "$LOADER" -type f 2>/dev/null | head -1)
[ -n "$LDSRC" ] || die "glibc loader $LOADER not found in the glibc bottle"
LDIMG="${LDSRC#"$ROOT"}"          # image-absolute path, /pkgx/gnu.org/glibc/…
mkdir -p "$ROOT$(dirname "$INTERP")"
ln -sf "$LDIMG" "$ROOT$INTERP"

# Auto-discover every dir holding a shared object → LD_LIBRARY_PATH. glibc's
# libc.so.6 lives in the nested lib/glibc-<v>/ dir, so we cannot assume <prefix>/lib.
LDPATH=""
while IFS= read -r d; do
  [ -n "$d" ] || continue
  LDPATH="${LDPATH:+$LDPATH:}${d#"$ROOT"}"
done < <(find "$ROOT/pkgx" \( -name '*.so' -o -name '*.so.*' \) 2>/dev/null \
         | while IFS= read -r f; do dirname "$f"; done | sort -u)
[ -n "$LDPATH" ] || die "no shared-object dirs discovered (empty LD_LIBRARY_PATH)"

cat > "$WORK/Dockerfile" <<EOF
FROM scratch
COPY root/ /
ENV LD_LIBRARY_PATH=$LDPATH
ENTRYPOINT ["$ENTRYPOINT"]
EOF

echo "mkscratch: building $TAG (linux/$OARCH)"
echo "  LD_LIBRARY_PATH=$LDPATH"
echo "  loader $INTERP -> $LDIMG"
echo "  ENTRYPOINT $ENTRYPOINT"
docker build --platform "linux/$OARCH" -t "$TAG" "$WORK" || die "docker build failed"
echo "mkscratch: OK — $TAG"
