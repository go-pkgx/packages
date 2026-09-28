project = "gnutls.org"
why     = "converted from gnutls.org-darwin-crau-attribute.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.env.darwin.CFLAGS"
    set = "$CFLAGS -Wno-implicit-int -Wno-unused-parameter -DCRAU_MAYBE_UNUSED="
  },
]
