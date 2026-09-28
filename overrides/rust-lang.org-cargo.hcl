project = "rust-lang.org/cargo"
why     = "converted from rust-lang.org-cargo-official-bootstrap.patch, rust-lang.org-cargo-s390x-bootstrap-triple.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.dependencies.linux"
    set = {
      "gnu.org/gcc" = "*"
    }
  },
  {
    path = "build.dependencies[\"openssl.org\"]"
    set = "^3"
  },
  {
    path = "build.script"
    prepend = [
      {
        "if" = "linux"
        "run" = <<EOT
set -eu
L=""
for c in "$${PKGX_DIR:-$HOME/.pkgx}"/gnu.org/gcc/*/lib/gcc/*/*/libgcc.a; do
  if [ -e "$c" ]; then L="$(dirname "$c")"; fi
done
if [ -z "$L" ]; then
  echo "no libgcc.a under $${PKGX_DIR:-$HOME/.pkgx}/gnu.org/gcc — is gnu.org/gcc a build dep?" >&2
  exit 1
fi
export RUSTFLAGS="$${RUSTFLAGS:-} -L$L"
echo "libgcc: $L"
EOT
      },
      {
        "run" = <<EOT
set -eu
# openssl-sys refuses to guess: "Could not find directory of OpenSSL
# installation, and this `-sys` crate cannot proceed without this
# knowledge." Point it at the bottle, same globbing rule as libgcc.
# The MAJOR symlink, not the concrete version. pkgx stages v3, v3.6 and
# v3.6.4 alike, and a glob of v3* keeps the most specific one — which
# openssl-sys then bakes into the binary as
#   @rpath/openssl.org/v3.6.4/lib/libssl.3.dylib
# while this recipe DECLARES a range (^3). The two disagree the moment a
# newer 3.x is published: the closure stages what the range resolves to,
# and the binary asks for a directory nobody put there. v3 is the level
# pkgx maintains for exactly this.
O="$${PKGX_DIR:-$HOME/.pkgx}/openssl.org/v3"
if [ ! -e "$O/include/openssl/ssl.h" ]; then
  echo "no openssl 3 at $O — is openssl.org ^3 a build dep?" >&2
  exit 1
fi
export OPENSSL_DIR="$O"
echo "openssl: $O"
EOT
      },
      {
        "run" = <<EOT
set -eu
# {{hw.target}} renders the same triples static.rust-lang.org names its
# archives by — x86_64/aarch64-unknown-linux-gnu,
# s390x-unknown-linux-gnu, x86_64/aarch64-apple-darwin — so there is no
# table mapping one naming to the other, and no chance of the two
# drifting apart. It replaces `case "$(uname -s)-$(uname -m)"`, which
# spawned two processes to learn something bk had already resolved AND
# had no s390x arm at all, so the seed for that architecture stopped at
# "no pinned bootstrap cargo".
#
# It is NOT a cross-build fix, and saying so would be false: bk's
# Target.Triple is the triple of the machine bk RUNS on for every
# non-windows target — `BREWKIT_TARGET=linux/s390x bk target` on a Mac
# prints aarch64-apple-darwin — so this is right here only because the
# factory builds each platform natively or under emulation, where the
# two coincide. A genuine cross build would need Target.Triple fixed
# first, not this recipe changed.
BT="{{hw.target}}"
case "$BT" in
  x86_64-unknown-linux-gnu)  SUM=9853db03d68578a30972e2755c89c66aec035fec641cf8f3a7117c81eec2578d ;;
  aarch64-unknown-linux-gnu) SUM=bd8d1da6fe88ea7e29338f24277c22156267447adbfc47d690467ad32d02c2a7 ;;
  s390x-unknown-linux-gnu)   SUM=468ace270ee4edac0a10185ea876ea555e41bacc0137ff08985d2155fe8cc777 ;;
  x86_64-apple-darwin)       SUM=b2fa21c8fed854775e379bb4617145abd047d1be729e8383148139ba1d05c88f ;;
  aarch64-apple-darwin)      SUM=17a4410a27bf7dad4765f3809265c225f25f8b009da3d4b76cd0927acdae04b5 ;;
  *) echo "no pinned bootstrap cargo for $BT" >&2; exit 1 ;;
esac
T="cargo-1.90.0-$BT.tar.xz"
curl -fsSLo "$T" "https://static.rust-lang.org/dist/$T"
# sha256sum on linux, shasum on macOS: the check is not optional, so the
# tool that performs it is chosen rather than assumed.
if command -v sha256sum >/dev/null 2>&1; then echo "$SUM  $T" | sha256sum -c -
else echo "$SUM  $T" | shasum -a 256 -c -; fi
tar xJf "$T"
mkdir -p "$PWD/.bootstrap"
mv "cargo-1.90.0-$BT/cargo/bin/cargo" "$PWD/.bootstrap/cargo"
export PATH="$PWD/.bootstrap:$PATH"
cargo --version
EOT
      },
    ]
  },
  {
    path = "dependencies[\"openssl.org\"]"
    set = "^3"
  },
]
