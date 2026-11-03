# fftw for wasm32-wasip1: one precision, scalar, static.
#
# ⛔ ONE PRECISION, NOT THREE. The host recipe configures and installs three
# times — `--enable-single`, default double, `--enable-long-double` — which is
# right where consumers link whichever they need. Here it is double only:
# nothing in the wasm set links fftw yet, so a third of the build time buys
# three libraries nobody has asked for. The day something needs float, add
# `--enable-single` as its own pass rather than guessing now.
#
# ⛔ NO SIMD, AND THAT IS NOT AN OVERSIGHT. `overrides/fftw.org.hcl` keys SIMD
# on the ARCHITECTURE precisely so that an arch with no entry gets the scalar
# build — "the right default for s390x and anything new", and wasm is the
# anything new. FFTW has no wasm SIMD128 backend: configure's choices are
# sse2/avx/avx2/neon/altivec/vsx, none of which wasm32 can take.
#
# ⛔ THREADS OFF. wasip1 has no pthreads, and fftw's threads layer is the one
# that would silently configure itself in on a host that has them.

provides = [
  "bin/fftw-wisdom",
]

build {
  dependencies = {
    "gnu.org/make" = "*"
  }
  script = [
    # --host makes autoconf cross-compile rather than run its probes, which is
    # the whole reason a wasm build of an autotools project works at all.
    "./configure --prefix=\"{{ prefix }}\" --host=wasm32-wasi --disable-shared --enable-static --disable-fortran --disable-threads --disable-openmp --disable-mpi CC=\"$BK_CC\" AR=llvm-ar RANLIB=llvm-ranlib CFLAGS=\"-O2\" || (cat config.log && false)",
    "make --jobs {{ hw.concurrency }}",
    "make install",
    # The tool is installed by `make install` without the extension; the lane
    # runs `<bin>.wasm` under wasmtime, as every recipe here does.
    "install -D tools/fftw-wisdom \"{{ prefix }}/bin/fftw-wisdom.wasm\"",
  ]
}

distributable {
  strip-components = 1
  url              = "https://fftw.org/fftw-{{version}}.tar.gz"
}

versions {
  url   = "https://fftw.org/download.html"
  match = "/fftw-\\d+\\.\\d+\\.\\d+\\.tar\\.gz/"
  strip = [
    "/^fftw-/",
    "/\\.tar\\.gz/",
  ]
}
