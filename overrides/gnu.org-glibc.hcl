project = "gnu.org/glibc"
why     = "bk exports `MAKEFLAGS=… AUTOCONF=true …` so a stale timestamp cannot send make off to re-run autotools (bk build/build.go). For every autotools project that is a harmless no-op, and for glibc it DESTROYS the source: its rule is `$(AUTOCONF) $(ACFLAGS) $< > $@.new` followed by `mv -f $@.new $@`, so `true` writes an EMPTY file over each sysdeps/*/preconfigure. The second configure — the one make itself triggers through Makeconfig:103 to regenerate config.status — then loads empty fragments, $machine stays `s390x` instead of becoming `s390/s390-64`, and it dies on `configure: error: The s390x is not supported.` while the FIRST configure, minutes earlier in the same log, had listed sysdeps/s390 quite happily. glibc's own Makefile wraps that whole rule in `ifneq ($(AUTOCONF),no)`, so `no` is the value it documents for exactly this. It has to reach make as a COMMAND-LINE assignment: measured on 2026-09-29, an entry later in MAKEFLAGS wins (no) and a command-line assignment wins (no), but the environment does NOT — `AUTOCONF=no make -e` still saw `true`, so setting it in build.env would have looked right and changed nothing."

edits = [
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
