provides = [
  "bin/bzip2",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    {
      prop = <<EOT
int fchown(int, unsigned, unsigned);
EOT
      run = "cp $PROP wasi_compat.h"
    },
    {
      prop = <<EOT
struct utimbuf;
int fchown(int fd, unsigned u, unsigned g) { (void)fd; (void)u; (void)g; return 0; }
int bz_fchmod(int fd, unsigned m) { (void)fd; (void)m; return 0; }
int bz_utime(const char *p, const struct utimbuf *t) { (void)p; (void)t; return 0; }
EOT
      run = "cp $PROP wasi_stubs.c"
    },
    {
      run = "$BK_CC -O2 -c -o wasi_stubs.o wasi_stubs.c"
    },
    "make libbz2.a bzip2 CC=\"$BK_CC\" AR=llvm-ar RANLIB=llvm-ranlib CFLAGS=\"-O2 -D_FILE_OFFSET_BITS=64 -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS -Dfchmod=bz_fchmod -Dutime=bz_utime -include wasi_compat.h\" LDFLAGS=\"$SRCROOT/wasi_stubs.o -lwasi-emulated-signal -lwasi-emulated-process-clocks\"",
    "install -D bzip2 \"{{ prefix }}/bin/bzip2.wasm\"",
    "install -D libbz2.a \"{{ prefix }}/lib/libbz2.a\"",
    "install -D bzlib.h \"{{ prefix }}/include/bzlib.h\"",
  ]
}

distributable {
  strip-components = 1
  url = "https://sourceware.org/pub/bzip2/bzip2-{{version}}.tar.gz"
}

versions {
  match = "/bzip2-(\\d+\\.\\d+\\.\\d+)\\.tar\\.gz/"
  strip = [
    "/bzip2-/",
    "/.tar.gz/",
  ]
  url = "https://sourceware.org/pub/bzip2/"
}
