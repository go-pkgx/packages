project = "gnu.org/glibc"
why     = "bk exports `MAKEFLAGS=… AUTOCONF=true …` so a stale timestamp cannot send make off to re-run autotools (bk build/build.go). For every autotools project that is a harmless no-op, and for glibc it DESTROYS the source: its rule is `$(AUTOCONF) $(ACFLAGS) $< > $@.new` followed by `mv -f $@.new $@`, so `true` writes an EMPTY file over each sysdeps/*/preconfigure. The second configure — the one make itself triggers through Makeconfig:103 to regenerate config.status — then loads empty fragments, $machine stays `s390x` instead of becoming `s390/s390-64`, and it dies on `configure: error: The s390x is not supported.` while the FIRST configure, minutes earlier in the same log, had listed sysdeps/s390 quite happily. glibc's own Makefile wraps that whole rule in `ifneq ($(AUTOCONF),no)`, so `no` is the value it documents for exactly this. It has to reach make as a COMMAND-LINE assignment: measured on 2026-09-29, an entry later in MAKEFLAGS wins (no) and a command-line assignment wins (no), but the environment does NOT — `AUTOCONF=no make -e` still saw `true`, so setting it in build.env would have looked right and changed nothing."

edits = [
  {
    # bk exports LDFLAGS="-Wl,-rpath,$PKGX_DIR" into every linux build, and
    # on linux that is the ONLY thing it puts there (buildscript/wrapper.go,
    # the `case "linux"` of wrapFlags). For a libc it is poison: glibc's own
    # loader asserts that the objects it processes carry NEITHER rpath tag —
    #
    #   get-dynamic-info.h:134  Assertion `info[DT_RPATH]   == NULL' failed!
    #   get-dynamic-info.h:133  Assertion `info[DT_RUNPATH] == NULL' failed!
    #
    # — and we met both in turn: the bottle shipped DT_RPATH, go-pkgx/bk#262
    # made SetRunpath write DT_RUNPATH as its name always claimed, and the
    # rebuilt bottle failed one assertion EARLIER. Modernising the tag walks
    # the failure up a line and no further, which is how we learned the
    # loader refuses the entry rather than the spelling.
    #
    # A libc has no use for one anyway: its objects sit beside each other and
    # the loader resolves them by absolute path. Clearing LDFLAGS drops the
    # rpath and nothing else on this platform, and fixup tolerates an ELF
    # with no slot to rewrite (rewriteRunpath treats ErrNoRunpath as a skip),
    # so the build is unaffected.
    #
    # It costs the sovereign lane's sysroot flags if glibc were ever built
    # with --libc=pkgx — which would be circular, and is not how this bottle
    # is made. Said here rather than discovered later.
    #
    # Why it matters beyond one bottle: gnu.org/glibc provides bin/ldd and
    # bottle puts glibc in EVERY linux closure, so a loader that aborts takes
    # `ldd` down for everything we publish — gnu.org/readline and
    # gnu.org/gcc/libstdcxx fail their tests on nothing but that.
    path = "build.env.LDFLAGS"
    set  = ""
  },
  {
    # The recipe's test keys $LDSO by architecture — x86-64 and aarch64 —
    # and s390x is not among them, so on the LinuxONE lane the test runs
    #
    #   test -f "$LIBDIR/"      -> false
    #   echo "missing "         -> a name nobody can look up
    #
    # and gnu.org/glibc was one of seven failures in the s390x seed's first
    # --test-only sweep (go-pkgx/bk#250). The bottle is fine; the test does
    # not know the architecture.
    #
    # ld64.so.1 is s390x's loader, taken from OUR OWN artefacts rather than
    # from memory: 362 occurrences of `ld64.so.1 is NEEDED` across this
    # lane's logs, and not one ld-linux-*.so.
    #
    # `set` deep-merges a mapping, so x86-64 and aarch64 keep theirs; and
    # the path's missing parents are created, so this does not depend on
    # test.env.s390x already existing.
    path = "test.env.s390x.LDSO"
    set  = "ld64.so.1"
  },
  {
    # And the BUILD env keys $LDSO the same way, which I missed when fixing
    # the test one. The script does
    #
    #   ln -sf ../lib/glibc-{{version.marketing}}/$LDSO ld.so
    #
    # so on s390x it ran with $LDSO EMPTY and made bin/ld.so a symlink to the
    # directory:
    #
    #   bin/ld.so -> ../lib/glibc-2.44/
    #
    # The recipe's own test checks `test -L bin/ld.so`, which a dangling link
    # passes — so the test that exists for this said nothing. Measured on the
    # runner, not inferred.
    path = "build.env.s390x.LDSO"
    set  = "ld64.so.1"
  },
  {
    path = "build.script"
    from = "make --jobs"
    to   = "make AUTOCONF=no --jobs"
  },
  {
    path = "build.script"
    from = "make install"
    to   = "make AUTOCONF=no install"
  },
]
