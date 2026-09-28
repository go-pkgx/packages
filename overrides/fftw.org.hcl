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
    from = "S "
    to   = "S $${EXTRA_ARGS:-} "
  },
  {
    path = "build.script"
    from = "re $ARGS"
    to   = "re $ARGS $${EXTRA_ARGS:-}"
  },
  {
    path = "build.script"
    from = "ble $ARGS"
    to   = "ble $ARGS $${EXTRA_ARGS:-}"
  },
]
