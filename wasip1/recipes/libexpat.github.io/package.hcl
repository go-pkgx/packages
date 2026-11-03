provides = [
  "bin/xmlwf",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    "./configure --prefix=\"{{ prefix }}\" --host=wasm32-wasi --disable-shared --enable-static --without-docbook CC=\"$BK_CC\" AR=llvm-ar RANLIB=llvm-ranlib CFLAGS=\"-O2\"",
    "make --jobs {{ hw.concurrency }} -C lib libexpat.la",
    "make --jobs {{ hw.concurrency }} -C xmlwf xmlwf",
    "install -D xmlwf/xmlwf \"{{ prefix }}/bin/xmlwf.wasm\"",
    "install -D lib/.libs/libexpat.a \"{{ prefix }}/lib/libexpat.a\"",
    "install -D lib/expat.h \"{{ prefix }}/include/expat.h\"",
    "install -D lib/expat_external.h \"{{ prefix }}/include/expat_external.h\"",
  ]
}

distributable {
  strip-components = 1
  url = "https://github.com/libexpat/libexpat/releases/download/R_{{ version.major }}_{{ version.minor }}_{{ version.patch }}/expat-{{ version }}.tar.gz"
}

versions {
  github = "libexpat/libexpat/releases"
}
