provides = [
  "bin/pcre2grep",
  "bin/pcre2test",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    "./configure --prefix=\"{{ prefix }}\" --host=wasm32-wasi --disable-shared --enable-static --disable-jit --disable-pcre2grep-callout --disable-pcre2grep-callout-fork CC=\"$BK_CC\" AR=llvm-ar RANLIB=llvm-ranlib CFLAGS=\"-O2 -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS\" LDFLAGS=\"-lwasi-emulated-signal -lwasi-emulated-process-clocks\"",
    "make --jobs {{ hw.concurrency }} libpcre2-8.la pcre2grep pcre2test",
    "install -D pcre2grep \"{{ prefix }}/bin/pcre2grep.wasm\"",
    "install -D pcre2test \"{{ prefix }}/bin/pcre2test.wasm\"",
    "install -D .libs/libpcre2-8.a \"{{ prefix }}/lib/libpcre2-8.a\"",
    "install -D src/pcre2.h \"{{ prefix }}/include/pcre2.h\"",
  ]
}

distributable {
  strip-components = 1
  url = "https://github.com/PCRE2Project/pcre2/releases/download/pcre2-{{version.major}}.{{version.minor}}/pcre2-{{version.major}}.{{version.minor}}.tar.gz"
}

versions {
  github = "PCRE2Project/pcre2/tags"
  strip = "/^pcre2-/"
}
