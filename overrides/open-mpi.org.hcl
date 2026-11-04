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
  (go-pkgx/packages#417).

  "Increasingly ancient" is the part worth re-reading: that comment is about
  the hardware GitHub used to offer, and this factory's x86-64 lane runs on
  macos-15-intel, an image from this year. The premise may simply have
  expired, and the way to find out is to build it — one runner, against a
  defect that currently has no other route to repair.

  If Fortran is still the blocker, the next step is --disable-mpi-fortran:
  fftw's MPI support uses the C API, so the Fortran bindings are not what it
  needs. Delete this override if upstream retargets the platform list, or
  replace it with the Fortran switch if the build proves the comment right.
WHY

edits = [
  {
    path   = "platforms"
    append = ["darwin/x86-64"]
  },
]
