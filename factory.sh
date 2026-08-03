#!/usr/bin/env bash
# Build pkgx pantry recipes with bk and publish signed, attested bottles to
# ghcr.io/go-pkgx/bottles. Individual recipe failures are logged (to
# failures.txt) but do not fail the run — the pantry is built progressively.
#
# Env: PLATFORM=linux/x86-64|linux/aarch64  (required)
#      RECIPES="p1 p2 ..."   (optional; blank = recipes.txt)
#      PANTRY=<dir>          (default: pantry)
#      OCI_USERNAME/OCI_PASSWORD  ghcr push creds
#      SIGNING_KEY           (optional; org secret → bk publish --sign)
set -uo pipefail

PLATFORM="${PLATFORM:?set PLATFORM=linux/x86-64|linux/aarch64}"
DIST="oci://ghcr.io/go-pkgx/bottles"
PANTRY="${PANTRY:-pantry}"

# Signing key (org secret) → a private temp file for `bk publish --sign`.
SIGN_ARGS=()
if [ -n "${SIGNING_KEY:-}" ]; then
  umask 077
  KEYFILE="$(mktemp)"
  printf '%s' "$SIGNING_KEY" > "$KEYFILE"
  SIGN_ARGS=(--sign "$KEYFILE")
  echo "signing: enabled"
else
  echo "signing: disabled (no SIGNING_KEY)"
fi

# Recipe list: workflow input wins, else recipes.txt (minus blanks/comments).
if [ -n "${RECIPES:-}" ]; then
  read -ra LIST <<<"$RECIPES"
else
  mapfile -t LIST < <(grep -vE '^[[:space:]]*(#|$)' recipes.txt)
fi
echo "building ${#LIST[@]} recipe(s) for $PLATFORM"

ok=0 fail=0
: > failures.txt
for proj in "${LIST[@]}"; do
  rec="$PANTRY/projects/$proj/package.yml"
  if [ ! -f "$rec" ]; then echo "SKIP $proj (no recipe at $rec)"; continue; fi

  echo "::group::build $proj ($PLATFORM)"
  out="$(bk --platform "$PLATFORM" build --recipe "$rec" --dist dist "$proj" 2>&1)"; rc=$?
  printf '%s\n' "$out" | tail -25
  echo "::endgroup::"
  if [ $rc -ne 0 ]; then echo "❌ BUILD FAIL $proj"; echo "$proj build" >> failures.txt; fail=$((fail+1)); continue; fi

  bottle="$(printf '%s\n' "$out" | sed -n 's/^bottle: //p' | tail -1)"
  ver="$(basename "${bottle:-}" | sed -E 's/^v(.*)\.tar\.(gz|xz)$/\1/')"
  if [ -z "${bottle:-}" ] || [ -z "${ver:-}" ]; then
    echo "❌ PARSE FAIL $proj (bottle=$bottle)"; echo "$proj parse" >> failures.txt; fail=$((fail+1)); continue
  fi

  echo "::group::publish $proj $ver"
  if SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-1700000000}" \
     bk publish --to "$DIST" --project "$proj" --version "$ver" \
        --platform "$PLATFORM" "${SIGN_ARGS[@]}" "$bottle"; then
    echo "✅ OK $proj $ver $PLATFORM"; ok=$((ok+1))
  else
    echo "❌ PUBLISH FAIL $proj"; echo "$proj publish" >> failures.txt; fail=$((fail+1))
  fi
  echo "::endgroup::"
done

echo "=== summary ($PLATFORM): $ok ok, $fail failed ==="
[ -s failures.txt ] && { echo "failures:"; cat failures.txt; }
exit 0
