provides = [
  "bin/brotli",
]

build {
  script = [
    {
      prop = <<EOT
#ifndef __ASSEMBLER__
int zs_chown(const char *, unsigned, unsigned);
#define chown zs_chown
#endif
EOT
      run = "cp $PROP wasi_compat.h"
    },
    {
      prop = <<EOT
int zs_chown(const char *p, unsigned u, unsigned g) { (void)p; (void)u; (void)g; return 0; }
EOT
      run = "cp $PROP wasi_stubs.c"
    },
    {
      run = "$BK_CC -O2 -Ic/include -include wasi_compat.h -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS -o brotli.wasm wasi_stubs.c c/common/*.c c/dec/*.c c/enc/*.c c/tools/brotli.c -lwasi-emulated-signal -lwasi-emulated-process-clocks"
    },
    "install -D brotli.wasm \"{{ prefix }}/bin/brotli.wasm\"",
  ]
}

distributable {
  strip-components = 1
  url = "https://github.com/google/brotli/archive/refs/tags/{{ version.tag }}.tar.gz"
}

versions {
  github = "google/brotli"
}
