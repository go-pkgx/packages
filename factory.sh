#!/usr/bin/env bash
# Build pkgx pantry recipes with bk and publish signed, attested bottles to
# ghcr.io/go-pkgx/packages. The requested projects are expanded to their full
# runtime-dependency closure (deps built before dependents), and a
# (project,version,platform) already in ghcr is skipped — so shared deps build
# once. Per-recipe failures are logged (failures.txt) but never fail the run:
# the pantry is built progressively.
#
# Env: PLATFORM=linux/x86-64|linux/aarch64  (required)
#      RECIPES="p1 p2 ..."   (optional; blank = recipes.txt)
#      PANTRY=<dir>          (default: pantry)
#      OCI_USERNAME/OCI_PASSWORD  ghcr creds
#      SIGNING_KEY           (optional; org secret → bk publish --sign)
set -uo pipefail

PLATFORM="${PLATFORM:?set PLATFORM=linux/x86-64|linux/aarch64}"
DIST="oci://ghcr.io/go-pkgx/packages"
PANTRY="${PANTRY:-pantry}"
OS="${PLATFORM%/*}"; ARCH="${PLATFORM#*/}"
OARCH="$ARCH"; [ "$ARCH" = x86-64 ] && OARCH=amd64; [ "$ARCH" = aarch64 ] && OARCH=arm64

SIGN_ARGS=()
if [ -n "${SIGNING_KEY:-}" ]; then
  umask 077; KEYFILE="$(mktemp)"; printf '%s' "$SIGNING_KEY" > "$KEYFILE"
  SIGN_ARGS=(--sign "$KEYFILE"); echo "signing: enabled"
else
  echo "signing: disabled (no SIGNING_KEY)"
fi

# Requested list: workflow input, else recipes.txt (minus blanks/comments).
# NB: `mapfile` is bash 4+, absent from macOS's stock bash 3.2 — read portably.
WANT=()
if [ -n "${RECIPES:-}" ]; then read -ra WANT <<<"$RECIPES"; else
  while IFS= read -r line; do WANT+=("$line"); done \
    < <(grep -vE '^[[:space:]]*(#|$)' recipes.txt); fi

# Expand to the topologically-ordered runtime closure (deps first).
LIST=()
while IFS= read -r line; do LIST+=("$line"); done \
  < <(PANTRY="$PANTRY" PLATFORM="$PLATFORM" go run ./closure "${WANT[@]}")
echo "closure: ${#LIST[@]} project(s) for $PLATFORM (from ${#WANT[@]} requested)"

# alreadyPublished proj ver → 0 if ghcr already has this platform for that version.
alreadyPublished() {
  local proj="$1" ver="$2" tok man
  tok=$(curl -fsSL -u "$OCI_USERNAME:$OCI_PASSWORD" \
    "https://ghcr.io/token?service=ghcr.io&scope=repository:go-pkgx/packages/${proj}:pull" \
    | sed -n 's/.*"token":"\([^"]*\)".*/\1/p') || return 1
  man=$(curl -fsSL -H "Authorization: Bearer $tok" \
    -H 'Accept: application/vnd.oci.image.index.v1+json' \
    "https://ghcr.io/v2/go-pkgx/packages/${proj}/manifests/${ver}" 2>/dev/null) || return 1
  printf '%s' "$man" | jq -e --arg os "$OS" --arg a "$OARCH" \
    '.manifests[]?.platform | select(.os==$os and .architecture==$a)' >/dev/null 2>&1
}

ok=0 fail=0 skip=0
: > failures.txt
for proj in "${LIST[@]}"; do
  rec="$PANTRY/projects/$proj/package.yml"
  [ -f "$rec" ] || { echo "SKIP $proj (no recipe)"; continue; }

  ver=$(curl -fsSL "https://dist.pkgx.dev/${proj}/${OS}/${ARCH}/versions.txt" 2>/dev/null | tail -1)
  if [ -n "$ver" ] && alreadyPublished "$proj" "$ver"; then
    echo "⏭  SKIP $proj $ver ($PLATFORM) — already in ghcr"; skip=$((skip+1)); continue
  fi

  echo "::group::build $proj ($PLATFORM)"
  out="$(bk --platform "$PLATFORM" build --recipe "$rec" --dist dist "$proj" 2>&1)"; rc=$?
  printf '%s\n' "$out" | tail -20
  echo "::endgroup::"
  [ $rc -eq 0 ] || { echo "❌ BUILD FAIL $proj"; echo "$proj build" >> failures.txt; fail=$((fail+1)); continue; }

  bottle="$(printf '%s\n' "$out" | sed -n 's/^bottle: //p' | tail -1)"
  bver="$(basename "${bottle:-}" | sed -E 's/^v(.*)\.tar\.(gz|xz)$/\1/')"
  [ -n "${bottle:-}" ] && [ -n "${bver:-}" ] || { echo "❌ PARSE FAIL $proj"; echo "$proj parse" >> failures.txt; fail=$((fail+1)); continue; }

  echo "::group::publish $proj $bver"
  if SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-1700000000}" \
     bk publish --to "$DIST" --project "$proj" --version "$bver" --platform "$PLATFORM" "${SIGN_ARGS[@]}" "$bottle"; then
    echo "✅ OK $proj $bver $PLATFORM"; ok=$((ok+1))
  else
    echo "❌ PUBLISH FAIL $proj"; echo "$proj publish" >> failures.txt; fail=$((fail+1))
  fi
  echo "::endgroup::"
done

echo "=== summary ($PLATFORM): $ok built, $skip skipped, $fail failed ==="
[ -s failures.txt ] && { echo "failures:"; cat failures.txt; }
exit 0
