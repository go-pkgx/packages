project = "fftw.org"
why     = "converted from fftw.org-no-fortran-on-darwin.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.dependencies[\"gnu.org/binutils\"]"
    remove = true
  },
  {
    path = "build.dependencies[\"gnu.org/gcc\"]"
    remove = true
  },
  {
    path = "build.dependencies.linux"
    set = {
      "gnu.org/binutils" = "~2.44"
      "gnu.org/gcc" = 14
    }
  },
  {
    path = "build.env.darwin.EXTRA_ARGS"
    set = "--disable-fortran"
  },
  {
    path = "build.env.linux"
    set = {
      "EXTRA_ARGS" = ""
    }
  },
  {
    path = "build.script"
    set = [
      "./configure --enable-single $ARGS $${EXTRA_ARGS:-} || (cat config.log && false)",
      "make --jobs {{hw.concurrency}} install",
      "make clean",
      "./configure $ARGS $${EXTRA_ARGS:-}",
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
