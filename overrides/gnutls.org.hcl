project = "gnutls.org"
why     = "gnutls' own header invites CRAU_MAYBE_UNUSED to be defined from outside, and without it the build fails on an attribute the compiler here does not accept in that position."

edits = [
  {
    path = "build.env.darwin.CFLAGS"
    set  = "$CFLAGS -Wno-implicit-int -Wno-unused-parameter -DCRAU_MAYBE_UNUSED="
  },
]
