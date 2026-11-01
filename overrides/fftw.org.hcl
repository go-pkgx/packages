project = "fftw.org"
why     = "fftw's darwin build asks for a Fortran compiler. There is none on darwin here, and the rebuild never started — the Fortran interface is not in the bottle either way. AND the published bottle has no SIMD at all: upstream's ARGS carry no --enable-neon or --enable-sse2/avx/avx2, so every consumer gets scalar C FFTW. Measured on the installed darwin/aarch64 bottle 3.3.11: libfftw3.3.dylib exports 542 _fftw_ symbols and ZERO n1fv_/t1fv_/n2fv_ NEON codelets, while libfftw3.a does contain a neon.o — compiled empty, which is why looking for the object is not the check."

edits = [
  {
    path   = "build.dependencies[\"gnu.org/binutils\"]"
    remove = true
  },
  {
    path   = "build.dependencies[\"gnu.org/gcc\"]"
    remove = true
  },
  {
    path = "build.dependencies.linux"
    set = {
      "gnu.org/binutils" = "~2.44"
      "gnu.org/gcc"      = 14
    }
  },
  {
    path = "build.env.darwin.EXTRA_ARGS"
    set  = "--disable-fortran"
  },
  {
    path = "build.env.linux"
    set = {
      "EXTRA_ARGS" = ""
    }
  },

  # ⛔ SIMD, BY ARCHITECTURE AND NOT BY OS.
  #
  # Keyed on the bare arch so it composes with the darwin/linux keys above
  # rather than racing them: SIMD depends on the instruction set, not the
  # kernel. An arch with no entry gets an empty SIMD_ARGS and the scalar build
  # it has today, which is the right default for s390x and anything new.
  #
  # Read from the FFTW sources, not guessed:
  #   configure.ac:192-205   --enable-neon is allowed for ANY precision when
  #                          host_cpu is aarch64, and single-only elsewhere
  #   simd-support/simd-neon.h:27-28
  #                          #error "NEON only works in single or double
  #                          precision" — so it must NOT go on the
  #                          --enable-long-double line, and it does not
  #   NEWS (3.3.5)           "Double precision Neon SIMD for aarch64"
  #
  # x86: sse2/avx/avx2 codelets carry RUNTIME CPU dispatch, so enabling them in
  # a bottle is safe on an older CPU — it selects at plan time. avx512 is
  # deliberately NOT here: it needs an assembler that accepts it, and a build
  # that fails on one runner and not another is worse than a missing codelet.
  {
    path = "build.env.aarch64"
    set = {
      "SIMD_ARGS"  = "--enable-neon"
      "SIMD_CHECK" = "HAVE_NEON"
    }
  },
  {
    path = "build.env.x86-64"
    set = {
      "SIMD_ARGS"  = "--enable-sse2 --enable-avx --enable-avx2"
      "SIMD_CHECK" = "HAVE_AVX2"
    }
  },
  {
    path = "build.script"
    set = [
      # single and double get SIMD; long double must not — simd-neon.h #errors
      # on it, and FFTW has no long-double SIMD on any target.
      #
      # ⛔ AND EACH ONE IS CHECKED. A configure that ACCEPTS --enable-neon and
      # then does not define HAVE_NEON produces a scalar library and exits 0 —
      # which is exactly how the bottle came to have no SIMD without anyone
      # noticing. The flag not taking effect must fail the build, not pass it.
      #
      # The known way for that to happen here: FFTW's configure.ac matches
      # host_cpu=aarch64, and a bundled config.guess old enough to report
      # Apple Silicon as `arm` misses the case. If this fires on darwin/arm64,
      # the fix is --host=aarch64-apple-darwin on that leg — and the config.log
      # dumped below is where to read it.
      "./configure --enable-single $ARGS $${SIMD_ARGS:-} $${EXTRA_ARGS:-} || (cat config.log && false)",
      "test -z \"$${SIMD_CHECK:-}\" || grep -q \"^#define $${SIMD_CHECK} 1\" config.h || { echo \"SIMD requested ($${SIMD_ARGS:-}) but $${SIMD_CHECK} is not defined: this would be a scalar build\"; cat config.log; false; }",
      "make --jobs {{hw.concurrency}} install",
      "make clean",
      "./configure $ARGS $${SIMD_ARGS:-} $${EXTRA_ARGS:-} || (cat config.log && false)",
      "test -z \"$${SIMD_CHECK:-}\" || grep -q \"^#define $${SIMD_CHECK} 1\" config.h || { echo \"SIMD requested ($${SIMD_ARGS:-}) but $${SIMD_CHECK} is not defined: this would be a scalar build\"; cat config.log; false; }",
      "make --jobs {{hw.concurrency}} install",
      "make clean",
      "./configure --enable-long-double $ARGS $${EXTRA_ARGS:-}",
      "make --jobs {{hw.concurrency}} install",
    ]
    expect = [
      "./configure --enable-single $ARGS || (cat config.log && false)",
      "make --jobs {{hw.concurrency}} install",
      "make clean",
      "./configure $ARGS",
      "make --jobs {{hw.concurrency}} install",
      "make clean",
      "./configure --enable-long-double $ARGS",
      "make --jobs {{hw.concurrency}} install",
    ]
  },
]
