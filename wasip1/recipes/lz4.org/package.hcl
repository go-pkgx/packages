provides = [
  "bin/lz4",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    {
      prop = <<EOT
#ifndef __ASSEMBLER__
int lz_chown(const char *, unsigned, unsigned);
#define chown lz_chown
#endif
EOT
      run = "cp $PROP programs/wasi_compat.h"
    },
    {
      prop = <<EOT
int lz_chown(const char *p, unsigned u, unsigned g) { (void)p; (void)u; (void)g; return 0; }
EOT
      run = "cp $PROP programs/wasi_stubs.c"
    },
    "make -C programs lz4 CC=\"$BK_CC\" HAVE_MULTITHREAD=0 CFLAGS=\"-O2 -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS -include wasi_compat.h\" LDLIBS=\"-lwasi-emulated-signal -lwasi-emulated-process-clocks -Wl,-z,stack-size=8388608\"",
    "install -D programs/lz4 \"{{ prefix }}/bin/lz4.wasm\"",
  ]
}

distributable {
  strip-components = 1
  url = "https://github.com/lz4/lz4/releases/download/{{ version.tag }}/lz4-{{ version }}.tar.gz"
}

versions {
  github = "lz4/lz4"
}
