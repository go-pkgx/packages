provides = [
  "bin/zstd",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    {
      prop = <<EOT
#ifndef __ASSEMBLER__
int zs_chown(const char *, unsigned, unsigned);
#define chown zs_chown
#endif
EOT
      run = "cp $PROP programs/wasi_compat.h"
    },
    {
      prop = <<EOT
int zs_chown(const char *p, unsigned u, unsigned g) { (void)p; (void)u; (void)g; return 0; }
EOT
      run = "cp $PROP programs/wasi_stubs.c"
    },
    "make -C programs zstd CC=\"$BK_CC\" HAVE_THREAD=0 HAVE_ZLIB=0 HAVE_LZMA=0 HAVE_LZ4=0 BACKTRACE=0 CFLAGS=\"-O2 -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS -include wasi_compat.h\" LDLIBS=\"-lwasi-emulated-signal -lwasi-emulated-process-clocks\"",
    "install -D programs/zstd \"{{ prefix }}/bin/zstd.wasm\"",
  ]
}

distributable {
  strip-components = 1
  url = "https://github.com/facebook/zstd/releases/download/{{ version.tag }}/zstd-{{ version }}.tar.gz"
}

versions {
  github = "facebook/zstd"
}
