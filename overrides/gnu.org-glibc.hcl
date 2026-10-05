project = "gnu.org/glibc"
why     = "the recipe's build.env names a dynamic loader for x86-64 and aarch64 and for no other architecture. On s390x $LDSO expands to nothing, so the `ld.so` convenience symlink points at a DIRECTORY and every wrapped bin/* execs one. bottle.LoaderNameFor has known s390x's loader is ld64.so.1 since the builder needed it; the recipe never learned."

merge {
  build {
    env {
      s390x {
        # s390x's loader is NOT ld-linux-s390x.so.1 -- the name cannot be
        # derived from the other two, which is half of why it was missed.
        # The s390x ABI calls it ld64.so.1, glibc installs it as that, and
        # bk's own bottle.LoaderNameFor already carries the same value for
        # the builder's sake.
        LDSO = "ld64.so.1"
      }
    }
  }
}

edits = [
  {
    # AND A GUARD, because the list above is the kind that is wrong again at
    # the next architecture. Today riscv64, ppc64le and loong64 all have a
    # published bk and no entry here, so the fourth one to be built would
    # repeat this silently:
    #
    #   ln -sf ../lib/glibc-2.44/ ld.so      <- a symlink to a directory
    #   exec "$libdir/" --library-path …     <- exec a directory
    #
    # Neither fails where it happens. The symlink is created, the wrapper is
    # written, `make install` is happy, and the bottle is published; the
    # failure arrives much later in somebody else's build, wearing
    # "No such file or directory" against a path that plainly exists.
    #
    # The test is on the FILE, not on the variable. An empty $LDSO and a
    # misspelled one are the same defect from the consumer's side, and only
    # one of the two is caught by `test -n`.
    why  = "turn a silent empty LDSO into a build failure that names the architecture"
    path = "build.script"
    from = "ln -sf ../lib/glibc-{{version.marketing}}/$LDSO ld.so"
    to   = <<EOT
if [ -z "$LDSO" ] || [ ! -e "../lib/glibc-{{version.marketing}}/$LDSO" ]; then
  echo "glibc: no dynamic loader named for {{hw.arch}}: LDSO=\"$LDSO\"" >&2
  echo "glibc: add it to build.env in the recipe (or to this override)." >&2
  echo "glibc: what make install actually left in the libdir:" >&2
  ls -1 ../lib/glibc-{{version.marketing}}/ld*.so* >&2 || true
  exit 1
fi
ln -sf ../lib/glibc-{{version.marketing}}/$LDSO ld.so
EOT
  },
]
