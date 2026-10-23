project = "opendev.org/openstack/python-openstackclient"
why     = "Asked for by a user on 2026-10-22, and the darwin bottle built and runs (10.5.0). Both linux lanes failed twice, identically, and not on anything about this package."

edits = [
  {
    why  = "⛔ THE BUILD REACHED FOR dist.pkgx.dev AND OUR RUNNERS CANNOT GET THERE. The recipe's first script line is `pkgx +openssl.org^3`, which runs the UPSTREAM pkgx and makes it fetch https://dist.pkgx.dev/pantry.tar.xz. On the self-hosted incus runners that is `Connection reset by peer` (code 104), twice in a row on both linux/x86-64 and linux/aarch64; the GitHub-hosted macOS runner could reach it, which is the only reason darwin succeeded. A sovereign factory must not need upstream's pantry at build time — the whole point of building here is that what we ship comes from what we hold. The dependency is declared instead, below, which stages openssl 3 from OUR registry; `true` keeps the script's shape and the substitution is idempotent, since the result no longer contains the text it replaced."
    path = "build.script"
    from = "pkgx +openssl.org^3"
    to   = "true  # openssl 3 is a declared build dependency; see overrides/"
  },
  {
    why  = "What the line above was for. `build.env.OPENSSL_DIR` is `{{pkgx.prefix}}/openssl.org/v3`, so the prefix has to exist: declaring it as a build dependency is how bk stages it, and it comes from the registry this factory publishes to rather than from upstream's."
    path = "build.dependencies[\"openssl.org\"]"
    set  = "^3"
  },
]
