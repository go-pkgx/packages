project = "open-mpi.org"
why     = <<-WHY
  This recipe excludes darwin/x86-64 with the comment "fortran not happy on
  increasingly ancient darwin/x86-64". The exclusion is a DECLARATION, not a
  missing dependency: measured against ghcr for darwin/x86-64, 7 of open-mpi's
  8 build and runtime dependencies are published there — hwloc 2.15.0,
  openpmix 6.1.0, libevent 2.1.13, zlib 1.3.2, python 3.14.7, binutils 2.47.0,
  and gnu.org/gcc 16.2.0, which is where gfortran comes from. The eighth is
  open-mpi itself.

  The exclusion costs a package nobody else can repair. fftw.org depends on
  open-mpi.org unconditionally, so fftw cannot be BUILT for darwin/x86-64 at
  all, and the bottle published there is not relocatable: `otool -L` on
  bin/fftw-wisdom shows /Users/runner/.pkgx/fftw.org/v3.3.11/lib/libfftw3.3.dylib
  as an LC_LOAD_DYLIB entry, so it looks for its own libraries in a build
  machine's home directory and finds them on no other machine. It cannot be
  superseded while its dependency refuses the platform
  (go-zpkg/packages#417).

  "Increasingly ancient" is the part worth re-reading: that comment is about
  the hardware GitHub used to offer, and this factory's x86-64 lane runs on
  macos-15-intel, an image from this year. The premise may simply have
  expired, and the way to find out is to build it — one runner, against a
  defect that currently has no other route to repair.

  Fortran turned out not to be the blocker. With the platform open, the build
  reached `make` and died linking prrte:

    ld: multiple errors: archive member invalid control bits in
        'util/.libs/libprrteutil.a'; ... (26 archives)
    clang: error: linker command failed with exit code 1

  That is an ARCHIVER mismatch, not a language one. The recipe forces Apple's
  linker on darwin ("prevent gcc from taking over", LD: /usr/bin/ld) while
  gnu.org/binutils sits in the build environment and puts GNU `ar` on PATH --
  configure says so itself: "checking for archiver @FILE support... @", which
  Apple's ar does not have. GNU ar writes the archives, Apple ld refuses
  them.

  Reproduced in isolation, both directions, in seconds:

    $ pkgx +gnu.org/binutils -- ar rcs libgnu.a foo.o
    $ clang main.c libgnu.a -o out
    ld: ignoring archive member 'foo.o' - 64-bit mach-o not 8-byte aligned
    clang: error: linker command failed with exit code 1

    $ /usr/bin/ar rcs libapple.a foo.o
    $ clang main.c libapple.a -o out      # links

  (The wording differs from CI's -- two ld versions complaining about the
  same class of malformed archive -- and it reproduces on arm64 too, so this
  is not about x86-64 being old.)

  So AR and RANLIB are pinned to Apple's on darwin, to match the linker the
  recipe already pins. Delete this override if upstream retargets the
  platform list or stops mixing the two toolchains.
WHY

edits = [
  {
    path   = "platforms"
    append = ["darwin/x86-64"]
  },
  {
    path = "build.env.darwin.AR"
    set  = "/usr/bin/ar"
  },
  {
    path = "build.env.darwin.RANLIB"
    set  = "/usr/bin/ranlib"
  },
]
