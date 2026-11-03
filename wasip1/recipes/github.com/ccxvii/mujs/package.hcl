provides = [
  "bin/mujs",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    "make -o utfdata.h -o astnames.h -o opnames.h build/release/mujs HAVE_READLINE=no CC=\"$BK_CC\" AR=llvm-ar CFLAGS=\"-std=c99 -O2 -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS -mllvm -wasm-enable-sjlj -mllvm -wasm-use-legacy-eh=false\" READLINE_LIBS=\"-lsetjmp -lwasi-emulated-signal -lwasi-emulated-process-clocks\"",
    "install -D build/release/mujs \"{{ prefix }}/bin/mujs.wasm\"",
  ]
}

distributable {
  strip-components = 1
  url = "https://github.com/ccxvii/mujs/archive/refs/tags/{{ version.raw }}.tar.gz"
}

versions {
  github = "ccxvii/mujs/tags"
}
