#!/usr/bin/env bash
# Build pkgx pantry recipes with bk and publish signed, attested bottles to
# ghcr.io/go-pkgx/packages. Each REQUESTED project builds EVERY available
# upstream version (all git tags / listed versions, newest first via
# `bk versions`), not just the latest; the requested set is also expanded to its
# full runtime-dependency closure (deps built before dependents), and those
# dep-only projects build at their resolved latest. A (project,version,platform)
# already in ghcr is skipped — so shared deps build once and, across cron runs,
# only missing (proj,ver,platform) tuples are (re)built (incremental). Per-build
# failures are logged (failures.txt) but never fail the run: the pantry is built
# progressively.
#
# Env: PLATFORM=linux/x86-64|linux/aarch64  (required)
#      RECIPES="p1 p2 ..."   (optional; blank = recipes.txt)
#      PANTRY=<dir>          (default: pantry)
#      MAX_VERSIONS=<n>      (optional; cap versions/requested-project, newest
#                             first; blank/unset = all — a package with hundreds
#                             of tags stays tractable per run since the missing
#                             ones are picked up incrementally on later runs)
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

# Local recipe overrides: candidate fixes for genuine upstream-recipe bugs,
# applied to the pantry before building so we can validate them here before
# proposing them upstream to pkgxdev/pantry. Each overrides/*.patch is a
# `git apply`-able diff against the pantry root (paths like
# projects/<proj>/package.yml) — i.e. exactly the future upstream PR. Idempotent:
# tracked pantry files are reset first so re-runs against a persistent local
# clone (the docker repro harness mounts one) apply cleanly; a fresh CI clone is
# already clean. A patch that no longer applies (upstream moved) is skipped loud,
# not fatal — the recipe just falls back to upstream as-is.
OVERRIDES_DIR="$(pwd)/overrides"
if ls "$OVERRIDES_DIR"/*.patch >/dev/null 2>&1; then
  git -C "$PANTRY" checkout -- . 2>/dev/null || true
  for p in "$OVERRIDES_DIR"/*.patch; do
    if git -C "$PANTRY" apply --check "$p" 2>/dev/null; then
      git -C "$PANTRY" apply "$p" && echo "override applied: $(basename "$p")"
    else
      echo "override SKIP (does not apply): $(basename "$p")" >&2
    fi
  done
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

# inWant proj → 0 if proj was explicitly requested (so it builds ALL versions);
# closure-only deps (return 1) build just their resolved latest. WANT is never
# empty (RECIPES or recipes.txt), so the expansion is bash-3.2-safe.
inWant() {
  local x="$1" w
  for w in "${WANT[@]}"; do [ "$w" = "$x" ] && return 0; done
  return 1
}

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
: > failures-detail.txt

# buildOne proj rec ver → skip-if-published, else build (pinned to ver when
# non-empty) and publish. An empty ver means "resolved latest" (dep-only path):
# the skip check then uses dist.pkgx.dev's latest version. ${arr[@]+...} is the
# bash-3.2 + set -u safe empty-array expansion.
buildOne() {
  local proj="$1" rec="$2" ver="$3" checkver out rc bottle bver
  local verflag=()
  if [ -n "$ver" ]; then
    checkver="$ver"; verflag=(--version "$ver")
  else
    checkver=$(curl -fsSL "https://dist.pkgx.dev/${proj}/${OS}/${ARCH}/versions.txt" 2>/dev/null | tail -1)
  fi

  # FORCE (set for explicit manual dispatches) rebuilds+republishes even when the
  # bottle is already in ghcr — needed to refresh a STALE bottle, e.g. a library
  # published static-only before the shared-lib build worked (breaks from-scratch
  # dynamic consumers). The daily cron leaves FORCE unset so skip-if-published
  # keeps the full sweep incremental.
  if [ -z "${FORCE:-}" ] && [ -n "$checkver" ] && alreadyPublished "$proj" "$checkver"; then
    echo "⏭  SKIP $proj $checkver ($PLATFORM) — already in ghcr"; skip=$((skip+1)); return
  fi

  echo "::group::build $proj ${ver:-latest} ($PLATFORM)"
  out="$(bk --platform "$PLATFORM" build --recipe "$rec" ${verflag[@]+"${verflag[@]}"} --dist dist "$proj" 2>&1)"; rc=$?
  printf '%s\n' "$out" | tail -20
  echo "::endgroup::"
  if [ $rc -ne 0 ]; then
    echo "❌ BUILD FAIL $proj ${ver:-latest}"; echo "$proj ${ver:-latest} build" >> failures.txt; fail=$((fail+1))
    # Capture the error tail so failures are diagnosable from the artifact alone
    # (container-job logs are not retrievable via the GitHub API).
    { echo "########## $proj ${ver:-latest} ($PLATFORM) — build failed rc=$rc"; printf '%s\n' "$out" | tail -30; echo; } >> failures-detail.txt
    return
  fi

  bottle="$(printf '%s\n' "$out" | sed -n 's/^bottle: //p' | tail -1)"
  bver="$(basename "${bottle:-}" | sed -E 's/^v(.*)\.tar\.(gz|xz)$/\1/')"
  [ -n "${bottle:-}" ] && [ -n "${bver:-}" ] || { echo "❌ PARSE FAIL $proj ${ver:-latest}"; echo "$proj ${ver:-latest} parse" >> failures.txt; fail=$((fail+1)); return; }

  echo "::group::publish $proj $bver"
  if SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-1700000000}" \
     bk publish --to "$DIST" --project "$proj" --version "$bver" --platform "$PLATFORM" "${SIGN_ARGS[@]}" "$bottle"; then
    echo "✅ OK $proj $bver $PLATFORM"; ok=$((ok+1))
  else
    echo "❌ PUBLISH FAIL $proj $bver"; echo "$proj $bver publish" >> failures.txt; fail=$((fail+1))
  fi
  echo "::endgroup::"
}

for proj in "${LIST[@]}"; do
  rec="$PANTRY/projects/$proj/package.yml"
  [ -f "$rec" ] || { echo "SKIP $proj (no recipe)"; continue; }

  # Requested projects build every available version (newest first, capped by
  # MAX_VERSIONS); closure-only deps build a single resolved-latest (ver="").
  VERS=()
  if inWant "$proj"; then
    while IFS="$(printf '\t')" read -r v _tag; do
      [ -n "$v" ] && VERS+=("$v")
    done < <(bk versions --recipe "$rec" 2>/dev/null \
             | { if [ -n "${MAX_VERSIONS:-}" ]; then head -n "$MAX_VERSIONS"; else cat; fi; })
    if [ ${#VERS[@]} -eq 0 ]; then
      echo "⚠ $proj: bk versions found none — falling back to resolved latest" >&2
      VERS=("")
    else
      echo "versions $proj: ${#VERS[@]} to consider ($PLATFORM)"
    fi
  else
    VERS=("")
  fi

  for ver in ${VERS[@]+"${VERS[@]}"}; do
    buildOne "$proj" "$rec" "$ver"
  done
done

echo "=== summary ($PLATFORM): $ok built, $skip skipped, $fail failed ==="
[ -s failures.txt ] && { echo "failures:"; cat failures.txt; }
exit 0
