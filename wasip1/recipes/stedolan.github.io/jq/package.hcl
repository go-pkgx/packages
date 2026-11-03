provides = [
  "bin/jq",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    "./configure --prefix=\"{{ prefix }}\" --host=wasm32-wasi --disable-shared --enable-static --disable-docs --disable-maintainer-mode --with-oniguruma=builtin CC=\"$BK_CC\" AR=llvm-ar RANLIB=llvm-ranlib CFLAGS=\"-O2 -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS\" LDFLAGS=\"-lwasi-emulated-signal -lwasi-emulated-process-clocks\"",
    "make --jobs {{ hw.concurrency }}",
    "install -D jq \"{{ prefix }}/bin/jq.wasm\"",
  ]
}

distributable {
  strip-components = 1
  url = "https://github.com/jqlang/jq/releases/download/jq-{{version.raw}}/jq-{{version.raw}}.tar.gz"
}

versions {
  github = "jqlang/jq/releases"
  strip = "/jq /"
}
