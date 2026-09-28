project = "gnu.org/libtasn1"
why     = "converted from gnu.org-libtasn1-pthread-probe.patch; the reason is in the git history of those files"

edits = [
  {
    path = "build.env"
    set = {
      "CFLAGS" = "$CFLAGS -Werror=implicit-function-declaration"
    }
  },
]
