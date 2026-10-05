project = "gnu.org/glibc"
why     = "two faults that only show once the bottle MOVES. (1) build.env names a dynamic loader for x86-64 and aarch64 and for no other architecture, so on s390x $LDSO is empty and the `ld.so` symlink points at a DIRECTORY. (2) glibc compiles its gconv directory in at $libdir/gconv, which here is inside the `+brewing` prefix, so after the rename iconv can convert nothing and the recipe declares no runtime env to say otherwise."

merge {
  # GCONV_PATH, because glibc's charset modules are found by a path baked
  # in AT BUILD TIME and this bottle does not stay where it was built.
  #
  # The recipe puts libdir at {{prefix}}/lib/glibc-<marketing> -- its own
  # comment says "--libdir alone only affects gconv/audit plugins" -- so
  # the compiled-in gconv dir is
  #
  #   <prefix>/lib/glibc-2.44/gconv
  #
  # with <prefix> the `+brewing` staging path. That directory is gone by
  # the time anyone installs the bottle, and iconv silently falls back to
  # the charsets built into libc. The second sovereign generation shows
  # what that costs a consumer:
  #
  #   FAIL gnu.org/libidn2 2.3.8: exit status 1
  #   idn2: libiconv required for non-UTF-8 character encoding: ANSI_X3.4-1968
  #
  # which reads as a MISSING LIBRARY and is a missing directory. libiconv
  # is not required at all on glibc; iconv is in libc, and it is the gconv
  # modules it cannot find.
  #
  # `runtime.env` is the mechanism pkgx already has for this -- gnu.org/
  # guile uses it for GUILE_LOAD_PATH -- and glibc declared none at all.
  # The modules ship in the bottle; only the pointer to them was missing.
  #
  # NOT caught by the recipe's own test, which asserts `bin/iconv
  # --version` prints a version. A converter that can convert nothing
  # still has a version.
  runtime {
    env {
      # `$${` so HCL emits a literal `${`: the recipe language's
      # `${{prefix}}` is a moustache preceded by a dollar, and HCL would
      # otherwise read `${` as the start of its own interpolation.
      GCONV_PATH = "$${{prefix}}/lib/glibc-{{version.marketing}}/gconv"
    }
  }

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
